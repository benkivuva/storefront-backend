package service

import (
	"context"
	"fmt"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type PaymentService struct {
	orders   domain.OrderRepository
	payments domain.PaymentRepository
	provider domain.PaymentProvider
	ids      domain.IDGenerator
	clock    domain.Clock
}

func NewPaymentService(
	orders domain.OrderRepository,
	payments domain.PaymentRepository,
	provider domain.PaymentProvider,
	ids domain.IDGenerator,
	clock domain.Clock,
) *PaymentService {
	return &PaymentService{
		orders:   orders,
		payments: payments,
		provider: provider,
		ids:      ids,
		clock:    clock,
	}
}

func (s *PaymentService) InitiatePayment(ctx context.Context, orderID domain.ID, method domain.PaymentMethod, customerPhone string) (domain.InitiateResult, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return domain.InitiateResult{}, fmt.Errorf("fetch order: %w", err)
	}

	if order.Status() != domain.StatusPendingPayment {
		return domain.InitiateResult{}, fmt.Errorf("order is not pending payment (current: %s)", order.Status())
	}

	now := s.clock.Now()
	if now.After(order.PaymentExpiresAt()) {
		return domain.InitiateResult{}, fmt.Errorf("order has expired")
	}

	paymentID := s.ids.NewID()
	payment, err := domain.NewPayment(paymentID, order.ID(), s.provider.Name(), method, order.TotalMinor(), string(order.DeliveryFee().Currency()), now)
	if err != nil {
		return domain.InitiateResult{}, fmt.Errorf("create payment: %w", err)
	}

	if err := s.payments.Create(ctx, payment); err != nil {
		return domain.InitiateResult{}, fmt.Errorf("save payment: %w", err)
	}

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
		_ = payment.MarkFailed(err.Error(), s.clock.Now())
		_ = s.payments.UpdateStatus(ctx, payment)
		return domain.InitiateResult{}, fmt.Errorf("provider initiate failed: %w", err)
	}

	if err := payment.MarkPending(result.ProviderRef, s.clock.Now()); err != nil {
		return domain.InitiateResult{}, fmt.Errorf("transition to pending: %w", err)
	}
	if err := s.payments.UpdateStatus(ctx, payment); err != nil {
		return domain.InitiateResult{}, fmt.Errorf("save pending payment: %w", err)
	}

	return result, nil
}
