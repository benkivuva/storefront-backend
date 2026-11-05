package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// Sentinel errors. Callers match with errors.Is.
var (
	ErrInvalidMoney     = errors.New("domain: money has no currency (zero value)")
	ErrInvalidCurrency  = errors.New("domain: unsupported currency")
	ErrNegativeAmount   = errors.New("domain: money amount must be non-negative")
	ErrCurrencyMismatch = errors.New("domain: money currency mismatch")
	ErrOverflow         = errors.New("domain: money arithmetic overflows int64")
	ErrUnderflow        = errors.New("domain: money subtraction would go negative")
)

// Currency is an ISO 4217 code.
type Currency string

// KES is the Kenyan shilling. Amounts are held in cents (2 decimal places).
const KES Currency = "KES"

// supported is a function, not a package-level map, so nothing can mutate the
// set of supported currencies at runtime.
func (c Currency) supported() bool {
	switch c {
	case KES:
		return true
	default:
		return false
	}
}

// Money is an immutable, non-negative amount in minor units of one currency.
//
// The fields are unexported, so outside this package a Money can only be made
// through New or Zero. The zero value (empty currency) is invalid and is
// rejected by every operation. Money is comparable with ==.
type Money struct {
	amountMinor int64
	currency    Currency
}

// New returns a Money, rejecting unsupported currencies and negative amounts.
func New(amountMinor int64, c Currency) (Money, error) {
	if !c.supported() {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidCurrency, c)
	}
	if amountMinor < 0 {
		return Money{}, fmt.Errorf("%w: %d", ErrNegativeAmount, amountMinor)
	}
	return Money{amountMinor: amountMinor, currency: c}, nil
}

// Zero returns a valid zero amount in currency c.
func Zero(c Currency) (Money, error) { return New(0, c) }

// AmountMinor returns the amount in minor units (cents for KES).
func (m Money) AmountMinor() int64 { return m.amountMinor }

// Currency returns the currency code ("" for the invalid zero value).
func (m Money) Currency() Currency { return m.currency }

// IsValid reports whether m was built by New/Zero (it has a currency).
func (m Money) IsValid() bool { return m.currency != "" }

// Add returns m + other, failing on the zero value, a currency mismatch, or
// int64 overflow.
func (m Money) Add(other Money) (Money, error) {
	if err := m.validatePair(other); err != nil {
		return Money{}, err
	}
	if m.amountMinor > math.MaxInt64-other.amountMinor {
		return Money{}, ErrOverflow
	}
	return Money{amountMinor: m.amountMinor + other.amountMinor, currency: m.currency}, nil
}

// Sub returns m - other, failing on the zero value, a currency mismatch, or
// if the result would be negative.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.validatePair(other); err != nil {
		return Money{}, err
	}
	if other.amountMinor > m.amountMinor {
		return Money{}, ErrUnderflow
	}
	return Money{amountMinor: m.amountMinor - other.amountMinor, currency: m.currency}, nil
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

type moneyJSON struct {
	AmountMinor int64    `json:"amount_minor"`
	Currency    Currency `json:"currency"`
}

// MarshalJSON encodes {"amount_minor":N,"currency":"KES"}. Without this,
// encoding/json would silently emit {} because the fields are unexported.
// The invalid zero value refuses to marshal rather than emit bad data.
func (m Money) MarshalJSON() ([]byte, error) {
	if !m.IsValid() {
		return nil, ErrInvalidMoney
	}
	return json.Marshal(moneyJSON{AmountMinor: m.amountMinor, Currency: m.currency})
}

// UnmarshalJSON decodes through New, so JSON can never bypass validation.
func (m *Money) UnmarshalJSON(data []byte) error {
	var raw moneyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	v, err := New(raw.AmountMinor, raw.Currency)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
