package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_AllowedOriginSetsHeader(t *testing.T) {
	allowed := []string{"https://storefront.example.com"}
	h := CORS(allowed, true)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://storefront.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://storefront.example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("ACAC = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q", got)
	}
}

func TestCORS_DisallowedOriginNoHeader(t *testing.T) {
	allowed := []string{"https://storefront.example.com"}
	h := CORS(allowed, true)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ACAO = %q (must be absent)", got)
	}
}

func TestCORS_NoOriginHeaderNoCORS(t *testing.T) {
	h := CORS([]string{"https://a.example.com"}, true)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ACAO = %q (must be absent)", got)
	}
}

func TestCORS_PreflightFromAllowedOrigin(t *testing.T) {
	allowed := []string{"https://storefront.example.com"}
	h := CORS(allowed, true)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight must not reach handler")
	}))
	r := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	r.Header.Set("Origin", "https://storefront.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods missing")
	}
}

func TestCORS_PreflightFromDisallowedOrigin_403(t *testing.T) {
	h := CORS([]string{"https://a.example.com"}, true)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight must not reach handler")
	}))
	r := httptest.NewRequest("OPTIONS", "/", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
