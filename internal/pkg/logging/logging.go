// Package logging configures structured logging with PII redaction.
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

const redacted = "[REDACTED]"

// sensitiveKeys matches attribute keys case-insensitively. Per §16: tokens,
// passwords, phone numbers, card data, raw webhook bodies never reach logs.
var sensitiveKeys = map[string]struct{}{
	"password":      {},
	"password_hash": {},
	"new_password":  {},
	"old_password":  {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"id_token":      {},
	"authorization": {},
	"api_key":       {},
	"secret":        {},
	"client_secret": {},
	"private_key":   {},
	"phone":         {},
	"phone_e164":    {},
	"msisdn":        {},
	"card_number":   {},
	"pan":           {},
	"cvv":           {},
	"cvc":           {},
	"pin":           {},
	"otp":           {},
	"csrf_token":    {},
	"session":       {},
	"cookie":        {},
	"set_cookie":    {},
	"raw_body":      {},
	"webhook_body":  {},
	"raw_webhook":   {},
}

// NewJSON returns a JSON handler that redacts sensitive attribute values.
func NewJSON(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	return Wrap(slog.NewJSONHandler(w, opts))
}

// Wrap returns a handler that redacts sensitive attribute values before
// delegating to next.
func Wrap(next slog.Handler) slog.Handler { return &handler{next: next} }

type handler struct {
	next slog.Handler
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

// WithAttrs must redact here: pre-attached attrs are merged by the
// underlying handler, not visible in Handle.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &handler{next: h.next.WithAttrs(redacted)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if isSensitive(a.Key) {
		return slog.String(a.Key, redacted)
	}
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		redacted := make([]slog.Attr, len(group))
		for i, child := range group {
			redacted[i] = redactAttr(child)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	}
	return a
}

func isSensitive(key string) bool {
	if key == "" {
		return false
	}
	_, ok := sensitiveKeys[strings.ToLower(key)]
	return ok
}
