package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

var (
	testUserID = domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	testRefreshRaw = []byte("raw-refresh-token-bytes")
)

func testUser(t *testing.T) domain.User {
	t.Helper()
	u, err := domain.NewUser(
		testUserID, "user@example.com", "hash", "Alice",
		domain.RoleCustomer, "+254712345678",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	return u
}

type fakeAuthService struct {
	registerUser domain.User
	registerErr  error
	registerArgs []string

	loginResult service.LoginResult
	loginErr    error
	loginArgs   []string

	refreshResult service.LoginResult
	refreshErr    error
	refreshRaw    []byte

	logoutRaw []byte
	logoutErr error
}

func (f *fakeAuthService) Register(_ context.Context, email, password, name, ip string) (domain.User, error) {
	f.registerArgs = []string{email, password, name, ip}
	return f.registerUser, f.registerErr
}

func (f *fakeAuthService) Login(_ context.Context, email, password, ip, ua string) (service.LoginResult, error) {
	f.loginArgs = []string{email, password, ip, ua}
	return f.loginResult, f.loginErr
}

func (f *fakeAuthService) Refresh(_ context.Context, raw []byte, ip, ua string) (service.LoginResult, error) {
	f.refreshRaw = raw
	return f.refreshResult, f.refreshErr
}

func (f *fakeAuthService) Logout(_ context.Context, raw []byte) error {
	f.logoutRaw = raw
	return f.logoutErr
}

func newAuthHandler(t *testing.T, svc AuthService) *AuthHandler {
	t.Helper()
	return NewAuthHandler(svc, nil, time.Hour, false)
}

func TestRegister_Success(t *testing.T) {
	svc := &fakeAuthService{registerUser: testUser(t)}
	h := newAuthHandler(t, svc)

	body := `{"email":"user@example.com","password":"correct-horse","name":"Alice"}`
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	r.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	h.Register(rec, r)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.registerArgs[0] != "user@example.com" {
		t.Errorf("email passed = %q", svc.registerArgs[0])
	}
	if svc.registerArgs[3] != "1.2.3.4" {
		t.Errorf("ip passed = %q", svc.registerArgs[3])
	}
	var resp dtoRegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.User.ID != testUserID.String() {
		t.Errorf("id = %q", resp.User.ID)
	}
}

type dtoRegisterResponse struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func TestRegister_InvalidJSON_400(t *testing.T) {
	h := newAuthHandler(t, &fakeAuthService{})
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	h.Register(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRegister_UnknownField_400(t *testing.T) {
	h := newAuthHandler(t, &fakeAuthService{})
	body := `{"email":"a@b.com","password":"x","name":"","admin":true}`
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Register(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRegister_ServiceError_400(t *testing.T) {
	svc := &fakeAuthService{registerErr: domain.ErrInvalidEmail}
	h := newAuthHandler(t, svc)
	body := `{"email":"bad","password":"x","name":""}`
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Register(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "INVALID_EMAIL")
}

func TestRegister_EmailTaken_409(t *testing.T) {
	svc := &fakeAuthService{registerErr: service.ErrEmailTaken}
	h := newAuthHandler(t, svc)
	body := `{"email":"a@b.com","password":"x","name":""}`
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Register(rec, r)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "EMAIL_TAKEN")
}

func TestLogin_Success(t *testing.T) {
	svc := &fakeAuthService{
		loginResult: service.LoginResult{
			AccessToken:  "access-abc",
			RefreshToken: testRefreshRaw,
			User:         testUser(t),
		},
	}
	h := newAuthHandler(t, svc)

	body := `{"email":"user@example.com","password":"correct-horse"}`
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	r.RemoteAddr = "1.2.3.4:5678"
	r.Header.Set("User-Agent", "test-agent")
	rec := httptest.NewRecorder()
	h.Login(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.loginArgs[3] != "test-agent" {
		t.Errorf("ua passed = %q", svc.loginArgs[3])
	}

	cookies := rec.Result().Cookies()
	var refresh *http.Cookie
	for _, c := range cookies {
		if c.Name == refreshCookieName {
			refresh = c
		}
	}
	if refresh == nil {
		t.Fatal("refresh cookie not set")
	}
	if !refresh.HttpOnly {
		t.Error("cookie not HttpOnly")
	}
	if refresh.SameSite != http.SameSiteStrictMode {
		t.Error("cookie not SameSite=Strict")
	}
	if refresh.Path != refreshCookiePath {
		t.Errorf("cookie path = %q", refresh.Path)
	}
	decoded, _ := base64.RawURLEncoding.DecodeString(refresh.Value)
	if string(decoded) != string(testRefreshRaw) {
		t.Errorf("cookie value did not round-trip")
	}

	var resp struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.AccessToken != "access-abc" {
		t.Errorf("access_token = %q", resp.AccessToken)
	}
}

func TestLogin_InvalidCredentials_401(t *testing.T) {
	svc := &fakeAuthService{loginErr: service.ErrInvalidCredentials}
	h := newAuthHandler(t, svc)
	body := `{"email":"a@b.com","password":"x"}`
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Login(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "INVALID_CREDENTIALS")
}

func TestRefresh_Success(t *testing.T) {
	svc := &fakeAuthService{
		refreshResult: service.LoginResult{
			AccessToken:  "new-access",
			RefreshToken: []byte("new-raw"),
			User:         testUser(t),
		},
	}
	h := newAuthHandler(t, svc)

	r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	r.AddCookie(&http.Cookie{
		Name:  refreshCookieName,
		Value: base64.RawURLEncoding.EncodeToString(testRefreshRaw),
	})
	rec := httptest.NewRecorder()
	h.Refresh(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if string(svc.refreshRaw) != string(testRefreshRaw) {
		t.Errorf("raw passed to service = %q", svc.refreshRaw)
	}

	// New cookie issued
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != refreshCookieName {
		t.Fatalf("cookies = %v", cookies)
	}
	decoded, _ := base64.RawURLEncoding.DecodeString(cookies[0].Value)
	if string(decoded) != "new-raw" {
		t.Errorf("new cookie = %q", decoded)
	}
}

func TestRefresh_MissingCookie_401(t *testing.T) {
	h := newAuthHandler(t, &fakeAuthService{})
	r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	rec := httptest.NewRecorder()
	h.Refresh(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRefresh_ServiceError_ClearsCookie(t *testing.T) {
	svc := &fakeAuthService{refreshErr: service.ErrTokenExpired}
	h := newAuthHandler(t, svc)

	r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	r.AddCookie(&http.Cookie{
		Name:  refreshCookieName,
		Value: base64.RawURLEncoding.EncodeToString(testRefreshRaw),
	})
	rec := httptest.NewRecorder()
	h.Refresh(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].MaxAge != -1 {
		t.Fatalf("expected clearing cookie; got %v", cookies)
	}
}

func TestLogout_Success(t *testing.T) {
	svc := &fakeAuthService{}
	h := newAuthHandler(t, svc)

	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{
		Name:  refreshCookieName,
		Value: base64.RawURLEncoding.EncodeToString(testRefreshRaw),
	})
	rec := httptest.NewRecorder()
	h.Logout(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if string(svc.logoutRaw) != string(testRefreshRaw) {
		t.Errorf("logout raw = %q", svc.logoutRaw)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].MaxAge != -1 {
		t.Fatalf("cookie not cleared: %v", cookies)
	}
}

func TestLogout_NoCookie_StillClearsAndReturns204(t *testing.T) {
	svc := &fakeAuthService{}
	h := newAuthHandler(t, svc)

	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()
	h.Logout(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if svc.logoutRaw != nil {
		t.Errorf("service called with nil cookie")
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
