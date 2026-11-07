package handlers

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

const (
	refreshCookieName = "refresh_token"
	refreshCookiePath = "/api/v1/auth"
)

// AuthService is the subset of *service.AuthService the auth handlers use.
// Consumer-defined so handlers can be tested with a four-method fake instead
// of the full service.
type AuthService interface {
	Register(ctx context.Context, email, password, name, ip string) (domain.User, error)
	Login(ctx context.Context, email, password, ip, userAgent string) (service.LoginResult, error)
	Refresh(ctx context.Context, raw []byte, ip, userAgent string) (service.LoginResult, error)
	Logout(ctx context.Context, raw []byte) error
}

type AuthHandler struct {
	svc        AuthService
	trusted    []netip.Prefix
	refreshTTL time.Duration
	secure     bool
}

func NewAuthHandler(svc AuthService, trusted []netip.Prefix, refreshTTL time.Duration, secureCookies bool) *AuthHandler {
	return &AuthHandler{
		svc:        svc,
		trusted:    trusted,
		refreshTTL: refreshTTL,
		secure:     secureCookies,
	}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}
	ip := middleware.ClientIP(r, h.trusted).String()
	u, err := h.svc.Register(r.Context(), req.Email, req.Password, req.Name, ip)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.RegisterResponse{User: dto.FromUser(u)})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		handleDecodeError(w, err)
		return
	}
	ip := middleware.ClientIP(r, h.trusted).String()
	ua := r.Header.Get("User-Agent")
	res, err := h.svc.Login(r.Context(), req.Email, req.Password, ip, ua)
	if err != nil {
		dto.WriteError(w, err)
		return
	}
	h.setRefreshCookie(w, res.RefreshToken)
	writeJSON(w, http.StatusOK, dto.LoginResponse{
		AccessToken: res.AccessToken,
		User:        dto.FromUser(res.User),
	})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	raw, err := h.readRefreshCookie(r)
	if err != nil {
		dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing refresh token")
		return
	}
	ip := middleware.ClientIP(r, h.trusted).String()
	ua := r.Header.Get("User-Agent")
	res, err := h.svc.Refresh(r.Context(), raw, ip, ua)
	if err != nil {
		h.clearRefreshCookie(w)
		dto.WriteError(w, err)
		return
	}
	h.setRefreshCookie(w, res.RefreshToken)
	writeJSON(w, http.StatusOK, dto.LoginResponse{
		AccessToken: res.AccessToken,
		User:        dto.FromUser(res.User),
	})
}

// Logout always returns 204 and clears the cookie, whether or not the
// service successfully revoked the token. A failed revoke is logged; the
// client sees a clean logout either way.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	raw, err := h.readRefreshCookie(r)
	if err == nil {
		if lerr := h.svc.Logout(r.Context(), raw); lerr != nil {
			slog.ErrorContext(r.Context(), "logout revoke failed", "error", lerr)
		}
	}
	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, raw []byte) {
	// #nosec G124 -- Secure is env-driven: true in staging/prod, false for local HTTP.
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     refreshCookiePath,
		MaxAge:   int(h.refreshTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *AuthHandler) readRefreshCookie(r *http.Request) ([]byte, error) {
	c, err := r.Cookie(refreshCookieName)
	if err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(c.Value)
}

func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	// #nosec G124 -- Secure is env-driven: true in staging/prod, false for local HTTP.
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
	})
}
