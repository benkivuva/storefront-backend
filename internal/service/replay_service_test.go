package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

type mockReplayRepo struct {
	publishedEvents []domain.OutboxEvent
}

func (m *mockReplayRepo) Create(ctx context.Context, e domain.OutboxEvent) error { return nil }
func (m *mockReplayRepo) FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	return nil, nil
}
func (m *mockReplayRepo) MarkPublished(ctx context.Context, id domain.ID) error           { return nil }
func (m *mockReplayRepo) MarkDead(ctx context.Context, id domain.ID, reason string) error { return nil }
func (m *mockReplayRepo) FetchPublishedInRange(ctx context.Context, from, to time.Time, limit int) ([]domain.OutboxEvent, error) {
	return m.publishedEvents, nil
}
func (m *mockReplayRepo) DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}
func (m *mockReplayRepo) OldestPending(ctx context.Context) (time.Time, error) {
	return time.Time{}, nil
}

func TestReplayService_Replay(t *testing.T) {
	now := time.Now()
	evt1 := domain.OutboxEvent{ID: id1, EventType: "OrderCreated", CreatedAt: now}

	repo := &mockReplayRepo{
		publishedEvents: []domain.OutboxEvent{evt1},
	}
	pub := &mockPublisher{}

	replaySvc := service.NewReplayService(repo, pub)

	count, err := replaySvc.Replay(context.Background(), now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 replayed event, got %d", count)
	}
	if len(pub.published) != 1 {
		t.Errorf("expected 1 published event, got %d", len(pub.published))
	}
	if pub.published[0].ID != evt1.ID {
		t.Errorf("replayed wrong event")
	}
}
