// Package handlers implements the HTTP handlers for the storefront API.
// Every handler parses its request, calls the service, and formats a DTO
// response. Business logic lives in the service layer, never here.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
)

var errDecodeRequest = errors.New("handlers: invalid request body")

// decodeJSON reads r.Body into dst, rejecting unknown fields and trailing
// data after the first JSON value. Both are 400-worthy client errors.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errDecodeRequest
	}
	if dec.More() {
		return errDecodeRequest
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "error", err)
	}
}

func writeDecodeError(w http.ResponseWriter) {
	dto.WriteProblem(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
}
