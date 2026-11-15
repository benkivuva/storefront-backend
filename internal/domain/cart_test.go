package domain

import (
	"errors"
	"testing"
	"time"
)

func TestCart(t *testing.T) {
	cartID := mustID(t, sampleID)
	userID := mustID(t, otherID)
	productID := mustID(t, "11111111-2222-7333-8444-555555555555")
	otherProductID := mustID(t, "22222222-3333-7444-8555-666666666666")
	now := time.Now().UTC()

	c, err := NewCart(cartID, userID, now)
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}

	if len(c.Items()) != 0 {
		t.Fatalf("expected empty cart, got %d items", len(c.Items()))
	}

	t.Run("AddItem new", func(t *testing.T) {
		if err := c.AddItem(productID, 1); err != nil {
			t.Fatalf("AddItem(1): %v", err)
		}
		if len(c.Items()) != 1 || c.Items()[0].Quantity() != 1 {
			t.Fatalf("expected 1 item with qty 1, got %+v", c.Items())
		}
	})

	t.Run("AddItem same increments", func(t *testing.T) {
		if err := c.AddItem(productID, 2); err != nil {
			t.Fatalf("AddItem(2): %v", err)
		}
		if len(c.Items()) != 1 || c.Items()[0].Quantity() != 3 {
			t.Fatalf("expected 1 item with qty 3, got %+v", c.Items())
		}
	})

	t.Run("AddItem invalid quantity", func(t *testing.T) {
		if err := c.AddItem(productID, 0); !errors.Is(err, ErrInvalidQuantity) {
			t.Errorf("AddItem(0): expected ErrInvalidQuantity, got %v", err)
		}
		if err := c.AddItem(productID, -5); !errors.Is(err, ErrInvalidQuantity) {
			t.Errorf("AddItem(-5): expected ErrInvalidQuantity, got %v", err)
		}
	})

	t.Run("AddItem max quantity exceeded", func(t *testing.T) {
		if err := c.AddItem(productID, 98); !errors.Is(err, ErrMaxQuantityExceeded) {
			t.Errorf("AddItem(98): expected ErrMaxQuantityExceeded, got %v", err)
		}
	})

	t.Run("UpdateQuantity success", func(t *testing.T) {
		if err := c.UpdateQuantity(productID, 5); err != nil {
			t.Fatalf("UpdateQuantity(5): %v", err)
		}
		if c.Items()[0].Quantity() != 5 {
			t.Fatalf("expected qty 5, got %d", c.Items()[0].Quantity())
		}
	})

	t.Run("UpdateQuantity invalid", func(t *testing.T) {
		if err := c.UpdateQuantity(productID, 0); !errors.Is(err, ErrInvalidQuantity) {
			t.Errorf("UpdateQuantity(0): expected ErrInvalidQuantity, got %v", err)
		}
		if err := c.UpdateQuantity(productID, 101); !errors.Is(err, ErrMaxQuantityExceeded) {
			t.Errorf("UpdateQuantity(101): expected ErrMaxQuantityExceeded, got %v", err)
		}
	})

	t.Run("UpdateQuantity not found", func(t *testing.T) {
		if err := c.UpdateQuantity(otherProductID, 1); !errors.Is(err, ErrItemNotFound) {
			t.Errorf("expected ErrItemNotFound, got %v", err)
		}
	})

	t.Run("RemoveItem success", func(t *testing.T) {
		if err := c.RemoveItem(productID); err != nil {
			t.Fatalf("RemoveItem: %v", err)
		}
		if len(c.Items()) != 0 {
			t.Fatalf("expected empty cart after removal, got %d items", len(c.Items()))
		}
	})

	t.Run("RemoveItem not found", func(t *testing.T) {
		if err := c.RemoveItem(productID); !errors.Is(err, ErrItemNotFound) {
			t.Errorf("expected ErrItemNotFound, got %v", err)
		}
	})

	t.Run("Immutable Items slice", func(t *testing.T) {
		_ = c.AddItem(productID, 1)
		items := c.Items()
		items[0] = CartItem{productID: otherProductID, quantity: 99}
		if c.Items()[0].ProductID() != productID {
			t.Fatal("modifying returned slice modified internal state")
		}
	})
}
