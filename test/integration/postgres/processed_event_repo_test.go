package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func TestProcessedEventRepo(t *testing.T) {
	pool, _ := setupOutboxDB(t) // Reusing the helper from outbox_repo_test.go
	ctx := context.Background()
	repo := postgres.NewProcessedEventRepo(pool)
	gen := idgen.New()

	consumer := "email-worker"
	eventID := gen.NewID()

	// 1. Should be false initially
	isProc, err := repo.IsProcessed(ctx, consumer, eventID)
	if err != nil {
		t.Fatalf("IsProcessed: %v", err)
	}
	if isProc {
		t.Fatal("expected event to be unprocessed initially")
	}

	// 2. Mark it processed
	if err := repo.MarkProcessed(ctx, consumer, eventID); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	// 3. Should be true now
	isProc, _ = repo.IsProcessed(ctx, consumer, eventID)
	if !isProc {
		t.Fatal("expected event to be processed")
	}

	// 4. Different consumer shouldn't see it
	isProc, _ = repo.IsProcessed(ctx, "analytics-worker", eventID)
	if isProc {
		t.Fatal("event should not be marked processed for a different consumer")
	}

	// 5. MarkProcessed should be idempotent (ON CONFLICT DO NOTHING)
	// Should not return an error even if we try to insert it again.
	if err := repo.MarkProcessed(ctx, consumer, eventID); err != nil {
		t.Fatalf("duplicate MarkProcessed should not error, got: %v", err)
	}
}

func TestProcessedEventRepo_ConcurrentMarks(t *testing.T) {
	pool, _ := setupOutboxDB(t)
	ctx := context.Background()
	repo := postgres.NewProcessedEventRepo(pool)
	gen := idgen.New()

	consumer := "email-worker"
	eventID := gen.NewID()

	var wg sync.WaitGroup
	const N = 10
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// If the SQL doesn't use ON CONFLICT DO NOTHING, this will throw duplicate key errors
			if err := repo.MarkProcessed(ctx, consumer, eventID); err != nil {
				t.Errorf("concurrent MarkProcessed failed: %v", err)
			}
		}()
	}
	wg.Wait()
}
