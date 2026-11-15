package service

import (
	"context"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type CartService struct {
	carts    domain.CartRepository
	products domain.ProductRepository
	ids      domain.IDGenerator
	clock    domain.Clock
}

func NewCartService(carts domain.CartRepository, products domain.ProductRepository, ids domain.IDGenerator, clock domain.Clock) *CartService {
	return &CartService{carts: carts, products: products, ids: ids, clock: clock}
}

func (s *CartService) GetCart(ctx context.Context, userID domain.ID) (domain.Cart, error) {
	c, err := s.carts.GetActiveByUser(ctx, userID)
	if err != nil {
		if err == domain.ErrNotFound {
			// Create new cart
			c, err = domain.NewCart(s.ids.NewID(), userID, s.clock.Now())
			if err != nil {
				return domain.Cart{}, err
			}
			if err := s.carts.Create(ctx, c); err != nil {
				return domain.Cart{}, err
			}
			return c, nil
		}
		return domain.Cart{}, err
	}
	return c, nil
}

func (s *CartService) AddItem(ctx context.Context, userID, productID domain.ID, qty int) error {
	p, err := s.products.GetByID(ctx, productID)
	if err != nil {
		return err
	}
	if !p.IsActive() {
		return domain.ErrNotFound
	}

	c, err := s.GetCart(ctx, userID)
	if err != nil {
		return err
	}

	if err := c.AddItem(productID, qty); err != nil {
		return err
	}
	return s.carts.Update(ctx, c)
}

func (s *CartService) UpdateQuantity(ctx context.Context, userID, productID domain.ID, qty int) error {
	c, err := s.GetCart(ctx, userID)
	if err != nil {
		return err
	}

	if err := c.UpdateQuantity(productID, qty); err != nil {
		return err
	}
	return s.carts.Update(ctx, c)
}

func (s *CartService) RemoveItem(ctx context.Context, userID, productID domain.ID) error {
	c, err := s.GetCart(ctx, userID)
	if err != nil {
		return err
	}

	if err := c.RemoveItem(productID); err != nil {
		return err
	}
	return s.carts.Update(ctx, c)
}

type CartViewItem struct {
	ProductID      domain.ID
	ProductName    string
	SKU            string
	Quantity       int
	UnitPriceMinor int64
	SubtotalMinor  int64
	Currency       string
}

type CartView struct {
	CartID     domain.ID
	Items      []CartViewItem
	TotalMinor int64
	Currency   string
}

func (s *CartService) GetCartView(ctx context.Context, userID domain.ID) (CartView, error) {
	c, err := s.GetCart(ctx, userID)
	if err != nil {
		return CartView{}, err
	}

	view := CartView{CartID: c.ID(), Currency: "KES"}
	var total int64

	for _, item := range c.Items() {
		p, err := s.products.GetByID(ctx, item.ProductID())
		if err != nil {
			continue
		}
		subtotal := p.Price().AmountMinor() * int64(item.Quantity())
		total += subtotal
		view.Items = append(view.Items, CartViewItem{
			ProductID:      p.ID(),
			ProductName:    p.Name(),
			SKU:            p.SKU(),
			Quantity:       item.Quantity(),
			UnitPriceMinor: p.Price().AmountMinor(),
			SubtotalMinor:  subtotal,
			Currency:       string(p.Price().Currency()),
		})
	}
	view.TotalMinor = total
	return view, nil
}
