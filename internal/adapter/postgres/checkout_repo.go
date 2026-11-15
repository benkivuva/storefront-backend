package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

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

func (r *CheckoutRepo) Execute(ctx context.Context, cartID domain.ID, o *domain.Order, idempotencyKey string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("checkout tx begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Convert Cart
	tag, err := tx.Exec(ctx, `UPDATE carts SET status = 'CONVERTED', updated_at = now() WHERE id = $1 AND status = 'ACTIVE'`, toUUID(cartID))
	if err != nil || tag.RowsAffected() == 0 {
		return domain.ErrConflict // Cart not active or missing
	}

	// 2. Insert Order
	_, err = tx.Exec(ctx, `
INSERT INTO orders (
id, user_id, shipping_address_id, status,
subtotal_minor, delivery_fee_minor, tax_minor, total_minor,
currency, payment_expires_at, idempotency_key, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		toUUID(o.ID()), toUUID(o.UserID()), toUUID(o.AddressID()), string(o.Status()),
		o.SubtotalMinor(), o.DeliveryFee().AmountMinor(), o.TaxMinor(), o.TotalMinor(),
		string(o.DeliveryFee().Currency()), o.PaymentExpiresAt(), idempotencyKey,
		o.CreatedAt(), o.UpdatedAt())
	if err != nil {
		return fmt.Errorf("order insert: %w", err)
	}

	// Sort items by ProductID to prevent deadlocks during concurrent checkouts
	items := o.Items()
	sort.Slice(items, func(i, j int) bool {
		return items[i].ProductID.String() < items[j].ProductID.String()
	})

	for _, item := range items {
		// 3. Insert Order Item
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

		// 4. Reserve Stock
		tag, err = tx.Exec(ctx, `
UPDATE inventory SET available = available - $2, updated_at = $3
WHERE product_id = $1 AND available >= $2`,
			toUUID(item.ProductID), item.Quantity, o.CreatedAt())
		if err != nil || tag.RowsAffected() == 0 {
			return domain.ErrConflict // Out of stock
		}

		// 5. Write Stock Movement
		_, err = tx.Exec(ctx, `
INSERT INTO stock_movements (id, product_id, delta, reason, order_id, created_at)
VALUES ($1, $2, $3, 'ORDER_RESERVE', $4, $5)`,
			toUUID(r.ids.NewID()), toUUID(item.ProductID), -item.Quantity, toUUID(o.ID()), o.CreatedAt())
		if err != nil {
			return fmt.Errorf("stock movement insert: %w", err)
		}
	}

	// 6. Insert Order History
	for _, h := range o.History() {
		_, _ = tx.Exec(ctx, `INSERT INTO order_status_history (order_id, from_status, to_status, actor, reason, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			toUUID(o.ID()), string(h.From), string(h.To), h.Actor, h.Reason, h.At)
	}

	// 7. Insert Outbox Event
	payload, _ := json.Marshal(o)
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, payload, created_at)
VALUES ($1, 'order', $2, 'OrderCreated', $3, $4)`,
		toUUID(r.ids.NewID()), toUUID(o.ID()), payload, o.CreatedAt())
	if err != nil {
		return fmt.Errorf("outbox insert: %w", err)
	}

	return tx.Commit(ctx)
}
