package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type PaymentRepo struct {
	pool *pgxpool.Pool
}

func NewPaymentRepo(pool *pgxpool.Pool) *PaymentRepo {
	return &PaymentRepo{pool: pool}
}

func (r *PaymentRepo) Create(ctx context.Context, p *domain.Payment) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO payments (
id, order_id, provider, method, status, amount_minor, currency, provider_ref, provider_txn_id, failure_reason, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		toUUID(p.ID()), toUUID(p.OrderID()), p.Provider(), string(p.Method()), string(p.Status()),
		p.AmountMinor(), p.Currency(), p.ProviderRef(), p.ProviderTxnID(), p.FailureReason(),
		p.CreatedAt(), p.UpdatedAt())
	if err != nil {
		return fmt.Errorf("payment create: %w", err)
	}
	return nil
}

func (r *PaymentRepo) GetByID(ctx context.Context, id domain.ID) (*domain.Payment, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, order_id, provider, method, status, amount_minor, currency, coalesce(provider_ref, ''), coalesce(provider_txn_id, ''), coalesce(failure_reason, ''), created_at, updated_at
FROM payments WHERE id = $1`, toUUID(id))
	return scanPayment(row)
}

func (r *PaymentRepo) GetByProviderRef(ctx context.Context, provider, ref string) (*domain.Payment, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, order_id, provider, method, status, amount_minor, currency, coalesce(provider_ref, ''), coalesce(provider_txn_id, ''), coalesce(failure_reason, ''), created_at, updated_at
FROM payments WHERE provider = $1 AND provider_ref = $2`, provider, ref)
	return scanPayment(row)
}

func (r *PaymentRepo) GetLatestByOrderID(ctx context.Context, orderID domain.ID) (*domain.Payment, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, order_id, provider, method, status, amount_minor, currency, coalesce(provider_ref, ''), coalesce(provider_txn_id, ''), coalesce(failure_reason, ''), created_at, updated_at
FROM payments WHERE order_id = $1 ORDER BY created_at DESC LIMIT 1`, toUUID(orderID))
	return scanPayment(row)
}

func (r *PaymentRepo) ListByStatus(ctx context.Context, status domain.PaymentStatus, limit int) ([]*domain.Payment, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, order_id, provider, method, status, amount_minor, currency, coalesce(provider_ref, ''), coalesce(provider_txn_id, ''), coalesce(failure_reason, ''), created_at, updated_at
FROM payments WHERE status = $1 ORDER BY created_at ASC LIMIT $2`, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("payment list by status: %w", err)
	}
	defer rows.Close()

	var out []*domain.Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PaymentRepo) UpdateStatus(ctx context.Context, p *domain.Payment) error {
	_, err := r.pool.Exec(ctx, `
UPDATE payments SET 
status = $2, provider_ref = $3, provider_txn_id = $4, failure_reason = $5, updated_at = $6
WHERE id = $1`,
		toUUID(p.ID()), string(p.Status()), p.ProviderRef(), p.ProviderTxnID(), p.FailureReason(), p.UpdatedAt())
	if err != nil {
		return fmt.Errorf("payment update status: %w", err)
	}
	return nil
}

func scanPayment(row pgx.Row) (*domain.Payment, error) {
	var rec domain.PaymentRecord
	err := row.Scan(
		(*[16]byte)(&rec.ID),
		(*[16]byte)(&rec.OrderID),
		&rec.Provider,
		&rec.Method,
		&rec.Status,
		&rec.AmountMinor,
		&rec.Currency,
		&rec.ProviderRef,
		&rec.ProviderTxnID,
		&rec.FailureReason,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan payment: %w", err)
	}
	return domain.RehydratePayment(rec), nil
}
