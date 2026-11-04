package domain

import (
"errors"
"math"
"math/rand/v2"
"testing"
)

func randMoney(t *testing.T, r *rand.Rand, upper int64) Money {
t.Helper()
m, err := New(r.Int64N(upper), KES)
if err != nil {
t.Fatalf("randMoney(%d): %v", upper, err)
}
return m
}

func TestNew_RejectsNegative(t *testing.T) {
if _, err := New(-1, KES); !errors.Is(err, ErrNegativeAmount) {
t.Fatalf("want ErrNegativeAmount, got %v", err)
}
}

func TestNew_RejectsUnsupportedCurrency(t *testing.T) {
for _, c := range []Currency{"", "USD", "kes"} {
if _, err := New(100, c); !errors.Is(err, ErrInvalidCurrency) {
t.Errorf("New(100, %q): want ErrInvalidCurrency, got %v", c, err)
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
good, _ := New(100, KES)
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

func TestCurrencyMismatch(t *testing.T) {
kes, _ := New(100, KES)
usd := Money{amountMinor: 100, currency: "USD"}
if _, err := kes.Add(usd); !errors.Is(err, ErrCurrencyMismatch) {
t.Fatalf("Add: want ErrCurrencyMismatch, got %v", err)
}
if _, err := kes.Sub(usd); !errors.Is(err, ErrCurrencyMismatch) {
t.Fatalf("Sub: want ErrCurrencyMismatch, got %v", err)
}
}

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

func TestProperty_OverflowDetected(t *testing.T) {
r := rand.New(rand.NewPCG(5, 6))
for i := 0; i < 1000; i++ {
a := randMoney(t, r, math.MaxInt64)
b := randMoney(t, r, math.MaxInt64)
sum, err := a.Add(b)
if a.AmountMinor() > math.MaxInt64-b.AmountMinor() {
if !errors.Is(err, ErrOverflow) {
t.Fatalf("iter %d: want ErrOverflow for %d+%d, got err=%v sum=%v",
i, a.AmountMinor(), b.AmountMinor(), err, sum)
}
} else {
if err != nil {
t.Fatalf("iter %d: unexpected: %v", i, err)
}
if sum.AmountMinor() != a.AmountMinor()+b.AmountMinor() {
t.Fatalf("iter %d: sum mismatch", i)
}
}
}
}

func TestProperty_UnderflowDetected(t *testing.T) {
r := rand.New(rand.NewPCG(9, 10))
for i := 0; i < 1000; i++ {
a := randMoney(t, r, math.MaxInt64/2)
b := randMoney(t, r, math.MaxInt64/2)
diff, err := a.Sub(b)
if b.AmountMinor() > a.AmountMinor() {
if !errors.Is(err, ErrUnderflow) {
t.Fatalf("iter %d: want ErrUnderflow for %d-%d, got err=%v",
i, a.AmountMinor(), b.AmountMinor(), err)
}
} else {
if err != nil {
t.Fatalf("iter %d: unexpected: %v", i, err)
}
if diff.AmountMinor() != a.AmountMinor()-b.AmountMinor() {
t.Fatalf("iter %d: diff mismatch", i)
}
}
}
}
