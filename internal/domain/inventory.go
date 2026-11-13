package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNegativeStock = errors.New("domain: stock cannot be negative")
	ErrInvalidReason = errors.New("domain: invalid stock movement reason")
)

type StockReason string

const (
	ReasonOrderReserve StockReason = "ORDER_RESERVE"
	ReasonOrderRelease StockReason = "ORDER_RELEASE"
	ReasonRestock      StockReason = "RESTOCK"
	ReasonAdjustment   StockReason = "ADJUSTMENT"
	ReasonSeed         StockReason = "SEED"
)

func (r StockReason) IsValid() bool {
	switch r {
	case ReasonOrderReserve, ReasonOrderRelease, ReasonRestock, ReasonAdjustment, ReasonSeed:
		return true
	}
	return false
}

type Inventory struct {
	productID         ID
	available         int
	lowStockThreshold int
	updatedAt         time.Time
}

func NewInventory(productID ID, available, threshold int, at time.Time) (Inventory, error) {
	if available < 0 {
		return Inventory{}, fmt.Errorf("%w: %d", ErrNegativeStock, available)
	}
	if threshold < 0 {
		threshold = 0
	}
	return Inventory{
		productID:         productID,
		available:         available,
		lowStockThreshold: threshold,
		updatedAt:         at,
	}, nil
}

// RehydrateInventory rebuilds an Inventory from persisted state without validation.
func RehydrateInventory(productID ID, available, threshold int, at time.Time) Inventory {
	return Inventory{
		productID:         productID,
		available:         available,
		lowStockThreshold: threshold,
		updatedAt:         at,
	}
}

func (i Inventory) ProductID() ID        { return i.productID }
func (i Inventory) Available() int       { return i.available }
func (i Inventory) Threshold() int       { return i.lowStockThreshold }
func (i Inventory) UpdatedAt() time.Time { return i.updatedAt }

type StockMovement struct {
	id        ID
	productID ID
	delta     int
	reason    StockReason
	orderID   ID
	actor     ID
	createdAt time.Time
}

func NewStockMovement(id, productID ID, delta int, reason StockReason, orderID, actor ID, at time.Time) (StockMovement, error) {
	if !reason.IsValid() {
		return StockMovement{}, fmt.Errorf("%w: %q", ErrInvalidReason, reason)
	}
	if delta == 0 {
		return StockMovement{}, fmt.Errorf("domain: stock movement delta cannot be zero")
	}
	return StockMovement{
		id:        id,
		productID: productID,
		delta:     delta,
		reason:    reason,
		orderID:   orderID,
		actor:     actor,
		createdAt: at,
	}, nil
}

func (m StockMovement) ID() ID               { return m.id }
func (m StockMovement) ProductID() ID        { return m.productID }
func (m StockMovement) Delta() int           { return m.delta }
func (m StockMovement) Reason() StockReason  { return m.reason }
func (m StockMovement) OrderID() ID          { return m.orderID }
func (m StockMovement) Actor() ID            { return m.actor }
func (m StockMovement) CreatedAt() time.Time { return m.createdAt }
