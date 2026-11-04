package domain

import (
"errors"
"fmt"
"math"
)

var (
ErrInvalidMoney     = errors.New("domain: money has no currency (zero value)")
ErrInvalidCurrency  = errors.New("domain: unsupported currency")
ErrNegativeAmount   = errors.New("domain: money amount must be non-negative")
ErrCurrencyMismatch = errors.New("domain: money currency mismatch")
ErrOverflow         = errors.New("domain: money addition overflows int64")
ErrUnderflow        = errors.New("domain: money subtraction would go negative")
)

type Currency string

const KES Currency = "KES"

var supportedCurrencies = map[Currency]bool{KES: true}

type Money struct {
amountMinor int64
currency    Currency
}

func New(amountMinor int64, c Currency) (Money, error) {
if !supportedCurrencies[c] {
return Money{}, fmt.Errorf("%w: %q", ErrInvalidCurrency, c)
}
if amountMinor < 0 {
return Money{}, fmt.Errorf("%w: %d", ErrNegativeAmount, amountMinor)
}
return Money{amountMinor: amountMinor, currency: c}, nil
}

func Zero(c Currency) (Money, error) { return New(0, c) }

func (m Money) AmountMinor() int64 { return m.amountMinor }
func (m Money) Currency() Currency { return m.currency }
func (m Money) IsValid() bool      { return m.currency != "" }

func (m Money) Add(other Money) (Money, error) {
if err := m.validatePair(other); err != nil {
return Money{}, err
}
if m.amountMinor > math.MaxInt64-other.amountMinor {
return Money{}, ErrOverflow
}
return Money{m.amountMinor + other.amountMinor, m.currency}, nil
}

func (m Money) Sub(other Money) (Money, error) {
if err := m.validatePair(other); err != nil {
return Money{}, err
}
if other.amountMinor > m.amountMinor {
return Money{}, ErrUnderflow
}
return Money{m.amountMinor - other.amountMinor, m.currency}, nil
}

func (m Money) validatePair(other Money) error {
if !m.IsValid() || !other.IsValid() {
return ErrInvalidMoney
}
if m.currency != other.currency {
return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
}
return nil
}
