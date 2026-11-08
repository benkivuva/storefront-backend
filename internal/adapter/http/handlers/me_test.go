package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/middleware"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

type fakeMeService struct {
	getMeUser domain.User
	getMeErr  error

	updateMeUser  domain.User
	updateMeErr   error
	updateMePatch service.UpdateMePatch

	listAddressesResult []domain.Address
	listAddressesErr    error

	createAddressResult domain.Address
	createAddressErr    error
	createAddressIn     service.CreateAddressInput

	listSessionsResult []service.SessionInfo
	listSessionsErr    error

	revokeSessionErr error
	revokeSessionID  domain.ID
}

func (f *fakeMeService) GetMe(_ context.Context, _ domain.ID) (domain.User, error) {
	return f.getMeUser, f.getMeErr
}

func (f *fakeMeService) UpdateMe(_ context.Context, _ domain.ID, patch service.UpdateMePatch) (domain.User, error) {
	f.updateMePatch = patch
	return f.updateMeUser, f.updateMeErr
}

func (f *fakeMeService) ListAddresses(_ context.Context, _ domain.ID) ([]domain.Address, error) {
	return f.listAddressesResult, f.listAddressesErr
}

func (f *fakeMeService) CreateAddress(_ context.Context, _ domain.ID, in service.CreateAddressInput) (domain.Address, error) {
	f.createAddressIn = in
	return f.createAddressResult, f.createAddressErr
}

func (f *fakeMeService) ListSessions(_ context.Context, _ domain.ID) ([]service.SessionInfo, error) {
	return f.listSessionsResult, f.listSessionsErr
}

func (f *fakeMeService) RevokeSession(_ context.Context, _, tokenID domain.ID) error {
	f.revokeSessionID = tokenID
	return f.revokeSessionErr
}

func authedRequest(method, target string, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	return r.WithContext(middleware.WithUser(r.Context(),
		middleware.Token{UserID: testUserID, Role: domain.RoleCustomer}))
}

func withPathParam(r *http.Request, name, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(name, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func testAddress(t *testing.T) domain.Address {
	t.Helper()
	a, err := domain.NewAddress(
		testUserID, testUserID,
		"Jane", "+254712345678",
		"12 Kimathi", "", "Nairobi", "Nairobi", "",
		time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	return a
}

func TestMeHandlers_NoUser_401(t *testing.T) {
	h := NewMeHandler(&fakeMeService{})
	cases := map[string]http.HandlerFunc{
		"GetMe":         h.GetMe,
		"UpdateMe":      h.UpdateMe,
		"ListAddresses": h.ListAddresses,
		"CreateAddress": h.CreateAddress,
		"ListSessions":  h.ListSessions,
		"RevokeSession": h.RevokeSession,
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			fn(rec, httptest.NewRequest("GET", "/", nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestGetMe_Success(t *testing.T) {
	u := testUser(t)
	svc := &fakeMeService{getMeUser: u}
	h := NewMeHandler(svc)

	rec := httptest.NewRecorder()
	h.GetMe(rec, authedRequest("GET", "/api/v1/me", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp struct {
		User struct {
			ID              string `json:"id"`
			Email           string `json:"email"`
			Role            string `json:"role"`
			IsEmailVerified bool   `json:"is_email_verified"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.User.ID != testUserID.String() {
		t.Errorf("id = %q", resp.User.ID)
	}
	if resp.User.Email != "user@example.com" {
		t.Errorf("email = %q", resp.User.Email)
	}
}

func TestUpdateMe_NameOnly(t *testing.T) {
	updated := testUser(t)
	svc := &fakeMeService{updateMeUser: updated}
	h := NewMeHandler(svc)

	rec := httptest.NewRecorder()
	h.UpdateMe(rec, authedRequest("PATCH", "/api/v1/me", `{"name":"Alice"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.updateMePatch.Name == nil || *svc.updateMePatch.Name != "Alice" {
		t.Errorf("Name patch = %v", svc.updateMePatch.Name)
	}
	if svc.updateMePatch.Phone != nil {
		t.Errorf("Phone should be nil, got %v", *svc.updateMePatch.Phone)
	}
}

func TestUpdateMe_EmptyPatch_400(t *testing.T) {
	svc := &fakeMeService{updateMeErr: service.ErrEmptyPatch}
	h := NewMeHandler(svc)

	rec := httptest.NewRecorder()
	h.UpdateMe(rec, authedRequest("PATCH", "/api/v1/me", `{}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestListAddresses_Success(t *testing.T) {
	svc := &fakeMeService{listAddressesResult: []domain.Address{testAddress(t)}}
	h := NewMeHandler(svc)

	rec := httptest.NewRecorder()
	h.ListAddresses(rec, authedRequest("GET", "/api/v1/me/addresses", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Addresses []struct {
			ID        string `json:"id"`
			Recipient string `json:"recipient"`
		} `json:"addresses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Addresses) != 1 || resp.Addresses[0].Recipient != "Jane" {
		t.Fatalf("addresses = %+v", resp.Addresses)
	}
}

func TestCreateAddress_Success(t *testing.T) {
	svc := &fakeMeService{createAddressResult: testAddress(t)}
	h := NewMeHandler(svc)

	body := `{"recipient":"Jane","phone":"+254712345678","line1":"12 Kimathi","town":"Nairobi"}`
	rec := httptest.NewRecorder()
	h.CreateAddress(rec, authedRequest("POST", "/api/v1/me/addresses", body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createAddressIn.Recipient != "Jane" {
		t.Errorf("recipient = %q", svc.createAddressIn.Recipient)
	}
}

func TestCreateAddress_ValidationError_400(t *testing.T) {
	svc := &fakeMeService{createAddressErr: domain.ErrInvalidPhone}
	h := NewMeHandler(svc)

	body := `{"recipient":"Jane","phone":"bad","line1":"x","town":"Nairobi"}`
	rec := httptest.NewRecorder()
	h.CreateAddress(rec, authedRequest("POST", "/api/v1/me/addresses", body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "INVALID_PHONE")
}

func TestListSessions_Success(t *testing.T) {
	now := time.Now().UTC()
	svc := &fakeMeService{
		listSessionsResult: []service.SessionInfo{
			{ID: testUserID, UserAgent: "curl", IP: "1.2.3.4",
				CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		},
	}
	h := NewMeHandler(svc)

	rec := httptest.NewRecorder()
	h.ListSessions(rec, authedRequest("GET", "/api/v1/me/sessions", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Sessions []struct {
			ID        string `json:"id"`
			UserAgent string `json:"user_agent"`
			IP        string `json:"ip"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Sessions) != 1 || resp.Sessions[0].IP != "1.2.3.4" {
		t.Fatalf("sessions = %+v", resp.Sessions)
	}
}

func TestRevokeSession_Success_204(t *testing.T) {
	svc := &fakeMeService{}
	h := NewMeHandler(svc)

	r := authedRequest("DELETE", "/api/v1/me/sessions/"+testUserID.String(), "")
	r = withPathParam(r, "id", testUserID.String())
	rec := httptest.NewRecorder()
	h.RevokeSession(rec, r)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.revokeSessionID != testUserID {
		t.Errorf("revokeSessionID = %v", svc.revokeSessionID)
	}
}

func TestRevokeSession_InvalidID_400(t *testing.T) {
	h := NewMeHandler(&fakeMeService{})
	r := authedRequest("DELETE", "/api/v1/me/sessions/not-a-uuid", "")
	r = withPathParam(r, "id", "not-a-uuid")
	rec := httptest.NewRecorder()
	h.RevokeSession(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertProblemCode(t, rec.Body.Bytes(), "INVALID_ID")
}

func TestRevokeSession_NotFound_404(t *testing.T) {
	svc := &fakeMeService{revokeSessionErr: domain.ErrNotFound}
	h := NewMeHandler(svc)

	r := authedRequest("DELETE", "/api/v1/me/sessions/"+testUserID.String(), "")
	r = withPathParam(r, "id", testUserID.String())
	rec := httptest.NewRecorder()
	h.RevokeSession(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
