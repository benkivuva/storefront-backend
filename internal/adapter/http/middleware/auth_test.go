package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type fakeVerifier struct {
	tokens map[string]Token
	err    error
}

func (f *fakeVerifier) Verify(raw string) (Token, error) {
	if f.err != nil {
		return Token{}, f.err
	}
	t, ok := f.tokens[raw]
	if !ok {
		return Token{}, errors.New("invalid token")
	}
	return t, nil
}

var (
	testUserID = domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	testCustomer = Token{UserID: testUserID, Role: domain.RoleCustomer}
)

func TestAuth_MissingHeader_401(t *testing.T) {
	v := &fakeVerifier{tokens: map[string]Token{}}
	h := Auth(v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
	assertProblemCode(t, rec.Body.Bytes(), "UNAUTHORIZED")
}

func TestAuth_MalformedHeader_401(t *testing.T) {
	v := &fakeVerifier{tokens: map[string]Token{"ok": testCustomer}}
	cases := map[string]string{
		"no scheme":       "sometoken",
		"wrong scheme":    "Basic dXNlcjpwYXNz",
		"empty token":     "Bearer ",
		"bearer no space": "BearerNoSpace",
		"bearer trailing": "Bearer   ",
	}
	for name, hdr := range cases {
		t.Run(name, func(t *testing.T) {
			h := Auth(v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("handler called")
			}))
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Authorization", hdr)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", rec.Code)
			}
		})
	}
}

func TestAuth_InvalidToken_401(t *testing.T) {
	v := &fakeVerifier{tokens: map[string]Token{"good": testCustomer}}
	h := Auth(v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called")
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAuth_ValidToken_PopulatesContext(t *testing.T) {
	v := &fakeVerifier{tokens: map[string]Token{"good": testCustomer}}
	var captured Token
	var capturedOK bool
	h := Auth(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, capturedOK = UserFrom(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !capturedOK {
		t.Fatal("UserFrom returned !ok")
	}
	if captured.UserID != testCustomer.UserID || captured.Role != testCustomer.Role {
		t.Fatalf("captured = %+v", captured)
	}
}

func TestAuth_LowercaseBearerAccepted(t *testing.T) {
	v := &fakeVerifier{tokens: map[string]Token{"good": testCustomer}}
	h := Auth(v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "bearer good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (RFC 7235 scheme case-insensitive)", rec.Code)
	}
}

func TestUserFrom_EmptyContext(t *testing.T) {
	if _, ok := UserFrom(httptest.NewRequest("GET", "/", nil).Context()); ok {
		t.Fatal("UserFrom returned ok on empty context")
	}
}

func assertProblemCode(t *testing.T, body []byte, wantCode string) {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal problem: %v (body: %s)", err, body)
	}
	if p["code"] != wantCode {
		t.Errorf("code = %v, want %q", p["code"], wantCode)
	}
}
