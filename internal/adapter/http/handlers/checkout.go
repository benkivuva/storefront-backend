package handlers

import (
	"context"
	"net/http"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

type CheckoutService interface {
	Checkout(ctx context.Context, userID, addressID domain.ID, expectedTotalMinor int64, idempotencyKey string) (*domain.Order, error)
}

type CheckoutHandler struct {
	svc CheckoutService
}

func NewCheckoutHandler(svc CheckoutService) *CheckoutHandler {
	return &CheckoutHandler{svc: svc}
}

type checkoutRequest struct {
	AddressID          string `json:"address_id"`
	ExpectedTotalMinor int64  `json:"expected_total_minor"`
}

func (h *CheckoutHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	var req checkoutRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}

	addrID, err := domain.ParseID(req.AddressID)
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid address id")
		return
	}

	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		dto.WriteProblem(w, http.StatusBadRequest, "MISSING_IDEMPOTENCY_KEY", "Idempotency-Key header is required")
		return
	}

	order, err := h.svc.Checkout(r.Context(), u.UserID, addrID, req.ExpectedTotalMinor, idemKey)
	if err != nil {
		dto.WriteError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, dto.FromOrder(order))
}
