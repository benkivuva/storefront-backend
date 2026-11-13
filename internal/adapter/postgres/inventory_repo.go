package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type InventoryRepo struct {
	pool *pgxpool.Pool
}

func NewInventoryRepo(pool *pgxpool.Pool) *InventoryRepo {
	return &InventoryRepo{pool: pool}
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

func (r *InventoryRepo) ListLowStock(ctx context.Context) ([]domain.Inventory, error) {
	rows, err := r.pool.Query(ctx, `
SELECT product_id, available, low_stock_threshold, updated_at
FROM inventory WHERE available <= low_stock_threshold`)
	if err != nil {
		return nil, fmt.Errorf("inventory low stock: %w", err)
	}
	defer rows.Close()

	var out []domain.Inventory
	for rows.Next() {
		var pid pgtype.UUID
		var avail, thresh int
		var updatedAt time.Time
		if err := rows.Scan(&pid, &avail, &thresh, &updatedAt); err != nil {
			return nil, err
		}
		out = append(out, domain.RehydrateInventory(fromUUID(pid), avail, thresh, updatedAt))
	}
	return out, rows.Err()
}
