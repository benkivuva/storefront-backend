package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type CheckoutRepo struct {
	pool *pgxpool.Pool
	ids  domain.IDGenerator
}

func NewCheckoutRepo(pool *pgxpool.Pool, ids domain.IDGenerator) *CheckoutRepo {
	return &CheckoutRepo{pool: pool, ids: ids}
}

type orderCreatedEvent struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Status           string    `json:"status"`
	SubtotalMinor    int64     `json:"subtotal_minor"`
	DeliveryFeeMinor int64     `json:"delivery_fee_minor"`
	TaxMinor         int64     `json:"tax_minor"`
	TotalMinor       int64     `json:"total_minor"`
	Currency         string    `json:"currency"`
	ItemCount        int       `json:"item_count"`
	CreatedAt        time.Time `json:"created_at"`
	TraceContext     string    `json:"trace_context,omitempty"`
}

const maxSerializationAttempts = 3

// Execute runs the checkout transaction, retrying serialization failures and
// deadlocks with bounded backoff. PostgreSQL may abort either error class under
// concurrent stock updates; both are safe to replay because the whole
// transaction rolls back.
func (r *CheckoutRepo) Execute(ctx context.Context, cartID domain.ID, o *domain.Order, idempotencyKey string) error {
	var err error
	for attempt := 0; attempt < maxSerializationAttempts; attempt++ {
		err = r.executeOnce(ctx, cartID, o, idempotencyKey)
		if err == nil || !isRetryableTxError(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
	return err
}

func isRetryableTxError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

func (r *CheckoutRepo) executeOnce(ctx context.Context, cartID domain.ID, o *domain.Order, idempotencyKey string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("checkout tx begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE carts SET status = 'CONVERTED', updated_at = now() WHERE id = $1 AND status = 'ACTIVE'`, toUUID(cartID))
	if err != nil || tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}

	var orderNumber int64
	err = tx.QueryRow(ctx, `
INSERT INTO orders (
id, user_id, shipping_address_id, status,
subtotal_minor, delivery_fee_minor, tax_minor, total_minor,
currency, payment_expires_at, idempotency_key, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING order_number`,
		toUUID(o.ID()), toUUID(o.UserID()), toUUID(o.AddressID()), string(o.Status()),
		o.SubtotalMinor(), o.DeliveryFee().AmountMinor(), o.TaxMinor(), o.TotalMinor(),
		string(o.DeliveryFee().Currency()), o.PaymentExpiresAt(), idempotencyKey,
		o.CreatedAt(), o.UpdatedAt()).Scan(&orderNumber)
	if err != nil {
		return fmt.Errorf("order insert: %w", err)
	}
	o.SetOrderNumber(orderNumber)

	items := o.Items()
	sort.Slice(items, func(i, j int) bool {
		return items[i].ProductID.String() < items[j].ProductID.String()
	})

	for _, item := range items {
		_, err = tx.Exec(ctx, `
INSERT INTO order_items (
id, order_id, product_id, sku, product_name, quantity,
unit_price_minor, tax_class, tax_rate_bp, line_gross_minor, line_vat_minor
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			toUUID(r.ids.NewID()), toUUID(o.ID()), toUUID(item.ProductID),
			item.SKU, item.ProductName, item.Quantity,
			item.UnitPrice.AmountMinor(), string(item.TaxClass), item.TaxRateBp,
			item.LineGrossMinor, item.LineVat.AmountMinor())
		if err != nil {
			return fmt.Errorf("order item insert: %w", err)
		}

		tag, err = tx.Exec(ctx, `
UPDATE inventory SET available = available - $2, updated_at = $3
WHERE product_id = $1 AND available >= $2`,
			toUUID(item.ProductID), item.Quantity, o.CreatedAt())
		if err != nil || tag.RowsAffected() == 0 {
			return domain.ErrConflict
		}

		_, err = tx.Exec(ctx, `
INSERT INTO stock_movements (id, product_id, delta, reason, order_id, created_at)
VALUES ($1, $2, $3, 'ORDER_RESERVE', $4, $5)`,
			toUUID(r.ids.NewID()), toUUID(item.ProductID), -item.Quantity, toUUID(o.ID()), o.CreatedAt())
		if err != nil {
			return fmt.Errorf("stock movement insert: %w", err)
		}
	}

	for _, h := range o.History() {
		_, _ = tx.Exec(ctx, `INSERT INTO order_status_history (order_id, from_status, to_status, actor, reason, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			toUUID(o.ID()), string(h.From), string(h.To), h.Actor, h.Reason, h.At)
	}

	traceCtx := ""
	if tp, ok := ctx.Value(domain.TraceparentKey).(string); ok && tp != "" {
		b, _ := json.Marshal(map[string]string{"traceparent": tp})
		traceCtx = string(b)
	}

	payload, err := json.Marshal(orderCreatedEvent{
		ID:               o.ID().String(),
		UserID:           o.UserID().String(),
		Status:           string(o.Status()),
		SubtotalMinor:    o.SubtotalMinor(),
		DeliveryFeeMinor: o.DeliveryFee().AmountMinor(),
		TaxMinor:         o.TaxMinor(),
		TotalMinor:       o.TotalMinor(),
		Currency:         string(o.DeliveryFee().Currency()),
		ItemCount:        len(items),
		CreatedAt:        o.CreatedAt(),
		TraceContext:     traceCtx,
	})
	if err != nil {
		return fmt.Errorf("marshal order event: %w", err)
	}

	_, err = tx.Exec(ctx, `
INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, payload, trace_context, created_at)
VALUES ($1, 'order', $2, 'OrderCreated', $3, nullif($4, '')::jsonb, $5)`,
		toUUID(r.ids.NewID()), toUUID(o.ID()), payload, traceCtx, o.CreatedAt())
	if err != nil {
		return fmt.Errorf("outbox insert: %w", err)
	}

	return tx.Commit(ctx)
}
