package service

import (
	"context"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type OrderService struct {
	orders domain.OrderRepository
}

func NewOrderService(orders domain.OrderRepository) *OrderService {
	return &OrderService{orders: orders}
}

func (s *OrderService) ListOrders(ctx context.Context, userID domain.ID) ([]*domain.Order, error) {
	return s.orders.ListByUser(ctx, userID)
}

func (s *OrderService) GetOrder(ctx context.Context, userID, orderID domain.ID) (*domain.Order, error) {
	o, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	// Prevent horizontal privilege escalation: a user can only view their own orders.
	if o.UserID() != userID {
		return nil, domain.ErrNotFound
	}
	return o, nil
}
