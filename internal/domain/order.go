package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidStatus     = errors.New("domain: invalid order status")
	ErrInvalidTransition = errors.New("domain: illegal order status transition")
	ErrEmptyActor        = errors.New("domain: order transition requires an actor")
	ErrZeroTime          = errors.New("domain: order transition requires a timestamp")
	ErrEmptyOrderItems   = errors.New("domain: order must have at least one item")
)

type Status string

const (
	StatusPendingPayment Status = "PENDING_PAYMENT"
	StatusPaid           Status = "PAID"
	StatusFulfilling     Status = "FULFILLING"
	StatusShipped        Status = "SHIPPED"
	StatusDelivered      Status = "DELIVERED"
	StatusCancelled      Status = "CANCELLED"
	StatusRefundPending  Status = "REFUND_PENDING"
	StatusRefunded       Status = "REFUNDED"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusPendingPayment, StatusPaid, StatusFulfilling, StatusShipped,
		StatusDelivered, StatusCancelled, StatusRefundPending, StatusRefunded:
		return true
	}
	return false
}

func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusPendingPayment:
		return next == StatusPaid || next == StatusCancelled
	case StatusPaid:
		return next == StatusFulfilling || next == StatusRefundPending
	case StatusFulfilling:
		return next == StatusShipped || next == StatusRefundPending
	case StatusShipped:
		return next == StatusDelivered
	case StatusDelivered:
		return next == StatusRefundPending
	case StatusRefundPending:
		return next == StatusRefunded
	}
	return false
}

type StatusChange struct {
	From, To Status
	Actor    string
	Reason   string
	At       time.Time
}

type OrderItemSnapshot struct {
	ProductID      ID
	SKU            string
	ProductName    string
	Quantity       int
	UnitPrice      Money
	TaxClass       TaxClass
	TaxRateBp      int
	LineVat        Money
	LineGrossMinor int64
}

type Order struct {
	id               ID
	userID           ID
	addressID        ID
	items            []OrderItemSnapshot
	deliveryFee      Money
	subtotalMinor    int64
	totalMinor       int64
	status           Status
	history          []StatusChange
	paymentExpiresAt time.Time
	createdAt        time.Time
	updatedAt        time.Time
}

func NewOrder(
	id, userID, addressID ID,
	items []OrderItemSnapshot,
	deliveryFee Money,
	at time.Time,
	expireAt time.Time,
) (*Order, error) {
	if len(items) == 0 {
		return nil, ErrEmptyOrderItems
	}

	var subtotal int64
	for _, item := range items {
		subtotal += item.LineGrossMinor
	}
	total := subtotal + deliveryFee.AmountMinor()

	return &Order{
		id:               id,
		userID:           userID,
		addressID:        addressID,
		items:            append([]OrderItemSnapshot(nil), items...),
		deliveryFee:      deliveryFee,
		subtotalMinor:    subtotal,
		totalMinor:       total,
		status:           StatusPendingPayment,
		history:          []StatusChange{},
		paymentExpiresAt: expireAt,
		createdAt:        at,
		updatedAt:        at,
	}, nil
}

func (o *Order) Transition(next Status, actor, reason string, at time.Time) error {
	if actor == "" {
		return ErrEmptyActor
	}
	if at.IsZero() {
		return ErrZeroTime
	}
	if !o.status.CanTransitionTo(next) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, o.status, next)
	}
	o.history = append(o.history, StatusChange{From: o.status, To: next, Actor: actor, Reason: reason, At: at})
	o.status = next
	o.updatedAt = at
	return nil
}

func (o *Order) ID() ID                      { return o.id }
func (o *Order) UserID() ID                  { return o.userID }
func (o *Order) AddressID() ID               { return o.addressID }
func (o *Order) Status() Status              { return o.status }
func (o *Order) SubtotalMinor() int64        { return o.subtotalMinor }
func (o *Order) TotalMinor() int64           { return o.totalMinor }
func (o *Order) DeliveryFee() Money          { return o.deliveryFee }
func (o *Order) PaymentExpiresAt() time.Time { return o.paymentExpiresAt }
func (o *Order) CreatedAt() time.Time        { return o.createdAt }
func (o *Order) UpdatedAt() time.Time        { return o.updatedAt }

func (o *Order) Items() []OrderItemSnapshot {
	out := make([]OrderItemSnapshot, len(o.items))
	copy(out, o.items)
	return out
}

func (o *Order) History() []StatusChange {
	out := make([]StatusChange, len(o.history))
	copy(out, o.history)
	return out
}
