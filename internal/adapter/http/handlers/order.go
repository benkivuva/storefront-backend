package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

type OrderService interface {
	ListOrders(ctx context.Context, userID domain.ID) ([]*domain.Order, error)
	GetOrder(ctx context.Context, userID, orderID domain.ID) (*domain.Order, error)
	CancelOrder(ctx context.Context, userID, orderID domain.ID) (*domain.Order, error)
}

type OrderHandler struct {
	svc OrderService
}

func NewOrderHandler(svc OrderService) *OrderHandler {
	return &OrderHandler{svc: svc}
}

func (h *OrderHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	orders, err := h.svc.ListOrders(r.Context(), u.UserID)
	if err != nil {
		dto.WriteError(w, err)
		return
	}

	resp := make([]dto.Order, len(orders))
	for i, o := range orders {
		resp[i] = dto.FromOrder(o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": resp})
}

func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	id, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid order id")
		return
	}

	o, err := h.svc.GetOrder(r.Context(), u.UserID, id)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromOrder(o))
}

func (h *OrderHandler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	id, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid order id")
		return
	}

	o, err := h.svc.CancelOrder(r.Context(), u.UserID, id)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromOrder(o))
}
