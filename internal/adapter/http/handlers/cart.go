package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

type CartService interface {
	GetCartView(ctx context.Context, userID domain.ID) (service.CartView, error)
	AddItem(ctx context.Context, userID, productID domain.ID, qty int) error
	UpdateQuantity(ctx context.Context, userID, productID domain.ID, qty int) error
	RemoveItem(ctx context.Context, userID, productID domain.ID) error
}

type CartHandler struct {
	svc CartService
}

func NewCartHandler(svc CartService) *CartHandler {
	return &CartHandler{svc: svc}
}

func (h *CartHandler) GetCart(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	view, err := h.svc.GetCartView(r.Context(), u.UserID)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromCartView(view))
}

type addItemRequest struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

func (h *CartHandler) AddItem(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	var req addItemRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}

	productID, err := domain.ParseID(req.ProductID)
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid product id")
		return
	}

	if err := h.svc.AddItem(r.Context(), u.UserID, productID, req.Quantity); err != nil {
		dto.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateQuantityRequest struct {
	Quantity int `json:"quantity"`
}

func (h *CartHandler) UpdateQuantity(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	productID, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid product id")
		return
	}

	var req updateQuantityRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}

	if err := h.svc.UpdateQuantity(r.Context(), u.UserID, productID, req.Quantity); err != nil {
		dto.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CartHandler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	productID, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid product id")
		return
	}

	if err := h.svc.RemoveItem(r.Context(), u.UserID, productID); err != nil {
		dto.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
