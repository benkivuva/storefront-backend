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

// MeService is the subset of *service.AuthService the /me handlers use.
type MeService interface {
	GetMe(ctx context.Context, userID domain.ID) (domain.User, error)
	UpdateMe(ctx context.Context, userID domain.ID, patch service.UpdateMePatch) (domain.User, error)
	ListAddresses(ctx context.Context, userID domain.ID) ([]domain.Address, error)
	CreateAddress(ctx context.Context, userID domain.ID, in service.CreateAddressInput) (domain.Address, error)
	ListSessions(ctx context.Context, userID domain.ID) ([]service.SessionInfo, error)
	RevokeSession(ctx context.Context, userID, tokenID domain.ID) error
}

type MeHandler struct {
	svc MeService
}

func NewMeHandler(svc MeService) *MeHandler {
	return &MeHandler{svc: svc}
}

func (h *MeHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	user, err := h.svc.GetMe(r.Context(), u.UserID)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MeResponse{User: dto.FromUser(user)})
}

type updateMeRequest struct {
	Name  *string `json:"name"`
	Phone *string `json:"phone"`
}

func (h *MeHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req updateMeRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}
	user, err := h.svc.UpdateMe(r.Context(), u.UserID, service.UpdateMePatch{
		Name:  req.Name,
		Phone: req.Phone,
	})
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MeResponse{User: dto.FromUser(user)})
}

func (h *MeHandler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	addrs, err := h.svc.ListAddresses(r.Context(), u.UserID)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.AddressesResponse{Addresses: dto.FromAddresses(addrs)})
}

type createAddressRequest struct {
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Line1     string `json:"line1"`
	Line2     string `json:"line2"`
	Town      string `json:"town"`
	County    string `json:"county"`
	Notes     string `json:"notes"`
}

func (h *MeHandler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req createAddressRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}
	a, err := h.svc.CreateAddress(r.Context(), u.UserID, service.CreateAddressInput{
		Recipient: req.Recipient,
		Phone:     req.Phone,
		Line1:     req.Line1,
		Line2:     req.Line2,
		Town:      req.Town,
		County:    req.County,
		Notes:     req.Notes,
	})
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.FromAddress(a))
}

func (h *MeHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ss, err := h.svc.ListSessions(r.Context(), u.UserID)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.SessionsResponse{Sessions: dto.FromSessions(ss)})
}

func (h *MeHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := domain.ParseID(idStr)
	if err != nil {
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_ID", "invalid session id")
		return
	}
	if err := h.svc.RevokeSession(r.Context(), u.UserID, id); err != nil {
		dto.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireUser pulls the authenticated token from context. Auth middleware
// guarantees presence; absence is a router-wiring bug — fail closed with 401.
func requireUser(w http.ResponseWriter, r *http.Request) (middleware.Token, bool) {
	u, ok := middleware.UserFrom(r.Context())
	if !ok {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return middleware.Token{}, false
	}
	return u, true
}
