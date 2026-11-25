package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type PaymentService struct {
	orders      domain.OrderRepository
	payments    domain.PaymentRepository
	webhookRepo domain.WebhookRepository
	provider    domain.PaymentProvider
	ids         domain.IDGenerator
	clock       domain.Clock
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

func (s *PaymentService) SetWebhookRepo(repo domain.WebhookRepository) {
	s.webhookRepo = repo
}

func (s *PaymentService) InitiatePayment(ctx context.Context, userID, orderID domain.ID, method domain.PaymentMethod, customerPhone string) (domain.InitiateResult, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return domain.InitiateResult{}, fmt.Errorf("fetch order: %w", err)
	}

	if order.UserID() != userID {
		return domain.InitiateResult{}, fmt.Errorf("%w: order does not belong to user", domain.ErrNotFound)
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

func (s *PaymentService) ProcessWebhook(ctx context.Context, rawBody []byte, headers map[string]string) error {
	evt, err := s.provider.VerifyWebhook(ctx, rawBody, headers)
	if err != nil {
		return fmt.Errorf("verify webhook: %w", err)
	}

	now := s.clock.Now()
	webhookRec := domain.WebhookRecord{
		ID:              s.ids.NewID(),
		Provider:        s.provider.Name(),
		ProviderEventID: evt.ProviderEventID,
		SignatureValid:  evt.SignatureValid,
		RawBody:         rawBody,
		ReceivedAt:      now,
	}

	if !evt.SignatureValid {
		_ = s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, nil, nil, nil)
		return errors.New("invalid webhook signature")
	}

	processed, err := s.webhookRepo.IsProcessed(ctx, s.provider.Name(), evt.ProviderEventID)
	if err != nil {
		return fmt.Errorf("check idempotency: %w", err)
	}
	if processed {
		return nil
	}

	payment, err := s.payments.GetByProviderRef(ctx, s.provider.Name(), evt.ProviderRef)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("payment not found for provider ref %s", evt.ProviderRef)
		}
		return fmt.Errorf("fetch payment: %w", err)
	}

	if payment.Status() == domain.PaymentSucceeded {
		return s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, nil, nil, nil)
	}

	if payment.AmountMinor() != evt.AmountMinor || payment.Currency() != evt.Currency {
		_ = payment.MarkFailed("amount/currency mismatch from webhook", now)
		_ = s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, payment, nil, nil)
		return fmt.Errorf("webhook amount mismatch: expected %d %s, got %d %s",
			payment.AmountMinor(), payment.Currency(), evt.AmountMinor, evt.Currency)
	}

	order, err := s.orders.GetByID(ctx, payment.OrderID())
	if err != nil {
		return fmt.Errorf("fetch order for payment: %w", err)
	}

	switch evt.Status {
	case domain.PaymentSucceeded:
		if order.Status() == domain.StatusCancelled {
			_ = payment.MarkOrphaned("payment arrived after order cancellation", now)
			payload, _ := json.Marshal(map[string]any{
				"payment_id": payment.ID().String(),
				"order_id":   order.ID().String(),
				"amount":     payment.AmountMinor(),
			})
			outbox := domain.OutboxEvent{
				ID:            s.ids.NewID(),
				AggregateType: "payment",
				AggregateID:   payment.ID(),
				EventType:     "PaymentOrphaned",
				Payload:       payload,
				CreatedAt:     now,
			}
			return s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, payment, nil, &outbox)
		}

		if order.Status() == domain.StatusPendingPayment {
			_ = payment.MarkSucceeded(evt.ProviderTxnID, now)
			if err := order.Transition(domain.StatusPaid, "payment_webhook", "provider confirmed", now); err != nil {
				return fmt.Errorf("order transition: %w", err)
			}

			payload, _ := json.Marshal(map[string]any{
				"order_id":    order.ID().String(),
				"payment_id":  payment.ID().String(),
				"total_minor": order.TotalMinor(),
			})
			outbox := domain.OutboxEvent{
				ID:            s.ids.NewID(),
				AggregateType: "order",
				AggregateID:   order.ID(),
				EventType:     "OrderPaid",
				Payload:       payload,
				CreatedAt:     now,
			}
			return s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, payment, order, &outbox)
		}

	case domain.PaymentFailed:
		_ = payment.MarkFailed(evt.FailureReason, now)
		return s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, payment, nil, nil)
	}

	return nil
}

func (s *PaymentService) ReapExpiredOrders(ctx context.Context) (int, error) {
	now := s.clock.Now()
	orders, err := s.orders.ListExpiredPending(ctx, now, 100)
	if err != nil {
		return 0, fmt.Errorf("list expired orders: %w", err)
	}

	slog.Info("reaper: found expired orders", "count", len(orders))

	reaped := 0
	for _, order := range orders {
		payment, err := s.payments.GetLatestByOrderID(ctx, order.ID())
		if err != nil {
			slog.Warn("reaper: no payment found for order", "order_id", order.ID().String(), "error", err)
			continue
		}

		if payment.Status() == domain.PaymentPending {
			statusEvt, err := s.provider.Status(ctx, payment.ProviderRef())
			if err != nil {
				slog.Warn("reaper: provider status check failed, skipping", "order_id", order.ID().String(), "error", err)
				continue
			}

			if statusEvt.Status == domain.PaymentSucceeded {
				if payment.AmountMinor() == statusEvt.AmountMinor {
					_ = payment.MarkSucceeded(statusEvt.ProviderTxnID, now)
					if err := order.Transition(domain.StatusPaid, "reaper", "late provider confirmation", now); err != nil {
						slog.Error("reaper: order transition failed", "error", err)
						continue
					}
					payload, _ := json.Marshal(map[string]any{
						"order_id":    order.ID().String(),
						"payment_id":  payment.ID().String(),
						"total_minor": order.TotalMinor(),
					})
					outbox := domain.OutboxEvent{
						ID:            s.ids.NewID(),
						AggregateType: "order",
						AggregateID:   order.ID(),
						EventType:     "OrderPaid",
						Payload:       payload,
						CreatedAt:     now,
					}
					webhookRec := domain.WebhookRecord{
						ID: s.ids.NewID(), Provider: s.provider.Name(),
						ProviderEventID: "reaper-" + order.ID().String(),
						SignatureValid:  true,
						RawBody:         []byte("{}"),
						ReceivedAt:      now,
					}
					if err := s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, payment, order, &outbox); err != nil {
						slog.Error("reaper: ProcessWebhookTx (paid) failed", "order_id", order.ID().String(), "error", err)
					} else {
						reaped++
					}
					continue
				}
			}
		}

		_ = payment.MarkExpired(now)
		if err := order.Transition(domain.StatusCancelled, "reaper", "payment expired", now); err != nil {
			slog.Error("reaper: order cancel transition failed", "order_id", order.ID().String(), "error", err)
			continue
		}

		payload, _ := json.Marshal(map[string]any{
			"order_id": order.ID().String(),
			"reason":   "payment_expired",
		})
		outbox := domain.OutboxEvent{
			ID:            s.ids.NewID(),
			AggregateType: "order",
			AggregateID:   order.ID(),
			EventType:     "OrderCancelled",
			Payload:       payload,
			CreatedAt:     now,
		}

		webhookRec := domain.WebhookRecord{
			ID: s.ids.NewID(), Provider: s.provider.Name(),
			ProviderEventID: "reaper-cancel-" + order.ID().String(),
			SignatureValid:  true,
			RawBody:         []byte("{}"),
			ReceivedAt:      now,
		}

		if err := s.webhookRepo.ProcessWebhookTx(ctx, webhookRec, payment, order, &outbox); err != nil {
			slog.Error("reaper: ProcessWebhookTx (cancel) failed", "order_id", order.ID().String(), "error", err)
		} else {
			reaped++
		}
	}

	return reaped, nil
}
