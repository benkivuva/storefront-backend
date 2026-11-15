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

func TestCartRepo(t *testing.T) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("test"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2)))
	if err != nil {
		t.Fatal(err)
	}
	defer ctr.Terminate(ctx)

	url, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	if err := migrate.Up(ctx, url); err != nil {
		t.Fatal(err)
	}

	pool, _ := pgxpool.New(ctx, url)
	defer pool.Close()

	repo := postgres.NewCartRepo(pool, idgen.New())

	// Setup dependencies
	userID := idgen.New().NewID()
	prodID := idgen.New().NewID()

	_, err = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, role) VALUES ($1, 'a@b.com', 'h', 'customer')`, [16]byte(userID))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO products (id, sku, name, price_minor, currency, tax_class) VALUES ($1, 'SKU1', 'P1', 1000, 'KES', 'STANDARD_16')`, [16]byte(prodID))
	if err != nil {
		t.Fatal(err)
	}

	// Test 1: Not found
	_, err = repo.GetActiveByUser(ctx, userID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Test 2: Create
	now := time.Now().UTC()
	cartID := idgen.New().NewID()
	c, _ := domain.NewCart(cartID, userID, now)
	c.AddItem(prodID, 2)

	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Test 3: Get Active
	fetched, err := repo.GetActiveByUser(ctx, userID)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if fetched.ID() != cartID || len(fetched.Items()) != 1 || fetched.Items()[0].Quantity() != 2 {
		t.Fatalf("mismatch on fetch: %+v", fetched)
	}

	// Test 4: Update (Add item)
	c.UpdateQuantity(prodID, 5)
	if err := repo.Update(ctx, c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	fetched2, _ := repo.GetActiveByUser(ctx, userID)
	if fetched2.Items()[0].Quantity() != 5 {
		t.Fatalf("update failed, got %d", fetched2.Items()[0].Quantity())
	}
}
