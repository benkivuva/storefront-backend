package dto

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

func TestFromError_AllKnown(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrInvalidCredentials, 401, "INVALID_CREDENTIALS"},
		{service.ErrTokenReused, 401, "TOKEN_REUSED"},
		{service.ErrTokenExpired, 401, "TOKEN_EXPIRED"},
		{service.ErrUnauthorized, 401, "UNAUTHORIZED"},
		{service.ErrEmailTaken, 409, "EMAIL_TAKEN"},
		{service.ErrEmptyPatch, 400, "EMPTY_PATCH"},
		{service.ErrAddressLimit, 409, "ADDRESS_LIMIT"},
		{domain.ErrNotFound, 404, "NOT_FOUND"},
		{domain.ErrConflict, 409, "CONFLICT"},
		{domain.ErrInvalidEmail, 400, "INVALID_EMAIL"},
		{domain.ErrInvalidPhone, 400, "INVALID_PHONE"},
		{domain.ErrInvalidName, 400, "INVALID_NAME"},
		{domain.ErrPasswordTooShort, 400, "PASSWORD_TOO_SHORT"},
		{domain.ErrPasswordTooLong, 400, "PASSWORD_TOO_LONG"},
		{domain.ErrPasswordCommon, 400, "PASSWORD_TOO_COMMON"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			m := FromError(tc.err)
			if m.Status != tc.status {
				t.Errorf("status = %d, want %d", m.Status, tc.status)
			}
			if m.Code != tc.code {
				t.Errorf("code = %q, want %q", m.Code, tc.code)
			}
			if m.Title == "" {
				t.Error("title empty")
			}
		})
	}
}

func TestFromError_UnknownIsInternal(t *testing.T) {
	m := FromError(errors.New("some random failure"))
	if m.Status != http.StatusInternalServerError {
		t.Errorf("status = %d", m.Status)
	}
	if m.Code != "INTERNAL" {
		t.Errorf("code = %q", m.Code)
	}
	if m.Title == "some random failure" {
		t.Error("title leaked error message")
	}
}

func TestFromError_WrappedStillMatches(t *testing.T) {
	wrapped := fmt.Errorf("login: find: %w", domain.ErrNotFound)
	if m := FromError(wrapped); m.Code != "NOT_FOUND" {
		t.Errorf("wrapped not matched; code = %q", m.Code)
	}
}

func TestFromError_InternalSentinelsMapToInternal(t *testing.T) {
	// Sentinels that must not leak to the client.
	internal := []error{
		domain.ErrZeroUserID,
		domain.ErrZeroUserTime,
		domain.ErrInvalidMoney,
		domain.ErrOverflow,
		domain.ErrZeroAddressID,
	}
	for _, err := range internal {
		m := FromError(err)
		if m.Code != "INTERNAL" {
			t.Errorf("%v: code = %q, want INTERNAL", err, m.Code)
		}
	}
}

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteProblem(rec, http.StatusBadRequest, "INVALID_EMAIL", "invalid email")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Type != "about:blank" || p.Code != "INVALID_EMAIL" || p.Status != 400 {
		t.Errorf("body = %+v", p)
	}
}

func TestWriteProblemExtra(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteProblemExtra(rec, http.StatusRequestEntityTooLarge,
		"BODY_TOO_LARGE", "request body too large",
		map[string]any{"max_bytes": 1048576})

	var p map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p["code"] != "BODY_TOO_LARGE" {
		t.Errorf("code = %v", p["code"])
	}
	if p["max_bytes"] != float64(1048576) {
		t.Errorf("max_bytes = %v", p["max_bytes"])
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, domain.ErrInvalidEmail)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Code != "INVALID_EMAIL" {
		t.Errorf("code = %q", p.Code)
	}
}
