package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestHTTP_Checkout_ConcurrentIdempotency(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})

	// 1. Setup Admin
	adminEmail := uniqueEmail(t)
	adminXFF := uniqueXFF(t)
	adminD := newDoer(t, s)
	adminD.do("POST", "/api/v1/auth/register", fmt.Sprintf(`{"email":%q,"password":%q,"name":"Admin"}`, adminEmail, testPassword), "", "", adminXFF)
	_, _ = s.Pool.Exec(context.Background(), `UPDATE users SET role = 'admin' WHERE email = $1`, adminEmail)

	resp, loginBody := adminD.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, adminEmail, testPassword), "", "", adminXFF)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login: %d", resp.StatusCode)
	}
	var loginRes struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(loginBody, &loginRes)
	adminToken := loginRes.AccessToken

	// 2. Create Product & Stock
	sku := "CHK-" + uniqueXFF(t)
	createBody := fmt.Sprintf(`{"sku":%q,"name":"Checkout Item","price_minor":10000,"tax_class":"STANDARD_16"}`, sku)
	req := httptest.NewRequest("POST", s.URL+"/api/v1/admin/products", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Forwarded-For", adminXFF)
	rec := httptest.NewRecorder()
	s.Srv.Config.Handler.ServeHTTP(rec, req)

	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Give it 10 stock
	stockBody := `{"delta":10,"reason":"RESTOCK"}`
	req = httptest.NewRequest("POST", s.URL+"/api/v1/admin/products/"+created.ID+"/inventory", strings.NewReader(stockBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Forwarded-For", adminXFF)
	s.Srv.Config.Handler.ServeHTTP(httptest.NewRecorder(), req)

	// 3. Setup Customer
	custEmail := uniqueEmail(t)
	custXFF := uniqueXFF(t)
	custD := newDoer(t, s)
	custD.do("POST", "/api/v1/auth/register", fmt.Sprintf(`{"email":%q,"password":%q,"name":"Cust"}`, custEmail, testPassword), "", "", custXFF)

	resp, loginBody = custD.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, custEmail, testPassword), "", "", custXFF)
	var custLogin struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(loginBody, &custLogin)
	custToken := custLogin.AccessToken

	// Add an address
	addrBody := `{"recipient":"Cust","phone":"+254712345678","line1":"1 St","town":"Nairobi"}`
	resp, addrResBody := custD.do("POST", "/api/v1/me/addresses", addrBody, custToken, "", custXFF)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create address: %d", resp.StatusCode)
	}
	var addr struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(addrResBody, &addr)

	// Add to cart (Qty 2)
	cartBody := fmt.Sprintf(`{"product_id":%q,"quantity":2}`, created.ID)
	resp, _ = custD.do("POST", "/api/v1/cart/items", cartBody, custToken, "", custXFF)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("add to cart: %d", resp.StatusCode)
	}

	// 4. Fire Two Concurrent Checkout Requests
	checkoutBody := fmt.Sprintf(`{"address_id":%q}`, addr.ID)
	idemKey := "idem-" + uniqueXFF(t)

	runCheckout := func() (int, []byte) {
		req := httptest.NewRequest("POST", s.URL+"/api/v1/checkout", strings.NewReader(checkoutBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+custToken)
		req.Header.Set("Idempotency-Key", idemKey)
		req.Header.Set("X-Forwarded-For", custXFF)
		rec := httptest.NewRecorder()
		s.Srv.Config.Handler.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}

	var wg sync.WaitGroup
	var code1, code2 int
	var body1, body2 []byte
	start := make(chan struct{})

	wg.Add(2)
	go func() { defer wg.Done(); <-start; code1, body1 = runCheckout() }()
	go func() { defer wg.Done(); <-start; code2, body2 = runCheckout() }()
	close(start)
	wg.Wait()

	// 5. Assertions
	// Both should succeed with 201 (one actually created it, the other replayed the response)
	if code1 != http.StatusCreated || code2 != http.StatusCreated {
		t.Fatalf("expected both 201, got %d and %d", code1, code2)
	}

	var order1, order2 struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body1, &order1)
	_ = json.Unmarshal(body2, &order2)

	if order1.ID == "" || order1.ID != order2.ID {
		t.Fatalf("expected matching order IDs, got %q and %q", order1.ID, order2.ID)
	}

	// Verify DB state: exactly 1 order
	var orderCount int
	_ = s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM orders WHERE id = $1::uuid`, order1.ID).Scan(&orderCount)
	if orderCount != 1 {
		t.Errorf("expected 1 order in DB, found %d", orderCount)
	}

	// Verify Stock: Started at 10, bought 2. Should be 8.
	var available int
	_ = s.Pool.QueryRow(context.Background(), `SELECT available FROM inventory WHERE product_id = $1::uuid`, created.ID).Scan(&available)
	if available != 8 {
		t.Errorf("expected stock 8, got %d", available)
	}
}
