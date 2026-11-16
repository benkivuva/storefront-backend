package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type OrderRepo struct {
	pool *pgxpool.Pool
	ids  domain.IDGenerator
}

func NewOrderRepo(pool *pgxpool.Pool, ids domain.IDGenerator) *OrderRepo {
	return &OrderRepo{pool: pool, ids: ids}
}

func (r *OrderRepo) Create(ctx context.Context, o *domain.Order, idempotencyKey string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("order create tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var addrID pgtype.UUID
	if !o.AddressID().IsZero() {
		addrID = toUUID(o.AddressID())
	}

	_, err = tx.Exec(ctx, `
INSERT INTO orders (
id, user_id, shipping_address_id, status,
subtotal_minor, delivery_fee_minor, tax_minor, total_minor,
currency, payment_expires_at, idempotency_key,
created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		toUUID(o.ID()), toUUID(o.UserID()), addrID, string(o.Status()),
		o.SubtotalMinor(), o.DeliveryFee().AmountMinor(), o.TaxMinor(), o.TotalMinor(),
		string(o.DeliveryFee().Currency()), o.PaymentExpiresAt(), idempotencyKey,
		o.CreatedAt(), o.UpdatedAt())
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("order insert: %w", err)
	}

	for _, item := range o.Items() {
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
	}

	for _, h := range o.History() {
		_, err = tx.Exec(ctx, `
INSERT INTO order_status_history (order_id, from_status, to_status, actor, reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
			toUUID(o.ID()), string(h.From), string(h.To), h.Actor, h.Reason, h.At)
		if err != nil {
			return fmt.Errorf("order history insert: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r *OrderRepo) GetByID(ctx context.Context, id domain.ID) (*domain.Order, error) {
	var (
		oid, userID, addrID                    pgtype.UUID
		orderNumber                            int64
		status, currency                       string
		subtotal, deliveryFee, taxMinor, total int64
		paymentExpiresAt, createdAt, updatedAt time.Time
	)

	err := r.pool.QueryRow(ctx, `
SELECT id, order_number, user_id, shipping_address_id, status,
       subtotal_minor, delivery_fee_minor, tax_minor, total_minor,
       currency, payment_expires_at, created_at, updated_at
FROM orders WHERE id = $1`, toUUID(id)).Scan(
		&oid, &orderNumber, &userID, &addrID, &status,
		&subtotal, &deliveryFee, &taxMinor, &total,
		&currency, &paymentExpiresAt, &createdAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("order get: %w", err)
	}

	orderCurrency := domain.Currency(currency)
	fee, _ := domain.New(deliveryFee, orderCurrency)

	rows, err := r.pool.Query(ctx, `
SELECT product_id, sku, product_name, quantity,
       unit_price_minor, tax_class, tax_rate_bp, line_gross_minor, line_vat_minor
FROM order_items WHERE order_id = $1 ORDER BY id ASC`, toUUID(id))
	if err != nil {
		return nil, fmt.Errorf("order items get: %w", err)
	}
	defer rows.Close()

	var items []domain.OrderItemSnapshot
	for rows.Next() {
		var (
			prodID    pgtype.UUID
			sku, name string
			qty       int
			unitPrice int64
			taxClass  string
			taxRate   int
			lineGross int64
			lineVat   int64
		)
		if err := rows.Scan(&prodID, &sku, &name, &qty, &unitPrice, &taxClass, &taxRate, &lineGross, &lineVat); err != nil {
			return nil, fmt.Errorf("item scan: %w", err)
		}
		uPrice, _ := domain.New(unitPrice, orderCurrency)
		lVat, _ := domain.New(lineVat, orderCurrency)
		items = append(items, domain.OrderItemSnapshot{
			ProductID:      fromUUID(prodID),
			SKU:            sku,
			ProductName:    name,
			Quantity:       qty,
			UnitPrice:      uPrice,
			TaxClass:       domain.TaxClass(taxClass),
			TaxRateBp:      taxRate,
			LineGrossMinor: lineGross,
			LineVat:        lVat,
		})
	}

	hRows, err := r.pool.Query(ctx, `
SELECT from_status, to_status, actor, reason, created_at
FROM order_status_history WHERE order_id = $1 ORDER BY id ASC`, toUUID(id))
	if err != nil {
		return nil, fmt.Errorf("order history get: %w", err)
	}
	defer hRows.Close()

	var history []domain.StatusChange
	for hRows.Next() {
		var fromStatus pgtype.Text
		var toStatus, actor string
		var reason pgtype.Text
		var at time.Time
		if err := hRows.Scan(&fromStatus, &toStatus, &actor, &reason, &at); err != nil {
			return nil, fmt.Errorf("history scan: %w", err)
		}
		history = append(history, domain.StatusChange{
			From: domain.Status(fromStatus.String), To: domain.Status(toStatus),
			Actor: actor, Reason: reason.String, At: at,
		})
	}

	return domain.RehydrateOrder(
		fromUUID(oid), orderNumber, fromUUID(userID), fromUUID(addrID),
		items, fee, subtotal, taxMinor, total, domain.Status(status),
		history, paymentExpiresAt, createdAt, updatedAt,
	), nil
}

func (r *OrderRepo) UpdateStatus(ctx context.Context, o *domain.Order) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("order status tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1`,
		toUUID(o.ID()), string(o.Status()), o.UpdatedAt())
	if err != nil {
		return fmt.Errorf("order status update: %w", err)
	}

	hist := o.History()
	if len(hist) == 0 {
		return fmt.Errorf("order has no history")
	}
	latest := hist[len(hist)-1]

	_, err = tx.Exec(ctx, `
INSERT INTO order_status_history (order_id, from_status, to_status, actor, reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
		toUUID(o.ID()), string(latest.From), string(latest.To), latest.Actor, latest.Reason, latest.At)
	if err != nil {
		return fmt.Errorf("order history insert: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *OrderRepo) ListByUser(ctx context.Context, userID domain.ID) ([]*domain.Order, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id FROM orders 
WHERE user_id = $1 ORDER BY created_at DESC LIMIT 100`, toUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("order list: %w", err)
	}
	defer rows.Close()

	var ids []domain.ID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, fromUUID(id))
	}

	var out []*domain.Order
	for _, id := range ids {
		o, err := r.GetByID(ctx, id)
		if err == nil {
			out = append(out, o)
		}
	}
	return out, nil
}
