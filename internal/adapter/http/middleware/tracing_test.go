package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

func TestTracing_PropagatesIncomingTraceparent(t *testing.T) {
	const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	var got string
	h := Tracing()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = r.Context().Value(domain.TraceparentKey).(string)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("traceparent", tp)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got != tp {
		t.Fatalf("traceparent = %q, want %q", got, tp)
	}
}

func TestTracing_NoHeaderStartsSpan(t *testing.T) {
	var present bool
	h := Tracing()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, present = r.Context().Value(domain.TraceparentKey).(string)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if present {
		t.Fatal("expected no traceparent without an incoming header or active tracer")
	}
}
