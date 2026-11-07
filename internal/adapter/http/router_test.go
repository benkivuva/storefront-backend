package httpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/handlers"
	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

var routerTestUserID = domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

type fakeAuthSvc struct{}

func (f *fakeAuthSvc) Register(_ context.Context, email, _, _, _ string) (domain.User, error) {
	u, err := domain.NewUser(routerTestUserID, email, "h", "", domain.RoleCustomer, "", time.Now().UTC())
	return u, err
}
func (f *fakeAuthSvc) Login(_ context.Context, _, _, _, _ string) (service.LoginResult, error) {
	return service.LoginResult{
		AccessToken:  "access",
		RefreshToken: []byte("refresh-raw"),
		User:         mustRouterUser(),
	}, nil
}
func (f *fakeAuthSvc) Refresh(_ context.Context, _ []byte, _, _ string) (service.LoginResult, error) {
	return service.LoginResult{
		AccessToken:  "access",
		RefreshToken: []byte("refresh-raw-2"),
		User:         mustRouterUser(),
	}, nil
}
func (f *fakeAuthSvc) Logout(_ context.Context, _ []byte) error { return nil }

type fakeMeSvc struct{}

func (f *fakeMeSvc) GetMe(_ context.Context, _ domain.ID) (domain.User, error) {
	return mustRouterUser(), nil
}
func (f *fakeMeSvc) UpdateMe(_ context.Context, _ domain.ID, _ service.UpdateMePatch) (domain.User, error) {
	return mustRouterUser(), nil
}
func (f *fakeMeSvc) ListAddresses(_ context.Context, _ domain.ID) ([]domain.Address, error) {
	return nil, nil
}
func (f *fakeMeSvc) CreateAddress(_ context.Context, _ domain.ID, _ service.CreateAddressInput) (domain.Address, error) {
	return domain.Address{}, errors.New("not implemented in fake")
}
func (f *fakeMeSvc) ListSessions(_ context.Context, _ domain.ID) ([]service.SessionInfo, error) {
	return nil, nil
}
func (f *fakeMeSvc) RevokeSession(_ context.Context, _, _ domain.ID) error { return nil }

func mustRouterUser() domain.User {
	u, err := domain.NewUser(routerTestUserID, "user@example.com", "h", "Alice",
		domain.RoleCustomer, "", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		panic(err)
	}
	return u
}

type allowAllLimiter struct{}

func (allowAllLimiter) Allow(_ context.Context, _ string, _ int) (middleware.Decision, error) {
	return middleware.Decision{Allowed: true, Remaining: 99}, nil
}

type denyAllLimiter struct{}

func (denyAllLimiter) Allow(_ context.Context, _ string, _ int) (middleware.Decision, error) {
	return middleware.Decision{Allowed: false, RetryAfter: time.Second}, nil
}

type fakeJWTVerifier struct{}

func (fakeJWTVerifier) Verify(raw string) (middleware.Token, error) {
	if raw == "good" {
		return middleware.Token{UserID: routerTestUserID, Role: domain.RoleCustomer}, nil
	}
	return middleware.Token{}, errors.New("invalid token")
}

func buildTestRouter(t *testing.T, limiter middleware.RateLimiter) http.Handler {
	t.Helper()
	authHandler := handlers.NewAuthHandler(&fakeAuthSvc{}, nil, time.Hour, false)
	meHandler := handlers.NewMeHandler(&fakeMeSvc{})
	return NewPublicRouter(RouterConfig{
		AuthHandler:    authHandler,
		MeHandler:      meHandler,
		JWTVerifier:    fakeJWTVerifier{},
		AuthLimiter:    limiter,
		GlobalLimiter:  limiter,
		AllowedOrigins: []string{"https://storefront.example.com"},
		AuthCapacity:   5,
		GlobalCapacity: 100,
		Prod:           false,
	})
}

func TestRouter_RegisterRoutes(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	body := `{"email":"a@b.com","password":"correct-horse","name":""}`
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestRouter_LoginRoutes(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	body := `{"email":"a@b.com","password":"correct-horse"}`
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			found = true
		}
	}
	if !found {
		t.Fatal("refresh_token cookie not set")
	}
}

func TestRouter_AuthRateLimited(t *testing.T) {
	h := buildTestRouter(t, denyAllLimiter{})
	body := `{"email":"a@b.com","password":"x"}`
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

func TestRouter_MeWithoutToken_401(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRouter_MeWithToken_200(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.User.ID != routerTestUserID.String() {
		t.Errorf("id = %q", resp.User.ID)
	}
}

func TestRouter_MeWithBadToken_401(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRouter_RefreshRequiresOrigin(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for disallowed origin", rec.Code)
	}
}

func TestRouter_RefreshAllowedOriginNoCookie_401(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	r.Header.Set("Origin", "https://storefront.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	// No cookie → handler returns 401, not 403
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRouter_CORSPreflight_AllowedOrigin(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	r := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	r.Header.Set("Origin", "https://storefront.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://storefront.example.com" {
		t.Errorf("ACAO = %q", got)
	}
}

func TestRouter_CORSPreflight_DisallowedOrigin_403(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	r := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRouter_NotFound(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nonexistent", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRouter_MethodNotAllowed(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/auth/login", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRouter_RequestIDPresent(t *testing.T) {
	h := buildTestRouter(t, allowAllLimiter{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/login",
		strings.NewReader(`{"email":"a@b.com","password":"x"}`)))

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID not set by top-level middleware")
	}
}

func TestInternalRouter_HealthzAndReadyz(t *testing.T) {
	ready := NewReady()
	h := NewInternalRouter(ready)

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d", path, rec.Code)
			}
		})
	}
}
