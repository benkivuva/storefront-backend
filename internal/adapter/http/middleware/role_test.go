package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

func withUser(r *http.Request, tok Token) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userCtxKey{}, tok))
}

func TestRequireRole_NoUserInContext_401(t *testing.T) {
	h := RequireRole()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRequireRole_NoRoles_AnyAuthenticatedUserPasses(t *testing.T) {
	h := RequireRole()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := withUser(httptest.NewRequest("GET", "/", nil),
		Token{UserID: testUserID, Role: domain.RoleCustomer})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRequireRole_RoleMismatch_403(t *testing.T) {
	h := RequireRole(domain.RoleAdmin)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	r := withUser(httptest.NewRequest("GET", "/", nil),
		Token{UserID: testUserID, Role: domain.RoleCustomer})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "FORBIDDEN")
}

func TestRequireRole_RoleMatch_Passes(t *testing.T) {
	h := RequireRole(domain.RoleAdmin)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := withUser(httptest.NewRequest("GET", "/", nil),
		Token{UserID: testUserID, Role: domain.RoleAdmin})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRequireRole_MultipleAllowed(t *testing.T) {
	h := RequireRole(domain.RoleCustomer, domain.RoleAdmin)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, role := range []domain.Role{domain.RoleCustomer, domain.RoleAdmin} {
		r := withUser(httptest.NewRequest("GET", "/", nil),
			Token{UserID: testUserID, Role: role})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("role %q: status = %d", role, rec.Code)
		}
	}
}
