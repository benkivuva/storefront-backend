package service

import (
	"context"
	"errors"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

var ErrOrderNotCancellable = errors.New("service: order can only be cancelled while awaiting payment")

type OrderService struct {
	orders domain.OrderRepository
	clock  domain.Clock
}

func NewOrderService(orders domain.OrderRepository, clock domain.Clock) *OrderService {
	return &OrderService{orders: orders, clock: clock}
}

func (s *OrderService) ListOrders(ctx context.Context, userID domain.ID) ([]*domain.Order, error) {
	return s.orders.ListByUser(ctx, userID)
}

func (s *OrderService) GetOrder(ctx context.Context, userID, orderID domain.ID) (*domain.Order, error) {
	o, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o.UserID() != userID {
		return nil, domain.ErrNotFound
	}
	return o, nil
}

// CancelOrder transitions a PENDING_PAYMENT order to CANCELLED and releases
// reserved stock in the same transaction.
func (s *OrderService) CancelOrder(ctx context.Context, userID, orderID domain.ID) (*domain.Order, error) {
	o, err := s.GetOrder(ctx, userID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status() != domain.StatusPendingPayment {
		return nil, ErrOrderNotCancellable
	}

	now := s.clock.Now()
	if err := o.Transition(domain.StatusCancelled, "customer", "cancelled by customer", now); err != nil {
		return nil, err
	}
	if err := s.orders.Cancel(ctx, o); err != nil {
		return nil, err
	}
	return o, nil
}
