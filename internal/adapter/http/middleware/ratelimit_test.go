package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type fakeLimiter struct {
	decision Decision
	err      error
	mu       sync.Mutex
	calls    int
	lastKey  string
}

func (f *fakeLimiter) Allow(_ context.Context, key string, _ int) (Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastKey = key
	return f.decision, f.err
}

func allow(remaining int) Decision        { return Decision{Allowed: true, Remaining: remaining} }
func reject(retry time.Duration) Decision { return Decision{Allowed: false, RetryAfter: retry} }

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func assertProblem(t *testing.T, body []byte, wantCode string) {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p["code"] != wantCode {
		t.Errorf("code = %v, want %q", p["code"], wantCode)
	}
}

func TestRateLimit_UnderQuota_Passes(t *testing.T) {
	l := &fakeLimiter{decision: allow(4)}
	h := RateLimit(RateLimitConfig{
		Limiter: l, Prefix: "rl", Capacity: 5,
		Key: func(r *http.Request) string { return "k" },
	})(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if l.calls != 1 {
		t.Fatalf("limiter calls = %d", l.calls)
	}
	if l.lastKey != "rl:k" {
		t.Fatalf("key = %q, want rl:k", l.lastKey)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "5" {
		t.Errorf("Limit = %q", rec.Header().Get("X-RateLimit-Limit"))
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "4" {
		t.Errorf("Remaining = %q", rec.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestRateLimit_OverQuota_429WithRetryAfter(t *testing.T) {
	l := &fakeLimiter{decision: reject(1500 * time.Millisecond)}
	called := false
	h := RateLimit(RateLimitConfig{
		Limiter: l, Prefix: "rl", Capacity: 5,
		Key: func(r *http.Request) string { return "k" },
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if called {
		t.Fatal("handler called despite rate limit")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	// 1500ms → ceil → 2
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
	assertProblem(t, rec.Body.Bytes(), "RATE_LIMITED")
}

func TestRateLimit_RetryAfterSubSecondRoundsUpToOne(t *testing.T) {
	l := &fakeLimiter{decision: reject(100 * time.Millisecond)}
	h := RateLimit(RateLimitConfig{
		Limiter: l, Prefix: "rl", Capacity: 5,
		Key: func(r *http.Request) string { return "k" },
	})(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

func TestRateLimit_PrimaryFails_FallbackAllows(t *testing.T) {
	primary := &fakeLimiter{err: errors.New("redis down")}
	fallback := &fakeLimiter{decision: allow(2)}
	h := RateLimit(RateLimitConfig{
		Limiter: primary, Fallback: fallback, Prefix: "rl", Capacity: 5,
		Key: func(r *http.Request) string { return "k" },
	})(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback should allow)", rec.Code)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("calls: primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}

func TestRateLimit_PrimaryFails_FallbackRejects(t *testing.T) {
	primary := &fakeLimiter{err: errors.New("redis down")}
	fallback := &fakeLimiter{decision: reject(2 * time.Second)}
	h := RateLimit(RateLimitConfig{
		Limiter: primary, Fallback: fallback, Prefix: "rl", Capacity: 5,
		Key: func(r *http.Request) string { return "k" },
	})(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (fallback enforces)", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q", got)
	}
}

func TestRateLimit_PrimaryFails_NoFallback_503(t *testing.T) {
	primary := &fakeLimiter{err: errors.New("redis down")}
	called := false
	h := RateLimit(RateLimitConfig{
		Limiter: primary, Prefix: "rl", Capacity: 5,
		Key: func(r *http.Request) string { return "k" },
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if called {
		t.Fatal("handler called despite 503")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblem(t, rec.Body.Bytes(), "RATE_LIMITER_UNAVAILABLE")
}

func TestIPKey_UsesClientPrefix(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.42:5678"
	got := IPKey(nil)(r)
	if got != "ip:192.168.1.0/24" {
		t.Fatalf("got %q", got)
	}
}

func TestIPKey_IgnoresSpoofedXFF(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	got := IPKey(nil)(r)
	if got != "ip:1.2.3.0/24" {
		t.Fatalf("got %q, want ip:1.2.3.0/24 (XFF ignored)", got)
	}
}

func TestIPKey_TrustedXFFHonored(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:5678"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	got := IPKey(trusted)(r)
	if got != "ip:9.9.9.0/24" {
		t.Fatalf("got %q, want ip:9.9.9.0/24", got)
	}
}

func TestAccountIPKey_WithUser(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:5678"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	r = withUser(r, Token{UserID: testUserID, Role: domain.RoleCustomer})

	got := AccountIPKey(trusted)(r)
	want := "acct:" + testUserID.String() + "|9.9.9.0/24"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAccountIPKey_NoUser_UsesAnonBucket(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	got := AccountIPKey(nil)(r)
	if got != "anon:1.2.3.0/24" {
		t.Fatalf("got %q, want anon:1.2.3.0/24", got)
	}
}

func TestRateLimit_Panic_OnMissingLimiter(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	RateLimit(RateLimitConfig{Key: func(r *http.Request) string { return "k" }})
}

func TestRateLimit_Panic_OnMissingKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	RateLimit(RateLimitConfig{Limiter: &fakeLimiter{}})
}
