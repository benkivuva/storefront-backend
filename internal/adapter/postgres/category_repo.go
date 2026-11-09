package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type CategoryRepo struct {
	pool *pgxpool.Pool
}

func NewCategoryRepo(pool *pgxpool.Pool) *CategoryRepo {
	return &CategoryRepo{pool: pool}
}

func (r *CategoryRepo) Create(ctx context.Context, c domain.Category) error {
	var parentID pgtype.UUID
	if !c.ParentID().IsZero() {
		parentID = toUUID(c.ParentID())
	}

	_, err := r.pool.Exec(ctx, `
INSERT INTO categories (id, parent_id, slug, name)
VALUES ($1, $2, $3, $4)`,
		toUUID(c.ID()), parentID, c.Slug(), c.Name())
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("category create: %w", err)
	}
	return nil
}

func (r *CategoryRepo) List(ctx context.Context) ([]domain.Category, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, parent_id, slug, name FROM categories ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("category list: %w", err)
	}
	defer rows.Close()

	var out []domain.Category
	for rows.Next() {
		var id, parentID pgtype.UUID
		var slug, name string
		if err := rows.Scan(&id, &parentID, &slug, &name); err != nil {
			return nil, err
		}
		c, err := domain.NewCategory(fromUUID(id), fromUUID(parentID), slug, name)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
