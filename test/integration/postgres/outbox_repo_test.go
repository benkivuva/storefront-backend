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

func setupOutboxDB(t *testing.T) (*pgxpool.Pool, *postgres.OutboxRepo) {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("test"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	url, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	if err := migrate.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, _ := pgxpool.New(ctx, url)
	t.Cleanup(pool.Close)

	return pool, postgres.NewOutboxRepo(pool)
}

func TestOutboxRepo_Lifecycle(t *testing.T) {
	pool, repo := setupOutboxDB(t)
	ctx := context.Background()
	gen := idgen.New()

	evt1 := domain.OutboxEvent{
		ID:            gen.NewID(),
		AggregateType: "order",
		AggregateID:   gen.NewID(),
		EventType:     "OrderCreated",
		Payload:       []byte(`{"order_id":"123"}`),
		CreatedAt:     time.Now(),
	}
	evt2 := domain.OutboxEvent{
		ID:            gen.NewID(),
		AggregateType: "order",
		AggregateID:   gen.NewID(),
		EventType:     "OrderPaid",
		Payload:       []byte(`{"order_id":"123"}`),
		CreatedAt:     time.Now(),
	}

	// 1. Create
	if err := repo.Create(ctx, evt1); err != nil {
		t.Fatalf("Create evt1: %v", err)
	}
	if err := repo.Create(ctx, evt2); err != nil {
		t.Fatalf("Create evt2: %v", err)
	}

	// 2. FetchPending
	events, err := repo.FetchPending(ctx, 10)
	if err != nil {
		t.Fatalf("FetchPending: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != evt1.ID || events[1].ID != evt2.ID {
		t.Fatalf("events out of order")
	}

	// 3. MarkPublished (evt1)
	if err := repo.MarkPublished(ctx, evt1.ID); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	// 4. MarkDead (evt2)
	if err := repo.MarkDead(ctx, evt2.ID, "redis unreachable"); err != nil {
		t.Fatalf("MarkDead: %v", err)
	}

	// 5. Verify DB state
	var status1, status2 string
	var attempts2 int
	var errMsg2 string
	var pubAt *time.Time

	err = pool.QueryRow(ctx, `SELECT status, published_at FROM outbox_events WHERE event_id = $1`, evt1.ID.String()).Scan(&status1, &pubAt)
	if err != nil || status1 != "published" || pubAt == nil {
		t.Fatalf("evt1 not marked published correctly: %s %v", status1, err)
	}

	err = pool.QueryRow(ctx, `SELECT status, attempts, last_error FROM outbox_events WHERE event_id = $1`, evt2.ID.String()).Scan(&status2, &attempts2, &errMsg2)
	if err != nil || status2 != "dead" || attempts2 != 1 || errMsg2 != "redis unreachable" {
		t.Fatalf("evt2 not marked dead correctly: %s %d %s %v", status2, attempts2, errMsg2, err)
	}

	// 6. FetchPending should now be empty
	remaining, _ := repo.FetchPending(ctx, 10)
	if len(remaining) != 0 {
		t.Fatalf("expected 0 remaining pending events, got %d", len(remaining))
	}
}

func TestOutboxRepo_SkipLocked(t *testing.T) {
	pool, repo := setupOutboxDB(t)
	ctx := context.Background()
	gen := idgen.New()

	evt := domain.OutboxEvent{
		ID:            gen.NewID(),
		AggregateType: "order",
		AggregateID:   gen.NewID(),
		EventType:     "OrderCreated",
		Payload:       []byte(`{}`),
		CreatedAt:     time.Now(),
	}
	_ = repo.Create(ctx, evt)

	// Start a transaction and lock the row manually
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `SELECT * FROM outbox_events WHERE event_id = $1 FOR UPDATE`, evt.ID.String())
	if err != nil {
		t.Fatal(err)
	}

	// Now, while it's locked, try to FetchPending.
	// Because of SKIP LOCKED, it should return 0 events instead of hanging.
	events, err := repo.FetchPending(ctx, 10)
	if err != nil {
		t.Fatalf("FetchPending while locked: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events due to SKIP LOCKED, got %d", len(events))
	}
}

func TestOutboxRepo_DeletePublishedBefore(t *testing.T) {
	pool, repo := setupOutboxDB(t)
	ctx := context.Background()
	gen := idgen.New()

	// Create an old published event (8 days ago)
	oldEvt := domain.OutboxEvent{
		ID:            gen.NewID(),
		AggregateType: "order",
		AggregateID:   gen.NewID(),
		EventType:     "OrderCreated",
		Payload:       []byte(`{}`),
		CreatedAt:     time.Now().Add(-8 * 24 * time.Hour),
	}
	_ = repo.Create(ctx, oldEvt)
	_ = repo.MarkPublished(ctx, oldEvt.ID)
	// Force the DB created_at to 8 days ago for the test
	_, _ = pool.Exec(ctx, `UPDATE outbox_events SET created_at = $1 WHERE event_id = $2`, oldEvt.CreatedAt, oldEvt.ID.String())

	// Create a fresh published event (today)
	newEvt := domain.OutboxEvent{
		ID:            gen.NewID(),
		AggregateType: "order",
		AggregateID:   gen.NewID(),
		EventType:     "OrderCreated",
		Payload:       []byte(`{}`),
		CreatedAt:     time.Now(),
	}
	_ = repo.Create(ctx, newEvt)
	_ = repo.MarkPublished(ctx, newEvt.ID)

	// Create an old PENDING event (should NOT be deleted)
	pendingEvt := domain.OutboxEvent{
		ID:            gen.NewID(),
		AggregateType: "order",
		AggregateID:   gen.NewID(),
		EventType:     "OrderCreated",
		Payload:       []byte(`{}`),
		CreatedAt:     time.Now().Add(-8 * 24 * time.Hour),
	}
	_ = repo.Create(ctx, pendingEvt)
	_, _ = pool.Exec(ctx, `UPDATE outbox_events SET created_at = $1 WHERE event_id = $2`, pendingEvt.CreatedAt, pendingEvt.ID.String())

	// Run the cleanup (delete published older than 7 days)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	deletedCount, err := repo.DeletePublishedBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeletePublishedBefore: %v", err)
	}
	if deletedCount != 1 {
		t.Errorf("expected 1 row deleted, got %d", deletedCount)
	}

	// Verify the old published event is gone
	var count int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_id = $1`, oldEvt.ID.String()).Scan(&count)
	if count != 0 {
		t.Error("old published event should have been deleted")
	}

	// Verify the fresh published event still exists
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_id = $1`, newEvt.ID.String()).Scan(&count)
	if count != 1 {
		t.Error("fresh published event should still exist")
	}

	// Verify the old PENDING event still exists (safety!)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_id = $1`, pendingEvt.ID.String()).Scan(&count)
	if count != 1 {
		t.Error("old pending event should NOT be deleted")
	}
}
