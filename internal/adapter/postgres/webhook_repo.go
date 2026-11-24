package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type WebhookRepo struct {
	pool *pgxpool.Pool
	ids  domain.IDGenerator
}

func NewWebhookRepo(pool *pgxpool.Pool, ids domain.IDGenerator) *WebhookRepo {
	return &WebhookRepo{pool: pool, ids: ids}
}

func (r *WebhookRepo) IsProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS(
SELECT 1 FROM payment_webhook_events WHERE provider = $1 AND provider_event_id = $2
)`, provider, eventID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check webhook processed: %w", err)
	}
	return exists, nil
}

func (r *WebhookRepo) ProcessWebhookTx(ctx context.Context, webhook domain.WebhookRecord, payment *domain.Payment, order *domain.Order, outbox *domain.OutboxEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("webhook tx begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
INSERT INTO payment_webhook_events (
id, provider, provider_event_id, signature_valid, raw_body, received_at
) VALUES ($1, $2, $3, $4, $5, $6)`,
		toUUID(webhook.ID), webhook.Provider, webhook.ProviderEventID,
		webhook.SignatureValid, webhook.RawBody, webhook.ReceivedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrConflict
		}
		return fmt.Errorf("insert webhook event: %w", err)
	}

	if payment != nil {
		_, err = tx.Exec(ctx, `
UPDATE payments SET 
status = $2, provider_ref = $3, provider_txn_id = $4, failure_reason = $5, updated_at = $6
WHERE id = $1`,
			toUUID(payment.ID()), string(payment.Status()), payment.ProviderRef(),
			payment.ProviderTxnID(), payment.FailureReason(), payment.UpdatedAt())
		if err != nil {
			return fmt.Errorf("update payment: %w", err)
		}
	}

	if order != nil {
		_, err = tx.Exec(ctx, `
UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1`,
			toUUID(order.ID()), string(order.Status()), order.UpdatedAt())
		if err != nil {
			return fmt.Errorf("update order: %w", err)
		}

		hist := order.History()
		if len(hist) > 0 {
			latest := hist[len(hist)-1]
			_, err = tx.Exec(ctx, `
INSERT INTO order_status_history (order_id, from_status, to_status, actor, reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
				toUUID(order.ID()), string(latest.From), string(latest.To),
				latest.Actor, latest.Reason, latest.At)
			if err != nil {
				return fmt.Errorf("insert order history: %w", err)
			}
		}

		if order.Status() == domain.StatusCancelled {
			for _, item := range order.Items() {
				_, err = tx.Exec(ctx, `
UPDATE inventory SET available = available + $2, updated_at = $3 
WHERE product_id = $1`,
					toUUID(item.ProductID), item.Quantity, order.UpdatedAt())
				if err != nil {
					return fmt.Errorf("release stock: %w", err)
				}

				_, err = tx.Exec(ctx, `
INSERT INTO stock_movements (id, product_id, delta, reason, order_id, created_at)
VALUES ($1, $2, $3, 'ORDER_RELEASE', $4, $5)`,
					toUUID(r.ids.NewID()), toUUID(item.ProductID), item.Quantity, toUUID(order.ID()), order.UpdatedAt())
				if err != nil {
					return fmt.Errorf("insert stock movement: %w", err)
				}
			}
		}
	}

	if outbox != nil {
		_, err = tx.Exec(ctx, `
INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, payload, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
			toUUID(outbox.ID), outbox.AggregateType, toUUID(outbox.AggregateID),
			outbox.EventType, outbox.Payload, outbox.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert outbox event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("webhook tx commit: %w", err)
	}

	return nil
}
