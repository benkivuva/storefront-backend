package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

var (
	ErrEmptyCart    = errors.New("service: cannot checkout an empty cart")
	ErrPriceChanged = errors.New("service: prices have changed since the cart was created")
)

type CheckoutService struct {
	carts     domain.CartRepository
	products  domain.ProductRepository
	addresses domain.AddressRepository
	checkout  domain.CheckoutRepository
	ids       domain.IDGenerator
	clock     domain.Clock
}

func NewCheckoutService(carts domain.CartRepository, products domain.ProductRepository, addresses domain.AddressRepository, checkout domain.CheckoutRepository, ids domain.IDGenerator, clock domain.Clock) *CheckoutService {
	return &CheckoutService{
		carts:     carts,
		products:  products,
		addresses: addresses,
		checkout:  checkout,
		ids:       ids,
		clock:     clock,
	}
}

func (s *CheckoutService) Checkout(ctx context.Context, userID, addressID domain.ID, expectedTotalMinor int64, idempotencyKey string) (*domain.Order, error) {
	// 1. Verify Address Ownership
	addr, err := s.addresses.FindByID(ctx, addressID)
	if err != nil || addr.UserID() != userID {
		return nil, fmt.Errorf("%w: invalid address", domain.ErrNotFound)
	}

	// 2. Fetch Cart
	c, err := s.carts.GetActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(c.Items()) == 0 {
		return nil, ErrEmptyCart
	}

	now := s.clock.Now()
	var snapshots []domain.OrderItemSnapshot
	var totalGross int64

	// 3. Re-price
	for _, item := range c.Items() {
		p, err := s.products.GetByID(ctx, item.ProductID())
		if err != nil {
			return nil, err
		}
		if !p.IsActive() {
			return nil, domain.ErrNotFound
		}

		lineGross := p.Price().AmountMinor() * int64(item.Quantity())
		totalGross += lineGross

		grossMoney, err := domain.New(lineGross, domain.KES)
		if err != nil {
			return nil, err
		}

		var rateBp int
		switch p.TaxClass() {
		case domain.TaxStandard16:
			rateBp = 1600
		case domain.TaxZeroRated, domain.TaxExempt:
			rateBp = 0
		}

		_, vat, err := domain.ExtractVAT(grossMoney, rateBp)
		if err != nil {
			return nil, err
		}

		snapshots = append(snapshots, domain.OrderItemSnapshot{
			ProductID:      p.ID(),
			SKU:            p.SKU(),
			ProductName:    p.Name(),
			Quantity:       item.Quantity(),
			UnitPrice:      p.Price(),
			TaxClass:       p.TaxClass(),
			TaxRateBp:      rateBp,
			LineVat:        vat,
			LineGrossMinor: lineGross,
		})
	}

	// Flat delivery fee for now
	deliveryFee, _ := domain.New(50000, domain.KES) // KES 500.00
	calculatedTotal := totalGross + deliveryFee.AmountMinor()

	// 4. Price Change Protection
	if expectedTotalMinor != calculatedTotal {
		return nil, fmt.Errorf("%w: expected %d, got %d", ErrPriceChanged, expectedTotalMinor, calculatedTotal)
	}

	expireAt := now.Add(15 * time.Minute)

	order, err := domain.NewOrder(s.ids.NewID(), userID, addressID, snapshots, deliveryFee, now, expireAt)
	if err != nil {
		return nil, err
	}

	if err := s.checkout.Execute(ctx, c.ID(), order, idempotencyKey); err != nil {
		return nil, err
	}

	return order, nil
}
