package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres/migrate"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func TestCheckoutRepo(t *testing.T) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("test"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	url, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	if err := migrate.Up(ctx, url); err != nil {
		t.Fatal(err)
	}

	pool, _ := pgxpool.New(ctx, url)
	defer pool.Close()

	repo := postgres.NewCheckoutRepo(pool, idgen.New())

	// Setup data
	userID := idgen.New().NewID()
	prodID := idgen.New().NewID()
	addrID := idgen.New().NewID()
	cartID := idgen.New().NewID()

	_, _ = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, 'c@d.com', 'h')`, [16]byte(userID))
	_, _ = pool.Exec(ctx, `INSERT INTO addresses (id, user_id, recipient, phone_e164, line1, town) VALUES ($1, $2, 'R', '+254712345678', 'L1', 'T')`, [16]byte(addrID), [16]byte(userID))
	_, _ = pool.Exec(ctx, `INSERT INTO products (id, sku, name, price_minor, currency, tax_class) VALUES ($1, 'SKU1', 'P1', 1000, 'KES', 'STANDARD_16')`, [16]byte(prodID))
	_, _ = pool.Exec(ctx, `INSERT INTO inventory (product_id, available) VALUES ($1, 10)`, [16]byte(prodID)) // Stock = 10
	_, _ = pool.Exec(ctx, `INSERT INTO carts (id, user_id, status) VALUES ($1, $2, 'ACTIVE')`, [16]byte(cartID), [16]byte(userID))

	now := time.Now().UTC()
	price, _ := domain.New(1000, domain.KES)
	vat, _ := domain.New(100, domain.KES)

	// 1. Successful Checkout (qty 2)
	item := domain.OrderItemSnapshot{
		ProductID: prodID, SKU: "SKU1", ProductName: "P1", Quantity: 2,
		UnitPrice: price, TaxClass: domain.TaxStandard16, TaxRateBp: 1600, LineVat: vat, LineGrossMinor: 2000,
	}
	order, _ := domain.NewOrder(idgen.New().NewID(), userID, addrID, []domain.OrderItemSnapshot{item}, price, now, now.Add(15*time.Minute))

	if err := repo.Execute(ctx, cartID, order, "idem-key-1"); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify DB state
	var available int
	_ = pool.QueryRow(ctx, `SELECT available FROM inventory WHERE product_id = $1`, [16]byte(prodID)).Scan(&available)
	if available != 8 {
		t.Fatalf("expected stock 8, got %d", available)
	}

	var cartStatus string
	_ = pool.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, [16]byte(cartID)).Scan(&cartStatus)
	if cartStatus != "CONVERTED" {
		t.Fatalf("expected CONVERTED, got %s", cartStatus)
	}

	var orderCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE id = $1`, [16]byte(order.ID())).Scan(&orderCount)
	if orderCount != 1 {
		t.Fatalf("order not created")
	}

	var outboxCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, [16]byte(order.ID())).Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("outbox event not created")
	}

	// 2. Failed Checkout (qty 20, but only 8 left)
	failCartID := idgen.New().NewID()
	_, _ = pool.Exec(ctx, `INSERT INTO carts (id, user_id, status) VALUES ($1, $2, 'ACTIVE')`, [16]byte(failCartID), [16]byte(userID))

	badItem := item
	badItem.Quantity = 20
	badOrder, _ := domain.NewOrder(idgen.New().NewID(), userID, addrID, []domain.OrderItemSnapshot{badItem}, price, now, now.Add(15*time.Minute))

	err = repo.Execute(ctx, failCartID, badOrder, "idem-key-2")
	if err == nil {
		t.Fatalf("expected error on insufficient stock, got nil")
	}

	// Verify rollback
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE id = $1`, [16]byte(badOrder.ID())).Scan(&orderCount)
	if orderCount != 0 {
		t.Fatalf("order was created despite rollback")
	}
	_ = pool.QueryRow(ctx, `SELECT available FROM inventory WHERE product_id = $1`, [16]byte(prodID)).Scan(&available)
	if available != 8 {
		t.Fatalf("stock was modified despite rollback, got %d", available)
	}
}
