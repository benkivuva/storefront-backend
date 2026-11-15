package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/clock"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func mustID(t *testing.T, s string) domain.ID {
	id, err := domain.ParseID(s)
	if err != nil {
		t.Fatalf("ParseID(%q): %v", s, err)
	}
	return id
}

type fakeCartRepo struct {
	carts map[domain.ID]domain.Cart
}

func (f *fakeCartRepo) GetActiveByUser(ctx context.Context, userID domain.ID) (domain.Cart, error) {
	for _, c := range f.carts {
		if c.UserID() == userID {
			return c, nil
		}
	}
	return domain.Cart{}, domain.ErrNotFound
}

func (f *fakeCartRepo) Create(ctx context.Context, c domain.Cart) error {
	f.carts[c.ID()] = c
	return nil
}

func (f *fakeCartRepo) Update(ctx context.Context, c domain.Cart) error {
	f.carts[c.ID()] = c
	return nil
}

type fakeProductRepoForCart struct {
	products map[domain.ID]domain.Product
}

func (f *fakeProductRepoForCart) GetByID(ctx context.Context, id domain.ID) (domain.Product, error) {
	p, ok := f.products[id]
	if !ok {
		return domain.Product{}, domain.ErrNotFound
	}
	return p, nil
}

func (f *fakeProductRepoForCart) Create(ctx context.Context, p domain.Product) error { return nil }
func (f *fakeProductRepoForCart) Update(ctx context.Context, p domain.Product) error { return nil }
func (f *fakeProductRepoForCart) Delete(ctx context.Context, id domain.ID, at time.Time) error {
	return nil
}
func (f *fakeProductRepoForCart) List(ctx context.Context, filters domain.ListFilters, limit int, cursor time.Time) ([]domain.Product, error) {
	return nil, nil
}

func TestCartService_GetCart_CreatesIfMissing(t *testing.T) {
	userID := mustID(t, "11111111-1111-7111-8111-111111111111")
	repo := &fakeCartRepo{carts: make(map[domain.ID]domain.Cart)}
	svc := NewCartService(repo, nil, idgen.New(), clock.New())

	c, err := svc.GetCart(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID() != userID {
		t.Fatalf("wrong user")
	}
	if len(repo.carts) != 1 {
		t.Fatalf("cart not created")
	}
}

func TestCartService_AddItem(t *testing.T) {
	userID := mustID(t, "11111111-1111-7111-8111-111111111111")
	prodID := mustID(t, "22222222-2222-7222-8222-222222222222")
	missingProdID := mustID(t, "99999999-9999-7999-8999-999999999999")
	now := time.Now().UTC()

	price, _ := domain.New(1000, domain.KES)
	activeProd, _ := domain.NewProduct(prodID, "SKU1", "Name", "", domain.ID{}, price, domain.TaxStandard16, "", now)
	inactiveProd := activeProd.SoftDelete(now)

	productRepo := &fakeProductRepoForCart{products: map[domain.ID]domain.Product{
		prodID: activeProd,
	}}

	repo := &fakeCartRepo{carts: make(map[domain.ID]domain.Cart)}
	svc := NewCartService(repo, productRepo, idgen.New(), clock.New())

	_, _ = svc.GetCart(context.Background(), userID)

	err := svc.AddItem(context.Background(), userID, prodID, 1)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	c, _ := repo.GetActiveByUser(context.Background(), userID)
	if len(c.Items()) != 1 {
		t.Fatalf("item not added")
	}

	err = svc.AddItem(context.Background(), userID, missingProdID, 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	productRepo.products[prodID] = inactiveProd
	err = svc.AddItem(context.Background(), userID, prodID, 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for inactive, got %v", err)
	}
}

func TestCartService_GetCartView(t *testing.T) {
	userID := mustID(t, "11111111-1111-7111-8111-111111111111")
	prodID := mustID(t, "22222222-2222-7222-8222-222222222222")
	now := time.Now().UTC()

	price, _ := domain.New(1500, domain.KES)
	prod, _ := domain.NewProduct(prodID, "SKU1", "Blue Widget", "", domain.ID{}, price, domain.TaxStandard16, "", now)

	productRepo := &fakeProductRepoForCart{products: map[domain.ID]domain.Product{
		prodID: prod,
	}}
	cartRepo := &fakeCartRepo{carts: make(map[domain.ID]domain.Cart)}
	svc := NewCartService(cartRepo, productRepo, idgen.New(), clock.New())

	_, _ = svc.GetCart(context.Background(), userID)
	_ = svc.AddItem(context.Background(), userID, prodID, 2)

	view, err := svc.GetCartView(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetCartView: %v", err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(view.Items))
	}
	item := view.Items[0]
	if item.ProductName != "Blue Widget" {
		t.Errorf("name = %q", item.ProductName)
	}
	if item.Quantity != 2 {
		t.Errorf("qty = %d", item.Quantity)
	}
	if item.UnitPriceMinor != 1500 {
		t.Errorf("unit price = %d", item.UnitPriceMinor)
	}
	if item.SubtotalMinor != 3000 {
		t.Errorf("subtotal = %d", item.SubtotalMinor)
	}
	if view.TotalMinor != 3000 {
		t.Errorf("total = %d", view.TotalMinor)
	}
}
