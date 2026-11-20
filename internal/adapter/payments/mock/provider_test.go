package mock_test

import (
	"context"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/adapter/payments/mock"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

var (
	testPaymentID = domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	testOrderID   = domain.ID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}
)

func TestMockProvider_Initiate(t *testing.T) {
	provider := mock.NewProvider()

	req := domain.InitiateRequest{
		PaymentID:   testPaymentID,
		OrderID:     testOrderID,
		AmountMinor: 10000,
		Currency:    "KES",
		Method:      domain.MethodCard,
		Reference:   "ref-123",
	}

	res, err := provider.Initiate(context.Background(), req)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if res.ProviderRef == "" {
		t.Error("expected provider ref")
	}
	if res.NextAction.Type != "redirect" {
		t.Errorf("expected redirect, got %s", res.NextAction.Type)
	}
}

func TestMockProvider_SimulateOutcomes(t *testing.T) {
	provider := mock.NewProvider()

	t.Run("success", func(t *testing.T) {
		evt, _ := provider.Status(context.Background(), "success-ref")
		if evt.Status != domain.PaymentSucceeded {
			t.Errorf("expected SUCCEEDED, got %s", evt.Status)
		}
	})

	t.Run("failure", func(t *testing.T) {
		evt, _ := provider.Status(context.Background(), "fail-ref")
		if evt.Status != domain.PaymentFailed {
			t.Errorf("expected FAILED, got %s", evt.Status)
		}
		if evt.FailureReason == "" {
			t.Error("expected failure reason")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		_, err := provider.Status(context.Background(), "timeout-ref")
		if err == nil {
			t.Error("expected timeout error")
		}
	})
}
