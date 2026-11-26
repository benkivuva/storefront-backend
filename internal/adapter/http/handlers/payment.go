package handlers

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

type PaymentService interface {
	InitiatePayment(ctx context.Context, userID, orderID domain.ID, method domain.PaymentMethod, customerPhone string) (domain.InitiateResult, error)
	GetPaymentStatus(ctx context.Context, userID, orderID domain.ID) (*domain.Payment, error)
	ListOrphanedPayments(ctx context.Context, limit int) ([]*domain.Payment, error)
	ProcessWebhook(ctx context.Context, rawBody []byte, headers map[string]string) error
}

type PaymentHandler struct {
	svc PaymentService
}

func NewPaymentHandler(svc PaymentService) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

func (h *PaymentHandler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	orderID, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid order id")
		return
	}

	var req dto.InitiatePaymentRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}

	res, err := h.svc.InitiatePayment(r.Context(), u.UserID, orderID, domain.PaymentMethod(req.Method), req.Phone)
	if err != nil {
		dto.WriteError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, dto.InitiatePaymentResponse{
		PaymentID:         res.PaymentID.String(),
		ProviderReference: res.ProviderRef,
		Provider:          res.Provider,
		Status:            "PENDING",
		NextAction: dto.NextAction{
			Type: res.NextAction.Type,
			URL:  res.NextAction.URL,
		},
	})
}

func (h *PaymentHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	orderID, err := domain.ParseID(chi.URLParam(r, "id"))
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid order id")
		return
	}

	payment, err := h.svc.GetPaymentStatus(r.Context(), u.UserID, orderID)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.FromPayment(payment))
}

func (h *PaymentHandler) ListOrphaned(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	payments, err := h.svc.ListOrphanedPayments(r.Context(), limit)
	if err != nil {
		dto.WriteError(w, err)
		return
	}

	resp := make([]dto.PaymentStatus, len(payments))
	for i, p := range payments {
		resp[i] = dto.FromPayment(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"payments": resp})
}

func (h *PaymentHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_BODY", "could not read body")
		return
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	if err := h.svc.ProcessWebhook(r.Context(), body, headers); err != nil {
		dto.WriteError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}
