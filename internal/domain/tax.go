package domain

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrInvalidRateBp = errors.New("domain: tax rate must be in [0, 10000] basis points")
	ErrTaxOverflow   = errors.New("domain: tax extraction overflows int64")
)

const maxRateBp int64 = 10_000

// ExtractVAT splits a VAT-inclusive gross into net and VAT using half-up
// rounding, per architecture §5.1:
//
// vat = round_half_up(gross * rateBp / (10000 + rateBp))
//
// The gross is the shelf price (VAT-inclusive, per A2); VAT is extracted,
// never added. rateBp is in basis points: 1600 = 16%, 0 = zero-rated/exempt.
// Rates live in the tax_rates table — this function knows the math, not the
// tax classes.
//
// On error, both return values are the zero Money.
func ExtractVAT(gross Money, rateBp int) (net, vat Money, err error) {
	if !gross.IsValid() {
		return Money{}, Money{}, ErrInvalidMoney
	}
	r := int64(rateBp)
	if r < 0 || r > maxRateBp {
		return Money{}, Money{}, fmt.Errorf("%w: %d", ErrInvalidRateBp, rateBp)
	}
	if r == 0 {
		z, err := Zero(gross.Currency())
		if err != nil {
			return Money{}, Money{}, err
		}
		return gross, z, nil
	}

	g := gross.AmountMinor()
	d := maxRateBp + r
	half := d / 2

	// Exact overflow check for g*r + half <= MaxInt64. Not conservative:
	// g > (MaxInt64 - half)/r iff g*r + half > MaxInt64, because g and r are
	// integers and (MaxInt64 - half)/r floors.
	if g > (math.MaxInt64-half)/r {
		return Money{}, Money{}, fmt.Errorf("%w: gross=%d rateBp=%d", ErrTaxOverflow, g, rateBp)
	}

	vatAmt := (g*r + half) / d

	vatMoney, err := New(vatAmt, gross.Currency())
	if err != nil {
		return Money{}, Money{}, err
	}
	netMoney, err := gross.Sub(vatMoney)
	if err != nil {
		return Money{}, Money{}, fmt.Errorf("domain: tax extraction produced vat > gross: %w", err)
	}
	return netMoney, vatMoney, nil
}
