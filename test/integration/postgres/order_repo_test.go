package postgres_test

import (
	"context"
	"errors"
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

func TestOrderRepo(t *testing.T) {
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

	repo := postgres.NewOrderRepo(pool, idgen.New())

	userID := idgen.New().NewID()
	prodID := idgen.New().NewID()
	addrID := idgen.New().NewID()

	_, _ = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, 'a@b.com', 'h')`, [16]byte(userID))
	_, _ = pool.Exec(ctx, `INSERT INTO products (id, sku, name, price_minor, currency, tax_class) VALUES ($1, 'SKU1', 'P1', 1000, 'KES', 'STANDARD_16')`, [16]byte(prodID))
	_, _ = pool.Exec(ctx, `INSERT INTO addresses (id, user_id, recipient, phone_e164, line1, town) VALUES ($1, $2, 'R', '+254712345678', 'L1', 'T')`, [16]byte(addrID), [16]byte(userID))

	now := time.Now().UTC()
	price, _ := domain.New(1000, domain.KES)
	vat, _ := domain.New(100, domain.KES)

	item := domain.OrderItemSnapshot{
		ProductID: prodID, SKU: "SKU1", ProductName: "P1", Quantity: 1,
		UnitPrice: price, TaxClass: domain.TaxStandard16, TaxRateBp: 1600, LineVat: vat, LineGrossMinor: 1000,
	}

	o, _ := domain.NewOrder(
		idgen.New().NewID(), userID, addrID,
		[]domain.OrderItemSnapshot{item}, price, now, now.Add(15*time.Minute),
	)

	// 1. Create
	idempotencyKey := "test-idem-key-123"
	if err := repo.Create(ctx, o, idempotencyKey); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 2. GetByID
	fetched, err := repo.GetByID(ctx, o.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.ID() != o.ID() || fetched.TotalMinor() != o.TotalMinor() {
		t.Fatalf("mismatch on fetch")
	}
	if len(fetched.Items()) != 1 || fetched.Items()[0].ProductName != "P1" {
		t.Fatalf("item mismatch")
	}

	// 3. Update Status
	if err := fetched.Transition(domain.StatusPaid, "system", "paid", now.Add(1*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, fetched); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	// Verify status change persisted
	fetched2, _ := repo.GetByID(ctx, o.ID())
	if fetched2.Status() != domain.StatusPaid {
		t.Fatalf("expected PAID, got %s", fetched2.Status())
	}
	if len(fetched2.History()) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(fetched2.History()))
	}

	// Verify idempotency
	err = repo.Create(ctx, o, idempotencyKey)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate idem key, got %v", err)
	}
}
