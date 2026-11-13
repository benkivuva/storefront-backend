package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTP_Catalog_E2E(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})

	// 1. Create an Admin user directly (bypasses public register endpoint)
	adminEmail := uniqueEmail(t)
	adminXFF := uniqueXFF(t)
	adminD := newDoer(t, s)

	reg := fmt.Sprintf(`{"email":%q,"password":%q,"name":"Admin"}`, adminEmail, testPassword)
	adminD.do("POST", "/api/v1/auth/register", reg, "", "", adminXFF)

	// Promote to admin in DB
	_, err := s.Pool.Exec(context.Background(), `UPDATE users SET role = 'admin' WHERE email = $1`, adminEmail)
	if err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	// Login as admin
	resp, loginBody := adminD.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, adminEmail, testPassword), "", "", adminXFF)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login: %d", resp.StatusCode)
	}
	var loginRes struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(loginBody, &loginRes); err != nil {
		t.Fatal(err)
	}

	// 2. Admin creates a product
	createBody := `{"sku":"E2E-001","name":"E2E Product","description":"Testing","price_minor":150000,"tax_class":"STANDARD_16"}`
	req := httptest.NewRequest("POST", s.URL+"/api/v1/admin/products", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+loginRes.AccessToken)
	req.Header.Set("X-Forwarded-For", uniqueXFF(t))
	rec := httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create product: %d body=%s", rec.Code, rec.Body)
	}
	var created struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// 3. Public user fetches it (Cache Miss -> DB -> Cache Write)
	publicD := newDoer(t, s)
	resp, body := publicD.do("GET", "/api/v1/products/"+created.ID, "", "", "", uniqueXFF(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get product: %d", resp.StatusCode)
	}

	var fetched struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &fetched); err != nil {
		t.Fatalf("unmarshal body: %v (body: %s)", err, body)
	}
	if fetched.Name != "E2E Product" {
		t.Errorf("name = %q", fetched.Name)
	}

	// 4. Admin updates with WRONG If-Match
	updateBody := `{"name":"Updated Name","price_minor":200000,"tax_class":"STANDARD_16"}`
	req = httptest.NewRequest("PUT", s.URL+"/api/v1/admin/products/"+created.ID, strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+loginRes.AccessToken)
	req.Header.Set("If-Match", "999") // Wrong version
	req.Header.Set("X-Forwarded-For", uniqueXFF(t))
	rec = httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("wrong If-Match: got %d, want 409", rec.Code)
	}

	// 5. Admin updates with CORRECT If-Match
	req = httptest.NewRequest("PUT", s.URL+"/api/v1/admin/products/"+created.ID, strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+loginRes.AccessToken)
	req.Header.Set("If-Match", fmt.Sprintf("%d", created.Version))
	req.Header.Set("X-Forwarded-For", uniqueXFF(t))
	rec = httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update product: %d body=%s", rec.Code, rec.Body)
	}

	// 6. Public user fetches again (Cache Invalidated -> DB -> Returns new data)
	resp, body = publicD.do("GET", "/api/v1/products/"+created.ID, "", "", "", uniqueXFF(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get after update: %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &fetched); err != nil {
		t.Fatalf("unmarshal body: %v (body: %s)", err, body)
	}
	if fetched.Name != "Updated Name" {
		t.Errorf("after update, name = %q, want Updated Name", fetched.Name)
	}
}
