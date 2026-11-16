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

type fakeCheckoutRepo struct {
	executed bool
}

func (f *fakeCheckoutRepo) Execute(ctx context.Context, cartID domain.ID, o *domain.Order, idemKey string) error {
	f.executed = true
	return nil
}

type fakeCheckoutAddressRepo struct {
	addresses map[domain.ID]domain.Address
}

func (f *fakeCheckoutAddressRepo) ListByUser(ctx context.Context, userID domain.ID) ([]domain.Address, error) {
	return nil, nil
}
func (f *fakeCheckoutAddressRepo) Create(ctx context.Context, a domain.Address) error { return nil }
func (f *fakeCheckoutAddressRepo) FindByID(ctx context.Context, id domain.ID) (domain.Address, error) {
	a, ok := f.addresses[id]
	if !ok {
		return domain.Address{}, domain.ErrNotFound
	}
	return a, nil
}

func TestCheckoutService_Checkout(t *testing.T) {
	userID := mustID(t, "11111111-1111-7111-8111-111111111111")
	addrID := mustID(t, "22222222-2222-7222-8222-222222222222")
	prodID := mustID(t, "33333333-3333-7333-8333-333333333333")
	cartID := mustID(t, "44444444-4444-7444-8444-444444444444")
	now := time.Now().UTC()

	c, _ := domain.NewCart(cartID, userID, now)
	_ = c.AddItem(prodID, 2)
	cartRepo := &fakeCartRepo{carts: map[domain.ID]domain.Cart{cartID: c}}

	price, _ := domain.New(1160, domain.KES)
	prod, _ := domain.NewProduct(prodID, "SKU1", "Widget", "", domain.ID{}, price, domain.TaxStandard16, "", now)
	prodRepo := &fakeProductRepoForCart{products: map[domain.ID]domain.Product{prodID: prod}}

	addr, _ := domain.NewAddress(addrID, userID, "R", "+254712345678", "L1", "", "T", "", "", now)
	addrRepo := &fakeCheckoutAddressRepo{addresses: map[domain.ID]domain.Address{addrID: addr}}

	chkRepo := &fakeCheckoutRepo{}
	svc := NewCheckoutService(cartRepo, prodRepo, addrRepo, chkRepo, idgen.New(), clock.New())

	_, err := svc.Checkout(context.Background(), userID, addrID, 52320, "idem-key-1")
	if err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	if !chkRepo.executed {
		t.Fatalf("repository was not called")
	}
}

func TestCheckoutService_EmptyCart(t *testing.T) {
	userID := mustID(t, "11111111-1111-7111-8111-111111111111")
	addrID := mustID(t, "22222222-2222-7222-8222-222222222222")
	cartID := mustID(t, "44444444-4444-7444-8444-444444444444")
	now := time.Now().UTC()

	c, _ := domain.NewCart(cartID, userID, now)
	cartRepo := &fakeCartRepo{carts: map[domain.ID]domain.Cart{cartID: c}}

	addr, _ := domain.NewAddress(addrID, userID, "R", "+254712345678", "L1", "", "T", "", "", now)
	addrRepo := &fakeCheckoutAddressRepo{addresses: map[domain.ID]domain.Address{addrID: addr}}

	svc := NewCheckoutService(cartRepo, &fakeProductRepoForCart{}, addrRepo, &fakeCheckoutRepo{}, idgen.New(), clock.New())

	_, err := svc.Checkout(context.Background(), userID, addrID, 0, "idem-key-2")
	if !errors.Is(err, ErrEmptyCart) {
		t.Fatalf("expected ErrEmptyCart, got %v", err)
	}
}
