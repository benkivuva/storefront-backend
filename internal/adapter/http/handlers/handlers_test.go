package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
)

func TestHandleDecodeError_BodyTooLarge_413(t *testing.T) {
	rec := httptest.NewRecorder()
	handleDecodeError(rec, middleware.ErrBodyTooLarge)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "BODY_TOO_LARGE")
}

func TestHandleDecodeError_InvalidJSON_400(t *testing.T) {
	rec := httptest.NewRecorder()
	handleDecodeError(rec, errDecodeRequest)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "INVALID_REQUEST")
}

func TestHandleDecodeError_WrappedBodyTooLarge_413(t *testing.T) {
	// handleDecodeError uses errors.Is, so a wrapped sentinel must still
	// route to 413. Guards against a refactor that compares via ==.
	wrapped := errors.Join(errors.New("read failed"), middleware.ErrBodyTooLarge)
	rec := httptest.NewRecorder()
	handleDecodeError(rec, wrapped)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}
