package payments

import (
	"fmt"

	"github.com/benkivuva/storefront-backend/internal/adapter/payments/mock"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

// New returns the configured payment provider. Sprint 6 ships only the mock
// adapter; the registry is the single place a Paystack or Daraja adapter is
// registered so call sites never branch on provider names.
func New(name string) (domain.PaymentProvider, error) {
	switch name {
	case "", "mock":
		return mock.NewProvider(), nil
	case "paystack", "daraja":
		return nil, fmt.Errorf("payment provider %q is not wired in this build", name)
	default:
		return nil, fmt.Errorf("unknown payment provider %q", name)
	}
}

// Supported reports the provider names this build can construct.
func Supported() []string { return []string{"mock"} }
