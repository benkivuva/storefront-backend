package service

import (
	"context"
	"fmt"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type PaymentService struct {
	orders   domain.OrderRepository
	payments domain.PaymentRepository
	provider domain.PaymentProvider
	clock    domain.Clock
}

func NewPaymentService(
	orders domain.OrderRepository,
	payments domain.PaymentRepository,
	provider domain.PaymentProvider,
	clock domain.Clock,
) *PaymentService {
	return &PaymentService{
		orders:   orders,
		payments: payments,
		provider: provider,
		clock:    clock,
	}
}

func (s *PaymentService) InitiatePayment(ctx context.Context, orderID domain.ID, method domain.PaymentMethod, customerPhone string) (domain.InitiateResult, error) {
	// 1. Fetch Order
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return domain.InitiateResult{}, fmt.Errorf("fetch order: %w", err)
	}

	// 2. Validate Order State
	if order.Status() != domain.StatusPendingPayment {
		return domain.InitiateResult{}, fmt.Errorf("order is not pending payment (current: %s)", order.Status())
	}

	now := s.clock.Now()
	if now.After(order.PaymentExpiresAt()) {
		return domain.InitiateResult{}, fmt.Errorf("order has expired")
	}

	// 3. Create Payment Record
	paymentID := domain.ID{} // In a real impl, use an ID generator here
	// Use a deterministic ID for now (we'll inject a generator later)
	copy(paymentID[:], []byte(fmt.Sprintf("%d", time.Now().UnixNano())))

	payment, err := domain.NewPayment(paymentID, order.ID(), s.provider.Name(), method, order.TotalMinor(), string(order.DeliveryFee().Currency()), now)
	if err != nil {
		return domain.InitiateResult{}, fmt.Errorf("create payment: %w", err)
	}

	if err := s.payments.Create(ctx, payment); err != nil {
		return domain.InitiateResult{}, fmt.Errorf("save payment: %w", err)
	}

	// 4. Call Provider
	req := domain.InitiateRequest{
		PaymentID:     payment.ID(),
		OrderID:       order.ID(),
		AmountMinor:   order.TotalMinor(),
		Currency:      string(order.DeliveryFee().Currency()),
		Method:        method,
		CustomerPhone: customerPhone,
		Reference:     order.ID().String(),
	}

	result, err := s.provider.Initiate(ctx, req)
	if err != nil {
		// Provider failed: mark the payment record as FAILED
		_ = payment.MarkFailed(err.Error(), s.clock.Now())
		_ = s.payments.UpdateStatus(ctx, payment)
		return domain.InitiateResult{}, fmt.Errorf("provider initiate failed: %w", err)
	}

	// 5. Update Payment to PENDING
	if err := payment.MarkPending(result.ProviderRef, s.clock.Now()); err != nil {
		return domain.InitiateResult{}, fmt.Errorf("transition to pending: %w", err)
	}
	if err := s.payments.UpdateStatus(ctx, payment); err != nil {
		return domain.InitiateResult{}, fmt.Errorf("save pending payment: %w", err)
	}

	return result, nil
}
