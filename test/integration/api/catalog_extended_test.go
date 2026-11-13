package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTP_Catalog_Extended(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})

	// 1. Setup Admin
	adminEmail := uniqueEmail(t)
	adminXFF := uniqueXFF(t)
	adminD := newDoer(t, s)

	reg := fmt.Sprintf(`{"email":%q,"password":%q,"name":"Admin"}`, adminEmail, testPassword)
	adminD.do("POST", "/api/v1/auth/register", reg, "", "", adminXFF)
	_, _ = s.Pool.Exec(context.Background(), `UPDATE users SET role = 'admin' WHERE email = $1`, adminEmail)

	resp, loginBody := adminD.do("POST", "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, adminEmail, testPassword), "", "", adminXFF)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login: %d", resp.StatusCode)
	}
	var loginRes struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(loginBody, &loginRes)
	adminToken := loginRes.AccessToken

	// 2. Create a standard product and verify Outbox + Audit
	sku := "SEARCH-" + uniqueXFF(t) // Use XFF to ensure unique SKU
	createBody := fmt.Sprintf(`{"sku":%q,"name":"Blue Widget","description":"A nice widget","price_minor":15000,"tax_class":"STANDARD_16"}`, sku)

	req := httptest.NewRequest("POST", s.URL+"/api/v1/admin/products", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Forwarded-For", adminXFF)
	rec := httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create product: %d body=%s", rec.Code, rec.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Verify Outbox
	var outboxCount int
	err := s.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_events WHERE aggregate_id = $1::uuid AND event_type = 'ProductUpserted'`, created.ID).Scan(&outboxCount)
	if err != nil || outboxCount != 1 {
		t.Errorf("expected 1 outbox event, got %d (err: %v)", outboxCount, err)
	}

	// Verify Audit Log
	var auditCount int
	err = s.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity_id = $1 AND action = 'product_created'`, created.ID).Scan(&auditCount)
	if err != nil || auditCount != 1 {
		t.Errorf("expected 1 audit log, got %d (err: %v)", auditCount, err)
	}

	// 3. Test Search Filters
	publicD := newDoer(t, s)

	// Search by Query (q=Blue)
	resp, body := publicD.do("GET", "/api/v1/products?q=Blue", "", "", "", uniqueXFF(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: %d", resp.StatusCode)
	}
	var searchRes struct {
		Products []struct {
			SKU string `json:"sku"`
		} `json:"products"`
	}
	_ = json.Unmarshal(body, &searchRes)
	if len(searchRes.Products) != 1 || searchRes.Products[0].SKU != sku {
		t.Errorf("search q=Blue failed, got %d results", len(searchRes.Products))
	}

	// Search by MinPrice (min_price=20000) - should NOT find the 15000 product
	_, body = publicD.do("GET", "/api/v1/products?min_price=20000", "", "", "", uniqueXFF(t))
	_ = json.Unmarshal(body, &searchRes)
	found := false
	for _, p := range searchRes.Products {
		if p.SKU == sku {
			found = true
		}
	}
	if found {
		t.Error("min_price filter failed, found cheap product")
	}

	// 4. Test Low Stock
	lowSku := "LOW-" + uniqueXFF(t)
	lowBody := fmt.Sprintf(`{"sku":%q,"name":"Rare Item","price_minor":5000,"tax_class":"STANDARD_16"}`, lowSku)
	req = httptest.NewRequest("POST", s.URL+"/api/v1/admin/products", strings.NewReader(lowBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Forwarded-For", adminXFF)
	rec = httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)

	var lowCreated struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &lowCreated)

	// Set stock to 2 (below default threshold of 10)
	stockBody := `{"delta":2,"reason":"RESTOCK"}`
	req = httptest.NewRequest("POST", s.URL+"/api/v1/admin/products/"+lowCreated.ID+"/inventory", strings.NewReader(stockBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Forwarded-For", adminXFF)
	rec = httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("adjust stock failed: status %d body %s", rec.Code, rec.Body)
	}

	// Check Low Stock Endpoint
	req = httptest.NewRequest("GET", s.URL+"/api/v1/admin/products/low-stock", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Forwarded-For", adminXFF)
	rec = httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("low stock endpoint: %d", rec.Code)
	}
	var lowRes []struct {
		ProductID string `json:"product_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &lowRes)

	lowFound := false
	for _, item := range lowRes {
		if item.ProductID == lowCreated.ID {
			lowFound = true
			break
		}
	}
	if !lowFound {
		t.Errorf("low stock endpoint failed to find product %s in %v", lowCreated.ID, lowRes)
	}

	// 5. Test Soft Delete
	req = httptest.NewRequest("DELETE", s.URL+"/api/v1/admin/products/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Forwarded-For", adminXFF)
	rec = httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}

	// Verify public API returns 404
	resp, _ = publicD.do("GET", "/api/v1/products/"+created.ID, "", "", "", uniqueXFF(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}

	// Verify DB still has it (soft deleted)
	var deletedAt *time.Time
	err = s.Pool.QueryRow(context.Background(), `SELECT deleted_at FROM products WHERE id = $1::uuid`, created.ID).Scan(&deletedAt)
	if err != nil || deletedAt == nil {
		t.Errorf("product not found in DB or not soft-deleted")
	}

	// Verify Outbox recorded the Deactivated event
	err = s.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_events WHERE aggregate_id = $1::uuid AND event_type = 'ProductDeactivated'`, created.ID).Scan(&outboxCount)
	if err != nil || outboxCount != 1 {
		t.Errorf("expected 1 deactivated outbox event, got %d", outboxCount)
	}
}
