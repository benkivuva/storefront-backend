package service_test

import (
"context"
"errors"
"testing"

"github.com/benkivuva/storefront-backend/internal/domain"
"github.com/benkivuva/storefront-backend/internal/service"
)

var (
id1 = domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
id2 = domain.ID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}
)

type mockOutboxRepo struct {
pendingEvents []domain.OutboxEvent
publishedIDs  []domain.ID
deadIDs       []domain.ID
fetchErr      error
markPubErr    error
}

func (m *mockOutboxRepo) Create(ctx context.Context, e domain.OutboxEvent) error { return nil }

func (m *mockOutboxRepo) FetchPending(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
if m.fetchErr != nil {
return nil, m.fetchErr
}
return m.pendingEvents, nil
}

func (m *mockOutboxRepo) MarkPublished(ctx context.Context, id domain.ID) error {
if m.markPubErr != nil {
return m.markPubErr
}
m.publishedIDs = append(m.publishedIDs, id)
return nil
}

func (m *mockOutboxRepo) MarkDead(ctx context.Context, id domain.ID, reason string) error {
m.deadIDs = append(m.deadIDs, id)
return nil
}

type mockPublisher struct {
published []domain.OutboxEvent
fail      bool
}

func (m *mockPublisher) Publish(ctx context.Context, event domain.OutboxEvent) error {
if m.fail {
return errors.New("redis unreachable")
}
m.published = append(m.published, event)
return nil
}

func TestOutboxRelay_ProcessBatch_Success(t *testing.T) {
evt1 := domain.OutboxEvent{ID: id1, EventType: "OrderCreated"}
evt2 := domain.OutboxEvent{ID: id2, EventType: "OrderPaid"}

repo := &mockOutboxRepo{
pendingEvents: []domain.OutboxEvent{evt1, evt2},
}
pub := &mockPublisher{}

relay := service.NewOutboxRelay(repo, pub)

if err := relay.ProcessBatch(context.Background(), 10); err != nil {
t.Fatalf("ProcessBatch: %v", err)
}

if len(pub.published) != 2 {
t.Errorf("expected 2 events published, got %d", len(pub.published))
}
if len(repo.publishedIDs) != 2 {
t.Errorf("expected 2 events marked published, got %d", len(repo.publishedIDs))
}
if repo.publishedIDs[0] != evt1.ID || repo.publishedIDs[1] != evt2.ID {
t.Error("marked published wrong IDs")
}
}

func TestOutboxRelay_ProcessBatch_PublishFails_MarksDead(t *testing.T) {
evt1 := domain.OutboxEvent{ID: id1, EventType: "OrderCreated", Attempts: 3}

repo := &mockOutboxRepo{
pendingEvents: []domain.OutboxEvent{evt1},
}
pub := &mockPublisher{fail: true}

relay := service.NewOutboxRelay(repo, pub)

if err := relay.ProcessBatch(context.Background(), 10); err != nil {
t.Fatalf("ProcessBatch: %v", err)
}

if len(pub.published) != 0 {
t.Errorf("expected 0 published, got %d", len(pub.published))
}
if len(repo.deadIDs) != 1 {
t.Errorf("expected 1 event marked dead, got %d", len(repo.deadIDs))
}
}
