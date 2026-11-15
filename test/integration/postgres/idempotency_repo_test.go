package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/adapter/postgres/migrate"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func TestIdempotencyRepo(t *testing.T) {
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

	repo := postgres.NewIdempotencyRepo(pool)

	userID := idgen.New().NewID()
	key := "idem-key-123"
	hash1 := []byte("hash1")
	hash2 := []byte("hash2")
	body := json.RawMessage(`{"order_id":"123"}`)

	// 1. Missing key
	_, err = repo.Get(ctx, userID, key)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. Save
	if err := repo.Save(ctx, userID, key, hash1, 201, body); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 3. Fetch and verify
	res, err := repo.Get(ctx, userID, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	var resJSON, expectedJSON map[string]interface{}
	_ = json.Unmarshal(res.Body, &resJSON)
	_ = json.Unmarshal(body, &expectedJSON)

	if res.Status != 201 || !reflect.DeepEqual(resJSON, expectedJSON) {
		t.Fatalf("mismatch: %+v", res)
	}

	// 4. Save same key, different hash
	err = repo.Save(ctx, userID, key, hash2, 200, body)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for hash mismatch, got %v", err)
	}
}
