package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type InventoryRepo struct {
	pool *pgxpool.Pool
}

func NewInventoryRepo(pool *pgxpool.Pool) *InventoryRepo {
	return &InventoryRepo{pool: pool}
}

// AdjustStock inserts the ledger entry and updates the available count
// in a single transaction.
func (r *InventoryRepo) AdjustStock(ctx context.Context, m domain.StockMovement) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("inventory tx begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
INSERT INTO stock_movements (id, product_id, delta, reason, order_id, actor, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		toUUID(m.ID()), toUUID(m.ProductID()), m.Delta(), string(m.Reason()),
		toNullUUID(m.OrderID()), toNullUUID(m.Actor()), m.CreatedAt())
	if err != nil {
		return fmt.Errorf("inventory ledger insert: %w", err)
	}

	tag, err := tx.Exec(ctx, `
UPDATE inventory SET available = available + $2, updated_at = $3
WHERE product_id = $1`,
		toUUID(m.ProductID()), m.Delta(), m.CreatedAt())
	if err != nil {
		return fmt.Errorf("inventory update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return tx.Commit(ctx)
}

func toNullUUID(id domain.ID) interface{} {
	if id.IsZero() {
		return nil
	}
	return toUUID(id)
}

func (r *InventoryRepo) Create(ctx context.Context, inv domain.Inventory) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO inventory (product_id, available, low_stock_threshold, updated_at)
VALUES ($1, $2, $3, $4)`,
		toUUID(inv.ProductID()), inv.Available(), inv.Threshold(), inv.UpdatedAt())
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("inventory create: %w", err)
	}
	return nil
}
