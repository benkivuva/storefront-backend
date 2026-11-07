package domain

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

func mustMoney(t *testing.T, amount int64) Money {
	t.Helper()
	m, err := New(amount, KES)
	if err != nil {
		t.Fatalf("New(%d, KES): %v", amount, err)
	}
	return m
}

func randMoney(t *testing.T, r *rand.Rand, upper int64) Money {
	t.Helper()
	if upper <= 0 {
		t.Fatalf("randMoney: upper must be positive, got %d", upper)
	}
	return mustMoney(t, r.Int64N(upper))
}

// sumFitsInt64 is an oracle that does not share code with the implementation:
// math/big cannot overflow, so it decides independently whether a+b fits.
func sumFitsInt64(a, b int64) bool {
	return new(big.Int).Add(big.NewInt(a), big.NewInt(b)).IsInt64()
}

// ---- construction ----

func TestNew_AcceptsBounds(t *testing.T) {
	for _, amt := range []int64{0, 1, math.MaxInt64} {
		m, err := New(amt, KES)
		if err != nil {
			t.Fatalf("New(%d, KES): %v", amt, err)
		}
		if m.AmountMinor() != amt || m.Currency() != KES {
			t.Fatalf("New(%d, KES) = %+v", amt, m)
		}
	}
}

func TestNew_RejectsNegative(t *testing.T) {
	for _, amt := range []int64{-1, math.MinInt64} {
		if _, err := New(amt, KES); !errors.Is(err, ErrNegativeAmount) {
			t.Fatalf("New(%d): want ErrNegativeAmount, got %v", amt, err)
		}
	}
}

func TestNew_RejectsUnsupportedCurrency(t *testing.T) {
	for _, c := range []Currency{"", "USD", "kes"} {
		if _, err := New(100, c); !errors.Is(err, ErrInvalidCurrency) {
			t.Errorf("New(100, %q): want ErrInvalidCurrency, got %v", c, err)
		}
		if _, err := Zero(c); !errors.Is(err, ErrInvalidCurrency) {
			t.Errorf("Zero(%q): want ErrInvalidCurrency, got %v", c, err)
		}
	}
}

func TestIsValid(t *testing.T) {
	var zero Money
	if zero.IsValid() {
		t.Fatal("zero value must be invalid")
	}
	z, err := Zero(KES)
	if err != nil {
		t.Fatalf("Zero(KES): %v", err)
	}
	if !z.IsValid() || z.AmountMinor() != 0 {
		t.Fatalf("Zero(KES) invalid: %+v", z)
	}
}

func TestZeroValue_OperationsFail(t *testing.T) {
	var zero Money
	good := mustMoney(t, 100)
	cases := map[string]func() error{
		"zero.Add(good)": func() error { _, err := zero.Add(good); return err },
		"good.Add(zero)": func() error { _, err := good.Add(zero); return err },
		"zero.Sub(good)": func() error { _, err := zero.Sub(good); return err },
		"good.Sub(zero)": func() error { _, err := good.Sub(zero); return err },
		"zero.Add(zero)": func() error { _, err := zero.Add(zero); return err },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			if err := f(); !errors.Is(err, ErrInvalidMoney) {
				t.Fatalf("want ErrInvalidMoney, got %v", err)
			}
		})
	}
}

// Only one currency is supported, so a second one has to be forged in-package.
func TestCurrencyMismatch(t *testing.T) {
	kes := mustMoney(t, 100)
	usd := Money{amountMinor: 100, currency: "USD"}
	if _, err := kes.Add(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Add: want ErrCurrencyMismatch, got %v", err)
	}
	if _, err := kes.Sub(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("Sub: want ErrCurrencyMismatch, got %v", err)
	}
}

// ---- exact boundaries (random inputs essentially never land on these) ----

func TestAdd_Boundaries(t *testing.T) {
	half := int64(1) << 62
	cases := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{"max plus zero", math.MaxInt64, 0, math.MaxInt64, nil},
		{"max-1 plus one lands exactly on max", math.MaxInt64 - 1, 1, math.MaxInt64, nil},
		{"max plus one overflows", math.MaxInt64, 1, 0, ErrOverflow},
		{"one plus max overflows", 1, math.MaxInt64, 0, ErrOverflow},
		{"half-1 plus half lands exactly on max", half - 1, half, math.MaxInt64, nil},
		{"half plus half overflows", half, half, 0, ErrOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mustMoney(t, tc.a).Add(mustMoney(t, tc.b))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				if got != (Money{}) {
					t.Fatalf("error result must be the zero Money, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.AmountMinor() != tc.want {
				t.Fatalf("got %d, want %d", got.AmountMinor(), tc.want)
			}
		})
	}
}

func TestSub_Boundaries(t *testing.T) {
	cases := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{"equal amounts give zero", 5, 5, 0, nil},
		{"one short underflows", 5, 6, 0, ErrUnderflow},
		{"zero minus one underflows", 0, 1, 0, ErrUnderflow},
		{"max minus max is zero", math.MaxInt64, math.MaxInt64, 0, nil},
		{"max minus zero is max", math.MaxInt64, 0, math.MaxInt64, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mustMoney(t, tc.a).Sub(mustMoney(t, tc.b))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				if got != (Money{}) {
					t.Fatalf("error result must be the zero Money, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.AmountMinor() != tc.want {
				t.Fatalf("got %d, want %d", got.AmountMinor(), tc.want)
			}
		})
	}
}

// ---- properties (fixed seeds: reproducible) ----

func TestProperty_AddCommutative(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 1000; i++ {
		a := randMoney(t, r, math.MaxInt64/2)
		b := randMoney(t, r, math.MaxInt64/2)
		ab, err1 := a.Add(b)
		ba, err2 := b.Add(a)
		if err1 != nil || err2 != nil {
			t.Fatalf("iter %d: unexpected error: %v / %v", i, err1, err2)
		}
		if ab != ba {
			t.Fatalf("iter %d: a+b=%v != b+a=%v", i, ab, ba)
		}
	}
}

func TestProperty_AddSubRoundtrip(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 1000; i++ {
		a := randMoney(t, r, math.MaxInt64/2)
		b := randMoney(t, r, math.MaxInt64/2)
		ab, err := a.Add(b)
		if err != nil {
			t.Fatalf("iter %d: Add: %v", i, err)
		}
		back, err := ab.Sub(b)
		if err != nil {
			t.Fatalf("iter %d: Sub: %v", i, err)
		}
		if back != a {
			t.Fatalf("iter %d: (a+b)-b=%v, want %v", i, back, a)
		}
	}
}

func TestProperty_ZeroIdentity(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	z, _ := Zero(KES)
	for i := 0; i < 100; i++ {
		a := randMoney(t, r, math.MaxInt64/2)
		if sum, err := a.Add(z); err != nil || sum != a {
			t.Fatalf("iter %d: a+0=%v,%v", i, sum, err)
		}
		if diff, err := a.Sub(z); err != nil || diff != a {
			t.Fatalf("iter %d: a-0=%v,%v", i, diff, err)
		}
	}
}

func TestProperty_OverflowMatchesOracle(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 1000; i++ {
		a := randMoney(t, r, math.MaxInt64)
		b := randMoney(t, r, math.MaxInt64)
		checkAdd(t, i, a, b)
	}
}

// Draws b at MaxInt64-a plus a small delta, so sums land on or next to the
// overflow boundary instead of (almost) never.
func TestProperty_OverflowNearBoundary(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	for i := 0; i < 1000; i++ {
		a := randMoney(t, r, math.MaxInt64)
		bAmt := math.MaxInt64 - a.AmountMinor() + (r.Int64N(5) - 2)
		if bAmt < 0 {
			continue
		}
		checkAdd(t, i, a, mustMoney(t, bAmt))
	}
}

func checkAdd(t *testing.T, i int, a, b Money) {
	t.Helper()
	sum, err := a.Add(b)
	x, y := a.AmountMinor(), b.AmountMinor()
	if !sumFitsInt64(x, y) {
		if !errors.Is(err, ErrOverflow) {
			t.Fatalf("iter %d: want ErrOverflow for %d+%d, got err=%v sum=%v", i, x, y, err, sum)
		}
		return
	}
	if err != nil {
		t.Fatalf("iter %d: %d+%d unexpected error: %v", i, x, y, err)
	}
	if sum.AmountMinor() != x+y {
		t.Fatalf("iter %d: %d+%d = %d", i, x, y, sum.AmountMinor())
	}
}

func TestProperty_UnderflowNearBoundary(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	for i := 0; i < 2000; i++ {
		a := randMoney(t, r, math.MaxInt64/2)
		bAmt := a.AmountMinor() + (r.Int64N(5) - 2)
		if i%2 == 0 { // alternate with fully random b
			bAmt = r.Int64N(math.MaxInt64 / 2)
		}
		if bAmt < 0 {
			continue
		}
		b := mustMoney(t, bAmt)
		diff, err := a.Sub(b)
		if bAmt > a.AmountMinor() {
			if !errors.Is(err, ErrUnderflow) {
				t.Fatalf("iter %d: want ErrUnderflow for %d-%d, got err=%v", i, a.AmountMinor(), bAmt, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("iter %d: %d-%d unexpected error: %v", i, a.AmountMinor(), bAmt, err)
		}
		if diff.AmountMinor() != a.AmountMinor()-bAmt {
			t.Fatalf("iter %d: diff mismatch", i)
		}
	}
}

// Native fuzzing. Plain `go test` runs only the seed corpus below;
// `go test -fuzz=FuzzAddSub ./internal/domain/` explores further.
func FuzzAddSub(f *testing.F) {
	f.Add(int64(0), int64(0))
	f.Add(int64(math.MaxInt64), int64(1))
	f.Add(int64(math.MaxInt64-1), int64(1))
	f.Add(int64(5), int64(5))
	f.Add(int64(5), int64(6))
	f.Fuzz(func(t *testing.T, x, y int64) {
		if x < 0 || y < 0 {
			t.Skip()
		}
		a, b := mustMoney(t, x), mustMoney(t, y)
		checkAdd(t, 0, a, b)
		if sumFitsInt64(x, y) {
			sum, _ := a.Add(b)
			if back, err := sum.Sub(b); err != nil || back != a {
				t.Fatalf("(%d+%d)-%d = %v, %v", x, y, y, back, err)
			}
		}
		diff, err := a.Sub(b)
		if y > x {
			if !errors.Is(err, ErrUnderflow) {
				t.Fatalf("%d-%d: want ErrUnderflow, got %v", x, y, err)
			}
		} else if err != nil || diff.AmountMinor() != x-y {
			t.Fatalf("%d-%d = %v, %v", x, y, diff, err)
		}
	})
}

// ---- JSON ----

func TestJSON_RoundTrip(t *testing.T) {
	m := mustMoney(t, 12345)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"amount_minor":12345,"currency":"KES"}`; string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
	var got Money
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != m {
		t.Fatalf("round trip: got %+v, want %+v", got, m)
	}
}

func TestJSON_InsideStruct(t *testing.T) {
	type resp struct {
		Total Money `json:"total"`
	}
	b, err := json.Marshal(resp{Total: mustMoney(t, 50)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"total":{"amount_minor":50,"currency":"KES"}}`; string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
}

func TestJSON_ZeroValueRefusesToMarshal(t *testing.T) {
	if _, err := json.Marshal(Money{}); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("want ErrInvalidMoney, got %v", err)
	}
}

func TestJSON_UnmarshalCannotBypassValidation(t *testing.T) {
	bad := map[string]string{
		"empty object":         `{}`,
		"null":                 `null`,
		"negative":             `{"amount_minor":-1,"currency":"KES"}`,
		"unsupported currency": `{"amount_minor":1,"currency":"USD"}`,
		"missing currency":     `{"amount_minor":1}`,
		"fractional amount":    `{"amount_minor":1.5,"currency":"KES"}`,
		"overflowing amount":   `{"amount_minor":9223372036854775808,"currency":"KES"}`,
	}
	for name, in := range bad {
		t.Run(name, func(t *testing.T) {
			var m Money
			if err := json.Unmarshal([]byte(in), &m); err == nil {
				t.Fatalf("Unmarshal(%s) succeeded: %+v", in, m)
			}
			if m != (Money{}) {
				t.Fatalf("failed Unmarshal must leave the zero value, got %+v", m)
			}
		})
	}
}
