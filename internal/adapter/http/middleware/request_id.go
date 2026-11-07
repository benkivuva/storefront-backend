// Package middleware provides HTTP middleware for the storefront API.
// Mount order matters: RequestID first, then SecurityHeaders, then
// BodyLimit, then per-route chain (rate limit, auth).
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

const requestIDHeader = "X-Request-ID"

type ctxKey int

const requestIDKey ctxKey = iota

// validRequestID rejects client-supplied IDs that could be used for header
// injection (newlines, control chars), log forgery, or unbounded-log DoS.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// RequestID ensures every request has an ID. A valid client-supplied
// X-Request-ID is echoed; anything else is replaced with a fresh 128-bit hex
// ID. The ID is set on the response BEFORE the handler runs so it survives
// panics and error paths, and is available via RequestIDFrom.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			if !validRequestID.MatchString(id) {
				id = newRequestID()
			}
			w.Header().Set(requestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFrom returns the request ID stored in ctx, or "" if none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
