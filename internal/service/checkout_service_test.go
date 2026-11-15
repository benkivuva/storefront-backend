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

func TestCheckoutService_Checkout(t *testing.T) {
	userID := mustID(t, "11111111-1111-7111-8111-111111111111")
	addrID := mustID(t, "22222222-2222-7222-8222-222222222222")
	prodID := mustID(t, "33333333-3333-7333-8333-333333333333")
	cartID := mustID(t, "44444444-4444-7444-8444-444444444444")
	now := time.Now().UTC()

	// Setup Cart
	c, _ := domain.NewCart(cartID, userID, now)
	_ = c.AddItem(prodID, 2)
	cartRepo := &fakeCartRepo{carts: map[domain.ID]domain.Cart{cartID: c}}

	// Setup Product (Price 1160, Tax STANDARD_16)
	price, _ := domain.New(1160, domain.KES)
	prod, _ := domain.NewProduct(prodID, "SKU1", "Widget", "", domain.ID{}, price, domain.TaxStandard16, "", now)
	prodRepo := &fakeProductRepoForCart{products: map[domain.ID]domain.Product{prodID: prod}}

	chkRepo := &fakeCheckoutRepo{}
	svc := NewCheckoutService(cartRepo, prodRepo, chkRepo, idgen.New(), clock.New())

	order, err := svc.Checkout(context.Background(), userID, addrID, "idem-key-1")
	if err != nil {
		t.Fatalf("Checkout failed: %v", err)
	}

	if !chkRepo.executed {
		t.Fatalf("repository was not called")
	}
	if order.Status() != domain.StatusPendingPayment {
		t.Errorf("expected PENDING_PAYMENT, got %s", order.Status())
	}
	// Price 1160 * 2 = 2320 Gross. VAT (16%) is 320. Total = 2320 + Delivery (500) = 2820.
	if order.SubtotalMinor() != 2320 {
		t.Errorf("expected subtotal 2320, got %d", order.SubtotalMinor())
	}
	if order.TaxMinor() != 320 {
		t.Errorf("expected tax 320, got %d", order.TaxMinor())
	}
	if order.TotalMinor() != 52320 {
		t.Errorf("expected total 52320, got %d", order.TotalMinor())
	}
}

func TestCheckoutService_EmptyCart(t *testing.T) {
	userID := mustID(t, "11111111-1111-7111-8111-111111111111")
	addrID := mustID(t, "22222222-2222-7222-8222-222222222222")
	cartID := mustID(t, "44444444-4444-7444-8444-444444444444")
	now := time.Now().UTC()

	c, _ := domain.NewCart(cartID, userID, now)
	cartRepo := &fakeCartRepo{carts: map[domain.ID]domain.Cart{cartID: c}}

	svc := NewCheckoutService(cartRepo, &fakeProductRepoForCart{}, &fakeCheckoutRepo{}, idgen.New(), clock.New())

	_, err := svc.Checkout(context.Background(), userID, addrID, "idem-key-2")
	if !errors.Is(err, ErrEmptyCart) {
		t.Fatalf("expected ErrEmptyCart, got %v", err)
	}
}
