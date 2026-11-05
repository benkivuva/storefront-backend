package domain

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

// halfUpNearest checks that vat is the nearest integer to gross*r/d, with
// ties going up: 2*|vat*d - gross*r| <= d. Uses big.Int because vat*d and
// gross*r both exceed int64 for large inputs.
func halfUpNearest(t *testing.T, gross, vat int64, rateBp int) {
	t.Helper()
	d := big.NewInt(int64(10000 + rateBp))
	r := big.NewInt(int64(rateBp))
	left := new(big.Int).Mul(big.NewInt(vat), d)
	right := new(big.Int).Mul(big.NewInt(gross), r)
	diff := new(big.Int).Sub(left, right)
	diff.Abs(diff)
	diff.Mul(diff, big.NewInt(2))
	if diff.Cmp(d) > 0 {
		t.Fatalf("vat=%d is not half-up nearest for gross=%d rateBp=%d (2*|diff|=%s > d=%s)",
			vat, gross, rateBp, diff, d)
	}
}

// ---- construction / error contract ----

func TestExtractVAT_ZeroValueMoneyRejected(t *testing.T) {
	if _, _, err := ExtractVAT(Money{}, 1600); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("want ErrInvalidMoney, got %v", err)
	}
}

func TestExtractVAT_InvalidRate(t *testing.T) {
	g := mustMoney(t, 1000)
	for _, r := range []int{-1, 10001, 999999} {
		_, _, err := ExtractVAT(g, r)
		if !errors.Is(err, ErrInvalidRateBp) {
			t.Errorf("rateBp=%d: want ErrInvalidRateBp, got %v", r, err)
		}
	}
}

func TestExtractVAT_Overflow(t *testing.T) {
	// MaxInt64 * 1600 vastly exceeds int64. The check must fire, not wrap.
	g := mustMoney(t, math.MaxInt64)
	if _, _, err := ExtractVAT(g, 1600); !errors.Is(err, ErrTaxOverflow) {
		t.Fatalf("want ErrTaxOverflow, got %v", err)
	}
}

func TestExtractVAT_ErrorReturnsZeroMoney(t *testing.T) {
	cases := []struct {
		name string
		g    Money
		r    int
	}{
		{"zero value Money", Money{}, 1600},
		{"negative rate", mustMoney(t, 100), -1},
		{"overflowing rate", mustMoney(t, 100), 10001},
		{"gross overflow", mustMoney(t, math.MaxInt64), 1600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			net, vat, err := ExtractVAT(tc.g, tc.r)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if net != (Money{}) || vat != (Money{}) {
				t.Fatalf("error result must be zero Money: net=%+v vat=%+v", net, vat)
			}
		})
	}
}

// ---- boundaries: exact-integer, ties, zero-rate ----

func TestExtractVAT_Boundaries(t *testing.T) {
	cases := []struct {
		name    string
		gross   int64
		rateBp  int
		wantNet int64
		wantVat int64
	}{
		// zero gross
		{"zero at 16%", 0, 1600, 0, 0},

		// zero-rate short-circuit
		{"zero-rated", 1000, 0, 1000, 0},
		{"zero-rated at zero gross", 0, 0, 0, 0},

		// rounding down (exact < .5)
		{"1 cent at 16% rounds down", 1, 1600, 1, 0},

		// rounding up (exact > .5)
		{"4 cents at 16% rounds up", 4, 1600, 3, 1},

		// exact integer (no rounding)
		{"29 cents at 16% is exact", 29, 1600, 25, 4},

		// the classic 16% example from the architecture
		{"1160 at 16%", 1160, 1600, 1000, 160},

		// ties — only reachable at even-divisor rates.
		// r=10000 => d=20000 => exact halves land at .5, 1.5, 2.5
		// half-up must round to the higher integer each time.
		{"tie 0.5 half-up to 1", 1, 10000, 0, 1},
		{"tie 1.5 half-up to 2", 3, 10000, 1, 2},
		{"tie 2.5 half-up to 3", 5, 10000, 2, 3},

		// max realistic rate
		{"100% VAT splits evenly", 10, 10000, 5, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			net, vat, err := ExtractVAT(mustMoney(t, tc.gross), tc.rateBp)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if net.AmountMinor() != tc.wantNet || vat.AmountMinor() != tc.wantVat {
				t.Fatalf("got net=%d vat=%d, want net=%d vat=%d",
					net.AmountMinor(), vat.AmountMinor(), tc.wantNet, tc.wantVat)
			}
		})
	}
}

// ---- properties ----

// Overflow is impossible in this range: g*10000 + 10000 <= MaxInt64 for
// g <= MaxInt64/20000, with ~10x margin. Covers realistic commerce values
// (KES 4.6 trillion) while exercising every rate in [0, 10000].
func TestProperty_NetPlusVATEqualsGross(t *testing.T) {
	r := rand.New(rand.NewPCG(21, 22))
	safe := int64(math.MaxInt64 / 20000)
	for i := 0; i < 2000; i++ {
		grossAmt := r.Int64N(safe)
		rateBp := int(r.Int64N(10001))
		gross, _ := New(grossAmt, KES)
		net, vat, err := ExtractVAT(gross, rateBp)
		if err != nil {
			t.Fatalf("iter %d: unexpected error (range is overflow-safe): %v", i, err)
		}
		sum, err := net.Add(vat)
		if err != nil || sum != gross {
			t.Fatalf("iter %d: net+vat != gross: %v + %v = %v, %v",
				i, net, vat, sum, err)
		}
		if vat.AmountMinor() < 0 || vat.AmountMinor() > gross.AmountMinor() {
			t.Fatalf("iter %d: vat out of [0, gross]: vat=%d gross=%d",
				i, vat.AmountMinor(), gross.AmountMinor())
		}
		halfUpNearest(t, gross.AmountMinor(), vat.AmountMinor(), rateBp)
	}
}

// Full int64 range. If ExtractVAT succeeds, invariants hold. If it returns
// ErrTaxOverflow, verify (via big.Int, independent of the implementation)
// that the overflow was real. Any other error is a bug.
func TestProperty_OverflowCorrectness(t *testing.T) {
	r := rand.New(rand.NewPCG(41, 42))
	for i := 0; i < 2000; i++ {
		grossAmt := r.Int64N(math.MaxInt64)
		rateBp := int(r.Int64N(10001))
		gross, _ := New(grossAmt, KES)
		net, vat, err := ExtractVAT(gross, rateBp)
		if err != nil {
			if !errors.Is(err, ErrTaxOverflow) {
				t.Fatalf("iter %d: unexpected error: %v", i, err)
			}
			// Independent check: was the overflow real?
			g := big.NewInt(grossAmt)
			rr := big.NewInt(int64(rateBp))
			d := big.NewInt(int64(10000 + rateBp))
			half := new(big.Int).Div(d, big.NewInt(2))
			product := new(big.Int).Mul(g, rr)
			product.Add(product, half)
			if product.IsInt64() {
				t.Fatalf("iter %d: ErrTaxOverflow but %s fits int64 (false positive)",
					i, product)
			}
			continue
		}
		sum, err := net.Add(vat)
		if err != nil || sum != gross {
			t.Fatalf("iter %d: net+vat != gross: %v + %v = %v, %v",
				i, net, vat, sum, err)
		}
		halfUpNearest(t, gross.AmountMinor(), vat.AmountMinor(), rateBp)
	}
}

func TestProperty_ZeroRateIsIdentity(t *testing.T) {
	r := rand.New(rand.NewPCG(31, 32))
	for i := 0; i < 200; i++ {
		gross := randMoney(t, r, math.MaxInt64)
		net, vat, err := ExtractVAT(gross, 0)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if net != gross {
			t.Fatalf("iter %d: net != gross at rateBp=0", i)
		}
		if vat.AmountMinor() != 0 {
			t.Fatalf("iter %d: vat != 0 at rateBp=0: %d", i, vat.AmountMinor())
		}
	}
}

// Fuzz across gross and rateBp. Plain go test runs the seeds;
// go test -fuzz=FuzzExtractVAT explores further.
func FuzzExtractVAT(f *testing.F) {
	f.Add(int64(1160), 1600)
	f.Add(int64(0), 1600)
	f.Add(int64(5), 10000)
	f.Add(int64(29), 1600)
	f.Add(int64(0), 0)
	f.Fuzz(func(t *testing.T, grossAmt int64, rateBp int) {
		if grossAmt < 0 || rateBp < 0 || rateBp > 10000 {
			t.Skip()
		}
		gross, err := New(grossAmt, KES)
		if err != nil {
			t.Fatalf("New(%d, KES): %v", grossAmt, err)
		}
		net, vat, err := ExtractVAT(gross, rateBp)
		if err != nil {
			if !errors.Is(err, ErrTaxOverflow) {
				t.Fatalf("unexpected error: %v", err)
			}
			return
		}
		sum, err := net.Add(vat)
		if err != nil || sum != gross {
			t.Fatalf("net+vat != gross: %v + %v = %v, %v", net, vat, sum, err)
		}
		if vat.AmountMinor() < 0 || vat.AmountMinor() > gross.AmountMinor() {
			t.Fatalf("vat out of range: vat=%d gross=%d",
				vat.AmountMinor(), gross.AmountMinor())
		}
		halfUpNearest(t, gross.AmountMinor(), vat.AmountMinor(), rateBp)
	})
}
