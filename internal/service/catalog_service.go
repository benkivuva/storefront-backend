package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type CatalogService struct {
	products   domain.ProductRepository
	categories domain.CategoryRepository
	inventory  domain.InventoryRepository
	cache      domain.Cache
	ids        domain.IDGenerator
	clock      domain.Clock
}

func NewCatalogService(
	products domain.ProductRepository,
	categories domain.CategoryRepository,
	inventory domain.InventoryRepository,
	cache domain.Cache,
	ids domain.IDGenerator,
	clock domain.Clock,
) *CatalogService {
	return &CatalogService{
		products:   products,
		categories: categories,
		inventory:  inventory,
		cache:      cache,
		ids:        ids,
		clock:      clock,
	}
}

func (s *CatalogService) GetProduct(ctx context.Context, id domain.ID) (domain.Product, error) {
	key := "product:" + id.String()

	data, err := s.cache.GetOrFetch(ctx, key, 60*time.Second, func() ([]byte, error) {
		p, err := s.products.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		return json.Marshal(p)
	})
	if err != nil {
		return domain.Product{}, err
	}

	// We need a way to unmarshal. Since domain.Product has unexported fields,
	// we can't json.Unmarshal directly into it. We must unmarshal into a record.
	var rec domain.ProductRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return domain.Product{}, fmt.Errorf("catalog: unmarshal product: %w", err)
	}
	return domain.RehydrateProduct(rec)
}

func (s *CatalogService) ListCategories(ctx context.Context) ([]domain.Category, error) {
	// Categories are highly cacheable and rarely change.
	key := "categories:all"
	data, err := s.cache.GetOrFetch(ctx, key, 300*time.Second, func() ([]byte, error) {
		cats, err := s.categories.List(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(cats)
	})
	if err != nil {
		return nil, err
	}

	var cats []domain.Category
	if err := json.Unmarshal(data, &cats); err != nil {
		return nil, fmt.Errorf("catalog: unmarshal categories: %w", err)
	}
	return cats, nil
}

// CreateProduct is an admin action. Invalidates the list cache if needed.
func (s *CatalogService) CreateProduct(ctx context.Context, sku, name, desc string, catID domain.ID, price domain.Money, taxClass domain.TaxClass) (domain.Product, error) {
	now := s.clock.Now()
	p, err := domain.NewProduct(s.ids.NewID(), sku, name, desc, catID, price, taxClass, "", now)
	if err != nil {
		return domain.Product{}, err
	}
	if err := s.products.Create(ctx, p); err != nil {
		return domain.Product{}, err
	}
	return p, nil
}

// UpdateProduct handles optimistic concurrency and cache invalidation.
func (s *CatalogService) UpdateProduct(ctx context.Context, id domain.ID, name, desc string, catID domain.ID, price domain.Money, taxClass domain.TaxClass, expectedVersion int) (domain.Product, error) {
	// 1. Get current
	p, err := s.products.GetByID(ctx, id)
	if err != nil {
		return domain.Product{}, err
	}

	// 2. Check version
	if p.Version() != expectedVersion {
		return domain.Product{}, domain.ErrConflict
	}

	// 3. Update domain entity
	updated, err := p.UpdateDetails(name, desc, catID, price, taxClass, "", s.clock.Now())
	if err != nil {
		return domain.Product{}, err
	}

	// 4. Persist
	if err := s.products.Update(ctx, updated); err != nil {
		return domain.Product{}, err
	}

	// 5. Invalidate cache
	_ = s.cache.Delete(ctx, "product:"+id.String())

	return updated, nil
}

func (s *CatalogService) AdjustStock(ctx context.Context, productID domain.ID, delta int, reason domain.StockReason) error {
	m, err := domain.NewStockMovement(s.ids.NewID(), productID, delta, reason, domain.ID{}, domain.ID{}, s.clock.Now())
	if err != nil {
		return err
	}

	err = s.inventory.AdjustStock(ctx, m)
	if err != nil {
		return err
	}

	// Invalidate product cache because stock might have changed (if it were included)
	_ = s.cache.Delete(ctx, "product:"+productID.String())
	return nil
}
