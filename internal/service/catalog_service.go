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
	outbox     domain.OutboxRepository
	audit      domain.AuditLog
	cache      domain.Cache
	ids        domain.IDGenerator
	clock      domain.Clock
}

func NewCatalogService(
	products domain.ProductRepository,
	categories domain.CategoryRepository,
	inventory domain.InventoryRepository,
	outbox domain.OutboxRepository,
	audit domain.AuditLog,
	cache domain.Cache,
	ids domain.IDGenerator,
	clock domain.Clock,
) *CatalogService {
	return &CatalogService{
		products:   products,
		categories: categories,
		inventory:  inventory,
		outbox:     outbox,
		audit:      audit,
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

	var p domain.Product
	if err := json.Unmarshal(data, &p); err != nil {
		return domain.Product{}, fmt.Errorf("catalog: unmarshal product: %w", err)
	}
	return p, nil
}

func (s *CatalogService) ListCategories(ctx context.Context) ([]domain.Category, error) {
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

func (s *CatalogService) CreateProduct(ctx context.Context, sku, name, desc string, catID domain.ID, price domain.Money, taxClass domain.TaxClass, actorID domain.ID, ip string) (domain.Product, error) {
	now := s.clock.Now()
	p, err := domain.NewProduct(s.ids.NewID(), sku, name, desc, catID, price, taxClass, "", now)
	if err != nil {
		return domain.Product{}, err
	}
	if err := s.products.Create(ctx, p); err != nil {
		return domain.Product{}, err
	}

	payload, _ := json.Marshal(p)
	_ = s.outbox.Create(ctx, domain.OutboxEvent{
		ID: s.ids.NewID(), AggregateType: "product", AggregateID: p.ID(),
		EventType: "ProductUpserted", Payload: payload, CreatedAt: now,
	})

	_ = s.audit.Append(ctx, domain.AuditEntry{
		Actor: actorID, Action: "product_created", EntityType: "product", EntityID: p.ID().String(), At: now,
	})

	return p, nil
}

func (s *CatalogService) UpdateProduct(ctx context.Context, id domain.ID, name, desc string, catID domain.ID, price domain.Money, taxClass domain.TaxClass, expectedVersion int, actorID domain.ID, ip string) (domain.Product, error) {
	p, err := s.products.GetByID(ctx, id)
	if err != nil {
		return domain.Product{}, err
	}

	if p.Version() != expectedVersion {
		return domain.Product{}, domain.ErrConflict
	}

	updated, err := p.UpdateDetails(name, desc, catID, price, taxClass, "", s.clock.Now())
	if err != nil {
		return domain.Product{}, err
	}

	if err := s.products.Update(ctx, updated); err != nil {
		return domain.Product{}, err
	}

	payload, _ := json.Marshal(updated)
	_ = s.outbox.Create(ctx, domain.OutboxEvent{
		ID: s.ids.NewID(), AggregateType: "product", AggregateID: updated.ID(),
		EventType: "ProductUpserted", Payload: payload, CreatedAt: s.clock.Now(),
	})

	_ = s.audit.Append(ctx, domain.AuditEntry{
		Actor: actorID, Action: "product_updated", EntityType: "product", EntityID: updated.ID().String(), At: s.clock.Now(),
	})

	_ = s.cache.Delete(ctx, "product:"+id.String())
	return updated, nil
}

func (s *CatalogService) DeleteProduct(ctx context.Context, id domain.ID, actorID domain.ID) error {
	if err := s.products.Delete(ctx, id, s.clock.Now()); err != nil {
		return err
	}

	_ = s.outbox.Create(ctx, domain.OutboxEvent{
		ID: s.ids.NewID(), AggregateType: "product", AggregateID: id,
		EventType: "ProductDeactivated", Payload: []byte(`{}`), CreatedAt: s.clock.Now(),
	})

	_ = s.audit.Append(ctx, domain.AuditEntry{
		Actor: actorID, Action: "product_deleted", EntityType: "product", EntityID: id.String(), At: s.clock.Now(),
	})

	_ = s.cache.Delete(ctx, "product:"+id.String())
	return nil
}

func (s *CatalogService) AdjustStock(ctx context.Context, productID domain.ID, delta int, reason domain.StockReason) error {
	m, err := domain.NewStockMovement(s.ids.NewID(), productID, delta, reason, domain.ID{}, domain.ID{}, s.clock.Now())
	if err != nil {
		return err
	}

	if err := s.inventory.AdjustStock(ctx, m); err != nil {
		return err
	}

	_ = s.cache.Delete(ctx, "product:"+productID.String())
	return nil
}

func (s *CatalogService) ListLowStock(ctx context.Context) ([]domain.Inventory, error) {
	return s.inventory.ListLowStock(ctx)
}

func (s *CatalogService) ListProducts(ctx context.Context, filters domain.ListFilters, limit int, cursor string) ([]domain.Product, error) {
	key := fmt.Sprintf("products:list:%s:%s:%d:%d:%t:%d:%s", filters.CategoryID, filters.Query, filters.MinPrice, filters.MaxPrice, filters.InStock, limit, cursor)

	data, err := s.cache.GetOrFetch(ctx, key, 60*time.Second, func() ([]byte, error) {
		var cursorTime time.Time
		if cursor != "" {
			t, err := time.Parse(time.RFC3339, cursor)
			if err != nil {
				return nil, fmt.Errorf("invalid cursor: %w", err)
			}
			cursorTime = t
		}
		ps, err := s.products.List(ctx, filters, limit, cursorTime)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ps)
	})
	if err != nil {
		return nil, err
	}

	var ps []domain.Product
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("catalog: unmarshal products list: %w", err)
	}
	return ps, nil
}
