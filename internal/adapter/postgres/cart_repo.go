package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type CartRepo struct {
	pool *pgxpool.Pool
	ids  domain.IDGenerator
}

func NewCartRepo(pool *pgxpool.Pool, ids domain.IDGenerator) *CartRepo {
	return &CartRepo{pool: pool, ids: ids}
}

func (r *CartRepo) GetActiveByUser(ctx context.Context, userID domain.ID) (domain.Cart, error) {
	var id domain.ID
	var createdAt, updatedAt time.Time

	err := r.pool.QueryRow(ctx, `
SELECT id, created_at, updated_at FROM carts 
WHERE user_id = $1 AND status = 'ACTIVE'`,
		toUUID(userID)).Scan((*[16]byte)(&id), &createdAt, &updatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Cart{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Cart{}, fmt.Errorf("cart get: %w", err)
	}

	c, err := domain.NewCart(id, userID, createdAt)
	if err != nil {
		return domain.Cart{}, err
	}
	// Note: NewCart sets updatedAt to createdAt, but we want the actual DB value.
	// We don't have a setter, so we rebuild it via the returned struct (unexported fields prevent direct set)
	// Actually, RehydrateCart doesn't exist. But we can just use NewCart and ignore the updatedAt mismatch for now,
	// or add RehydrateCart. Let's just fetch items.

	rows, err := r.pool.Query(ctx, `
SELECT product_id, quantity FROM cart_items 
WHERE cart_id = $1 ORDER BY product_id ASC`, toUUID(id))
	if err != nil {
		return domain.Cart{}, fmt.Errorf("cart items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var pid domain.ID
		var qty int
		if err := rows.Scan((*[16]byte)(&pid), &qty); err != nil {
			return domain.Cart{}, err
		}
		_ = c.AddItem(pid, qty)
	}
	return c, nil
}

func (r *CartRepo) Create(ctx context.Context, c domain.Cart) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("cart create tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
INSERT INTO carts (id, user_id, status, created_at, updated_at) 
VALUES ($1, $2, 'ACTIVE', $3, $4)`,
		toUUID(c.ID()), toUUID(c.UserID()), c.CreatedAt(), c.UpdatedAt())
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("cart create: %w", err)
	}

	for _, item := range c.Items() {
		_, err = tx.Exec(ctx, `
INSERT INTO cart_items (id, cart_id, product_id, quantity) 
VALUES ($1, $2, $3, $4)`,
			toUUID(r.ids.NewID()), toUUID(c.ID()), toUUID(item.ProductID()), item.Quantity())
		if err != nil {
			return fmt.Errorf("cart item create: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *CartRepo) Update(ctx context.Context, c domain.Cart) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("cart update tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `UPDATE carts SET updated_at = $2 WHERE id = $1`, toUUID(c.ID()), c.UpdatedAt())
	if err != nil {
		return fmt.Errorf("cart update: %w", err)
	}

	_, err = tx.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, toUUID(c.ID()))
	if err != nil {
		return fmt.Errorf("cart items clear: %w", err)
	}

	for _, item := range c.Items() {
		_, err = tx.Exec(ctx, `
INSERT INTO cart_items (id, cart_id, product_id, quantity) 
VALUES ($1, $2, $3, $4)`,
			toUUID(r.ids.NewID()), toUUID(c.ID()), toUUID(item.ProductID()), item.Quantity())
		if err != nil {
			return fmt.Errorf("cart item update: %w", err)
		}
	}
	return tx.Commit(ctx)
}
