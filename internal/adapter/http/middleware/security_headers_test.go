package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders_Prod(t *testing.T) {
	h := SecurityHeaders(SecurityHeadersConfig{Prod: true})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	required := map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
	}
	for k, want := range required {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy missing")
	}
}

func TestSecurityHeaders_Dev_NoHSTS(t *testing.T) {
	h := SecurityHeaders(SecurityHeadersConfig{Prod: false})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must be absent in dev")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("X-Content-Type-Options must still be present in dev")
	}
}

func TestNoStore(t *testing.T) {
	h := NoStore()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
