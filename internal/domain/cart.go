package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidQuantity     = errors.New("domain: quantity must be positive")
	ErrMaxQuantityExceeded = errors.New("domain: max quantity per item exceeded")
	ErrItemNotFound        = errors.New("domain: item not found in cart")
	ErrZeroCartID          = errors.New("domain: cart id is zero")
	ErrZeroCartUserID      = errors.New("domain: cart user id is zero")
	ErrZeroCartTime        = errors.New("domain: cart timestamp is zero")
)

const MaxCartItemQuantity = 100

type CartItem struct {
	productID ID
	quantity  int
}

func (i CartItem) ProductID() ID { return i.productID }
func (i CartItem) Quantity() int { return i.quantity }

type Cart struct {
	id        ID
	userID    ID
	items     []CartItem
	createdAt time.Time
	updatedAt time.Time
}

func NewCart(id, userID ID, at time.Time) (Cart, error) {
	if id.IsZero() {
		return Cart{}, ErrZeroCartID
	}
	if userID.IsZero() {
		return Cart{}, ErrZeroCartUserID
	}
	if at.IsZero() {
		return Cart{}, ErrZeroCartTime
	}
	return Cart{
		id:        id,
		userID:    userID,
		items:     []CartItem{},
		createdAt: at,
		updatedAt: at,
	}, nil
}

func (c *Cart) AddItem(productID ID, qty int) error {
	if qty <= 0 {
		return ErrInvalidQuantity
	}
	for i, item := range c.items {
		if item.productID == productID {
			newQty := item.quantity + qty
			if newQty > MaxCartItemQuantity {
				return fmt.Errorf("%w: %d", ErrMaxQuantityExceeded, newQty)
			}
			c.items[i].quantity = newQty
			return nil
		}
	}
	if qty > MaxCartItemQuantity {
		return fmt.Errorf("%w: %d", ErrMaxQuantityExceeded, qty)
	}
	c.items = append(c.items, CartItem{productID: productID, quantity: qty})
	return nil
}

func (c *Cart) UpdateQuantity(productID ID, qty int) error {
	if qty <= 0 {
		return ErrInvalidQuantity
	}
	if qty > MaxCartItemQuantity {
		return fmt.Errorf("%w: %d", ErrMaxQuantityExceeded, qty)
	}
	for i, item := range c.items {
		if item.productID == productID {
			c.items[i].quantity = qty
			return nil
		}
	}
	return ErrItemNotFound
}

func (c *Cart) RemoveItem(productID ID) error {
	for i, item := range c.items {
		if item.productID == productID {
			c.items = append(c.items[:i], c.items[i+1:]...)
			return nil
		}
	}
	return ErrItemNotFound
}

func (c Cart) ID() ID               { return c.id }
func (c Cart) UserID() ID           { return c.userID }
func (c Cart) CreatedAt() time.Time { return c.createdAt }
func (c Cart) UpdatedAt() time.Time { return c.updatedAt }

func (c Cart) Items() []CartItem {
	out := make([]CartItem, len(c.items))
	copy(out, c.items)
	return out
}
