package domain

import (
	"errors"
	"testing"
	"time"
)

func TestOrder_CreationAndTotals(t *testing.T) {
	userID := mustID(t, sampleID)
	addrID := mustID(t, otherID)
	now := time.Now().UTC()
	expireAt := now.Add(15 * time.Minute)

	price1, _ := New(11600, KES)
	vat1, _ := New(1600, KES)

	item1 := OrderItemSnapshot{
		ProductID:      mustID(t, "11111111-1111-7111-8111-111111111111"),
		SKU:            "SKU1",
		ProductName:    "Widget",
		Quantity:       2,
		UnitPrice:      price1,
		TaxClass:       TaxStandard16,
		TaxRateBp:      1600,
		LineVat:        vat1,
		LineGrossMinor: 23200,
	}

	o, err := NewOrder(
		mustID(t, "33333333-3333-7333-8333-333333333333"),
		userID,
		addrID,
		[]OrderItemSnapshot{item1},
		price1, // delivery fee (using same money for simplicity in test)
		now,
		expireAt,
	)
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}

	if o.Status() != StatusPendingPayment {
		t.Fatalf("expected PENDING_PAYMENT, got %s", o.Status())
	}
	if o.SubtotalMinor() != 23200 {
		t.Fatalf("expected subtotal 23200, got %d", o.SubtotalMinor())
	}
	if o.TotalMinor() != 34800 { // 23200 (items) + 11600 (delivery)
		t.Fatalf("expected total 34800, got %d", o.TotalMinor())
	}
	if len(o.Items()) != 1 || o.Items()[0].UnitPrice.AmountMinor() != 11600 {
		t.Fatalf("item snapshot incorrect")
	}
}

func TestOrder_StateTransitions(t *testing.T) {
	o := mustNewOrder(t)

	// Legal transition
	if err := o.Transition(StatusPaid, "system", "payment confirmed", time.Now()); err != nil {
		t.Fatalf("Transition to PAID: %v", err)
	}
	if o.Status() != StatusPaid {
		t.Fatalf("expected PAID")
	}

	// Illegal transition
	if err := o.Transition(StatusPendingPayment, "system", "revert", time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}

	// Legal transition
	if err := o.Transition(StatusFulfilling, "admin", "processing", time.Now()); err != nil {
		t.Fatalf("Transition to FULFILLING: %v", err)
	}

	// History check
	if len(o.History()) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(o.History()))
	}
}

func TestOrder_TerminalStates(t *testing.T) {
	// Cancelled is terminal
	cancelled := mustNewOrder(t)
	_ = cancelled.Transition(StatusCancelled, "user", "changed mind", time.Now())
	if err := cancelled.Transition(StatusPaid, "system", "", time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition from CANCELLED, got %v", err)
	}

	// Refunded is terminal
	refunded := mustNewOrder(t)
	_ = refunded.Transition(StatusPaid, "system", "", time.Now())
	_ = refunded.Transition(StatusRefundPending, "user", "", time.Now())
	_ = refunded.Transition(StatusRefunded, "system", "", time.Now())
	if err := refunded.Transition(StatusFulfilling, "admin", "", time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition from REFUNDED, got %v", err)
	}
}

// Helper to create a valid base order
func mustNewOrder(t *testing.T) *Order {
	t.Helper()
	userID := mustID(t, sampleID)
	addrID := mustID(t, otherID)
	now := time.Now().UTC()
	price, _ := New(1000, KES)
	vat, _ := New(100, KES)

	// Must provide at least one item now
	items := []OrderItemSnapshot{
		{
			ProductID:      mustID(t, "11111111-1111-7111-8111-111111111111"),
			SKU:            "SKU-TEST",
			ProductName:    "Test Product",
			Quantity:       1,
			UnitPrice:      price,
			TaxClass:       TaxStandard16,
			TaxRateBp:      1600,
			LineVat:        vat,
			LineGrossMinor: 1000,
		},
	}

	o, err := NewOrder(
		mustID(t, "33333333-3333-7333-8333-333333333333"),
		userID,
		addrID,
		items,
		price,
		now,
		now.Add(15*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}
