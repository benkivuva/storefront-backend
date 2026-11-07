package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginCheck_AllowedOriginPasses(t *testing.T) {
	h := OriginCheck([]string{"https://storefront.example.com"})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	r.Header.Set("Origin", "https://storefront.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestOriginCheck_DisallowedOrigin_403(t *testing.T) {
	h := OriginCheck([]string{"https://a.example.com"})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("handler must not run")
		}))
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "FORBIDDEN")
}

func TestOriginCheck_NoHeaders_Passes(t *testing.T) {
	h := OriginCheck([]string{"https://a.example.com"})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (no Origin/Referer must pass)", rec.Code)
	}
}

func TestOriginCheck_RefererAllowedPasses(t *testing.T) {
	h := OriginCheck([]string{"https://storefront.example.com"})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Referer", "https://storefront.example.com/some/page")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestOriginCheck_RefererDisallowed_403(t *testing.T) {
	h := OriginCheck([]string{"https://a.example.com"})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Referer", "https://evil.example.com/page")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestOriginCheck_MalformedReferer_403(t *testing.T) {
	h := OriginCheck([]string{"https://a.example.com"})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Referer", "not-a-url")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d (malformed referer must reject)", rec.Code)
	}
}
