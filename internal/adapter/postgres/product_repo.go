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

type ProductRepo struct {
	pool *pgxpool.Pool
}

func NewProductRepo(pool *pgxpool.Pool) *ProductRepo {
	return &ProductRepo{pool: pool}
}

const productSelect = `
SELECT id, sku, name, COALESCE(description, ''), category_id, 
       price_minor, currency, tax_class, COALESCE(etims_item_code, ''), 
       is_active, version, deleted_at, created_at, updated_at
FROM products`

func (r *ProductRepo) Create(ctx context.Context, p domain.Product) error {
	var catID pgtype.UUID
	if !p.CategoryID().IsZero() {
		catID = toUUID(p.CategoryID())
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO products 
  (id, sku, name, description, category_id, price_minor, currency, tax_class, etims_item_code, is_active, version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		toUUID(p.ID()), p.SKU(), p.Name(), strOrNil(p.Description()), catID,
		p.Price().AmountMinor(), p.Price().Currency(), string(p.TaxClass()), strOrNil(p.EtimsItemCode()),
		p.IsActive(), p.Version(), p.CreatedAt(), p.UpdatedAt())
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("product create: %w", err)
	}
	return nil
}

func (r *ProductRepo) GetByID(ctx context.Context, id domain.ID) (domain.Product, error) {
	p, err := scanProduct(r.pool.QueryRow(ctx, productSelect+" WHERE id = $1 AND deleted_at IS NULL", toUUID(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Product{}, domain.ErrNotFound
	}
	return p, err
}

func (r *ProductRepo) Update(ctx context.Context, p domain.Product) error {
	var catID pgtype.UUID
	if !p.CategoryID().IsZero() {
		catID = toUUID(p.CategoryID())
	}

	tag, err := r.pool.Exec(ctx, `
UPDATE products SET
name = $2, description = $3, category_id = $4, 
price_minor = $5, tax_class = $6, etims_item_code = $7, 
version = $8, updated_at = $9
WHERE id = $1 AND version = $10 AND deleted_at IS NULL`,
		toUUID(p.ID()), p.Name(), strOrNil(p.Description()), catID,
		p.Price().AmountMinor(), string(p.TaxClass()), strOrNil(p.EtimsItemCode()),
		p.Version(), p.UpdatedAt(), p.Version()-1)

	if err != nil {
		return fmt.Errorf("product update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (r *ProductRepo) Delete(ctx context.Context, id domain.ID, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE products SET deleted_at = $2, updated_at = $2, version = version + 1
WHERE id = $1 AND deleted_at IS NULL`,
		toUUID(id), at)
	if err != nil {
		return fmt.Errorf("product delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ProductRepo) List(ctx context.Context, f domain.ListFilters, limit int, cursor time.Time) ([]domain.Product, error) {
	query := productSelect + " WHERE deleted_at IS NULL"
	args := []interface{}{}
	argCount := 1

	if f.CategoryID != "" {
		query += fmt.Sprintf(" AND category_id = $%d::uuid", argCount)
		args = append(args, f.CategoryID)
		argCount++
	}
	if f.Query != "" {
		query += fmt.Sprintf(" AND to_tsvector('simple', name || ' ' || coalesce(description, '')) @@ plainto_tsquery('simple', $%d)", argCount)
		args = append(args, f.Query)
		argCount++
	}
	if f.MinPrice > 0 {
		query += fmt.Sprintf(" AND price_minor >= $%d", argCount)
		args = append(args, f.MinPrice)
		argCount++
	}
	if f.MaxPrice > 0 {
		query += fmt.Sprintf(" AND price_minor <= $%d", argCount)
		args = append(args, f.MaxPrice)
		argCount++
	}
	if f.InStock {
		query += " AND id IN (SELECT product_id FROM inventory WHERE available > 0)"
	}

	if !cursor.IsZero() {
		query += fmt.Sprintf(" AND created_at < $%d", argCount)
		args = append(args, cursor)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("product list: %w", err)
	}
	defer rows.Close()

	var out []domain.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProduct(row pgx.Row) (domain.Product, error) {
	var (
		id, catID                                  pgtype.UUID
		sku, name, desc, currency, taxClass, etims string
		priceMinor                                 int64
		isActive                                   bool
		version                                    int
		deletedAt                                  *time.Time
		createdAt, updatedAt                       time.Time
	)
	if err := row.Scan(&id, &sku, &name, &desc, &catID, &priceMinor, &currency, &taxClass, &etims,
		&isActive, &version, &deletedAt, &createdAt, &updatedAt); err != nil {
		return domain.Product{}, err
	}

	price, err := domain.New(priceMinor, domain.Currency(currency))
	if err != nil {
		return domain.Product{}, err
	}
	rec := domain.ProductRecord{
		ID: fromUUID(id), SKU: sku, Name: name, Description: desc, CategoryID: fromUUID(catID),
		Price: price, TaxClass: domain.TaxClass(taxClass), EtimsItemCode: etims,
		IsActive: isActive, Version: version, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if deletedAt != nil {
		rec.DeletedAt = *deletedAt
	}
	return domain.RehydrateProduct(rec)
}
