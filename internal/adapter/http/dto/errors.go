package dto

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

// Problem is an RFC 7807 document. Code is the stable machine-readable
// identifier the frontend branches on; Title is human-readable and may
// change without notice.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
}

type ErrorMapping struct {
	Status int
	Code   string
	Title  string
}

// errorMappings is the allowlist of errors safe to expose to the client.
// Errors not in this table map to 500 INTERNAL, so internal-only sentinels
// (ErrZeroUserID, ErrInvalidMoney, and the like) never leak their messages.
var errorMappings = []struct {
	err    error
	Status int
	Code   string
	Title  string
}{
	// Service-layer errors
	{service.ErrInvalidCredentials, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials"},
	{service.ErrTokenReused, http.StatusUnauthorized, "TOKEN_REUSED", "refresh token reuse detected"},
	{service.ErrTokenExpired, http.StatusUnauthorized, "TOKEN_EXPIRED", "token expired"},
	{service.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"},
	{service.ErrEmailTaken, http.StatusConflict, "EMAIL_TAKEN", "email already registered"},
	{service.ErrEmptyPatch, http.StatusBadRequest, "EMPTY_PATCH", "no fields to update"},
	{service.ErrAddressLimit, http.StatusConflict, "ADDRESS_LIMIT", "address limit reached"},

	// Domain errors safe to expose
	{domain.ErrNotFound, http.StatusNotFound, "NOT_FOUND", "resource not found"},
	{domain.ErrConflict, http.StatusConflict, "CONFLICT", "conflict"},
	{domain.ErrInvalidEmail, http.StatusBadRequest, "INVALID_EMAIL", "invalid email"},
	{domain.ErrInvalidPhone, http.StatusBadRequest, "INVALID_PHONE", "invalid phone number"},
	{domain.ErrInvalidName, http.StatusBadRequest, "INVALID_NAME", "invalid name"},
	{domain.ErrInvalidRole, http.StatusBadRequest, "INVALID_ROLE", "invalid role"},
	{domain.ErrInvalidRecipient, http.StatusBadRequest, "INVALID_RECIPIENT", "invalid recipient"},
	{domain.ErrInvalidLine1, http.StatusBadRequest, "INVALID_LINE1", "invalid address line 1"},
	{domain.ErrInvalidTown, http.StatusBadRequest, "INVALID_TOWN", "invalid town"},
	{domain.ErrInvalidID, http.StatusBadRequest, "INVALID_ID", "invalid identifier"},
	{domain.ErrPasswordTooShort, http.StatusBadRequest, "PASSWORD_TOO_SHORT", "password too short"},
	{domain.ErrPasswordTooLong, http.StatusBadRequest, "PASSWORD_TOO_LONG", "password too long"},
	{domain.ErrPasswordCommon, http.StatusBadRequest, "PASSWORD_TOO_COMMON", "password too common"},
}

// FromError maps err to (status, code, title). Uses errors.Is, so wrapped
// errors match. Unmapped errors return 500 INTERNAL with a generic title —
// the client never sees internal detail.
func FromError(err error) ErrorMapping {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			return ErrorMapping{Status: m.Status, Code: m.Code, Title: m.Title}
		}
	}
	return ErrorMapping{
		Status: http.StatusInternalServerError,
		Code:   "INTERNAL",
		Title:  "internal server error",
	}
}

func WriteProblem(w http.ResponseWriter, status int, code, title string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Code:   code,
	})
}

// WriteProblemExtra writes a problem with additional top-level fields —
// used for e.g. 413 with max_bytes.
func WriteProblemExtra(w http.ResponseWriter, status int, code, title string, extras map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	body := map[string]any{
		"type":   "about:blank",
		"title":  title,
		"status": status,
		"code":   code,
	}
	for k, v := range extras {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

// WriteError maps err and writes the corresponding problem response.
func WriteError(w http.ResponseWriter, err error) {
	m := FromError(err)
	WriteProblem(w, m.Status, m.Code, m.Title)
}
