package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

var (
	ErrInvalidStatus     = errors.New("domain: invalid order status")
	ErrInvalidTransition = errors.New("domain: illegal order status transition")
	ErrEmptyActor        = errors.New("domain: order transition requires an actor")
	ErrZeroTime          = errors.New("domain: order transition requires a timestamp")
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

// CanTransitionTo reports whether an order may move from s to next.
// CANCELLED and REFUNDED are terminal.
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

// StatusChange is one entry in an order's audit trail.
type StatusChange struct {
	From, To Status
	Actor    string
	Reason   string
	At       time.Time
}

// Order is the order aggregate. Status changes only through Transition.
type Order struct {
	id      ID
	userID  ID
	status  Status
	history []StatusChange
}

// RehydrateOrder rebuilds an Order from stored state.
func RehydrateOrder(id, userID ID, status Status, history []StatusChange) (*Order, error) {
	if !status.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, status)
	}
	return &Order{id: id, userID: userID, status: status, history: slices.Clone(history)}, nil
}

// Transition moves the order to next and records it. On error the order is
// unchanged. The caller supplies at so tests and services control the clock.
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
	return nil
}

func (o *Order) ID() ID         { return o.id }
func (o *Order) UserID() ID     { return o.userID }
func (o *Order) Status() Status { return o.status }

// History returns a copy of the audit trail, oldest first.
func (o *Order) History() []StatusChange { return slices.Clone(o.history) }