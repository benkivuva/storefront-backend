package apitest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func TestHTTP_FlashSale_Oversell(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, serverOpts{})
	ctx := context.Background()

	// 1. Setup Product & Stock directly in DB
	prodID := newID(t)
	sku := "FLASH-" + uniqueXFF(t)
	_, err := s.Pool.Exec(ctx, `INSERT INTO products (id, sku, name, price_minor, currency, tax_class, is_active) VALUES ($1, $2, 'Flash Item', 10000, 'KES', 'STANDARD_16', true)`, [16]byte(prodID), sku)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO inventory (product_id, available) VALUES ($1, 1)`, [16]byte(prodID)) // Stock = 1
	if err != nil {
		t.Fatal(err)
	}

	// 2. Setup 50 Customers, Addresses, and Carts directly in DB
	const N = 50
	userIDs := make([]domain.ID, N)
	addrIDs := make([]domain.ID, N)
	cartIDs := make([]domain.ID, N)
	tokens := make([]string, N)

	for i := 0; i < N; i++ {
		userIDs[i] = newID(t)
		addrIDs[i] = newID(t)
		cartIDs[i] = newID(t)
		email := fmt.Sprintf("user%d-%s@test.local", i, uniqueXFF(t))

		// Insert User
		_, err = s.Pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, 'h', 'customer')`, [16]byte(userIDs[i]), email)
		if err != nil {
			t.Fatal(err)
		}

		// Insert Address
		_, err = s.Pool.Exec(ctx, `INSERT INTO addresses (id, user_id, recipient, phone_e164, line1, town) VALUES ($1, $2, 'R', '+254712345678', 'L1', 'T')`, [16]byte(addrIDs[i]), [16]byte(userIDs[i]))
		if err != nil {
			t.Fatal(err)
		}

		// Insert Cart & Item
		_, err = s.Pool.Exec(ctx, `INSERT INTO carts (id, user_id, status) VALUES ($1, $2, 'ACTIVE')`, [16]byte(cartIDs[i]), [16]byte(userIDs[i]))
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Pool.Exec(ctx, `INSERT INTO cart_items (id, cart_id, product_id, quantity) VALUES ($1, $2, $3, 1)`, [16]byte(newID(t)), [16]byte(cartIDs[i]), [16]byte(prodID))
		if err != nil {
			t.Fatal(err)
		}

		// Generate JWT directly
		tokens[i], err = sharedKS.Sign(userIDs[i].String(), string(domain.RoleCustomer), 15*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
	}

	// 3. Fire 50 Concurrent Checkouts
	var wg sync.WaitGroup
	results := make([]int, N)
	start := make(chan struct{})

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			checkoutBody := fmt.Sprintf(`{"address_id":%q,"expected_total_minor":60000}`, addrIDs[idx].String())
			req := httptest.NewRequest("POST", s.URL+"/api/v1/checkout", strings.NewReader(checkoutBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tokens[idx])
			req.Header.Set("Idempotency-Key", "idem-flash-"+uniqueXFF(t))
			req.Header.Set("X-Forwarded-For", uniqueXFF(t))
			rec := httptest.NewRecorder()
			<-start
			s.Srv.Config.Handler.ServeHTTP(rec, req)
			results[idx] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()

	// 4. Assert exactly 1 success and 49 conflicts
	success, conflict := 0, 0
	for _, code := range results {
		switch code {
		case http.StatusCreated:
			success++
		case http.StatusConflict:
			conflict++
		default:
			t.Errorf("unexpected status code: %d", code)
		}
	}

	if success != 1 || conflict != N-1 {
		t.Fatalf("Expected exactly 1 success and %d conflicts, got %d success and %d conflicts", N-1, success, conflict)
	}

	// 5. Verify stock is exactly 0
	var available int
	_ = s.Pool.QueryRow(ctx, `SELECT available FROM inventory WHERE product_id = $1::uuid`, prodID.String()).Scan(&available)
	if available != 0 {
		t.Fatalf("Oversell! Expected 0 stock, got %d", available)
	}

	// 6. Verify exactly 1 order created for this specific SKU
	var orderCount int
	_ = s.Pool.QueryRow(ctx, `
SELECT count(*) FROM orders o 
JOIN order_items oi ON o.id = oi.order_id 
WHERE oi.sku = $1`, sku).Scan(&orderCount)
	if orderCount != 1 {
		t.Fatalf("Expected exactly 1 order for SKU %s, got %d", sku, orderCount)
	}
}

func newID(t *testing.T) domain.ID {
	t.Helper()
	return idgen.New().NewID()
}
