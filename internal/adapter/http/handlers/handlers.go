// Package handlers implements the HTTP handlers for the storefront API.
// Every handler parses its request, calls the service, and formats a DTO
// response. Business logic lives in the service layer, never here.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
)

var (
	errDecodeRequest        = errors.New("handlers: invalid request body")
	errUnsupportedMediaType = errors.New("handlers: unsupported media type")
)

func decodeJSON(r *http.Request, dst any) error {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return errUnsupportedMediaType
	}

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if middleware.BodyTooLarge(err) {
			return middleware.ErrBodyTooLarge
		}
		return errDecodeRequest
	}
	if dec.More() {
		return errDecodeRequest
	}
	return nil
}

func handleDecodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, middleware.ErrBodyTooLarge):
		dto.WriteProblem(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body too large")
	case errors.Is(err, errUnsupportedMediaType):
		dto.WriteProblem(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "content-type must be application/json")
	default:
		dto.WriteProblem(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "error", err)
	}
}
