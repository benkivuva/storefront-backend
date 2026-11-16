package service_test

import (
"context"
"errors"
"testing"
"time"

"github.com/benkivuva/storefront-backend/internal/domain"
"github.com/benkivuva/storefront-backend/internal/service"
)

type mockStreamReader struct {
msgs         []domain.StreamMessage
readErr      error
ackedIDs     []string
dlqMsgs      []domain.StreamMessage
}

func (m *mockStreamReader) ReadGroup(ctx context.Context, group, consumer string, count int, block time.Duration) ([]domain.StreamMessage, error) {
return m.msgs, m.readErr
}

func (m *mockStreamReader) Ack(ctx context.Context, group string, msgID string) error {
m.ackedIDs = append(m.ackedIDs, msgID)
return nil
}

func (m *mockStreamReader) MoveToDLQ(ctx context.Context, group string, msg domain.StreamMessage, reason string) error {
m.dlqMsgs = append(m.dlqMsgs, msg)
return nil
}

type mockHandler struct {
handledEvents []domain.OutboxEvent
shouldFail    bool
}

func (m *mockHandler) Handle(ctx context.Context, event domain.OutboxEvent) error {
if m.shouldFail {
return errors.New("handler failed")
}
m.handledEvents = append(m.handledEvents, event)
return nil
}

type mockProcessedRepo struct {
processed map[string]bool
marked    []string
}

func (m *mockProcessedRepo) IsProcessed(ctx context.Context, consumer string, eventID domain.ID) (bool, error) {
return m.processed[eventID.String()], nil
}

func (m *mockProcessedRepo) MarkProcessed(ctx context.Context, consumer string, eventID domain.ID) error {
m.marked = append(m.marked, eventID.String())
m.processed[eventID.String()] = true
return nil
}

func TestConsumer_ProcessNextBatch_Success(t *testing.T) {
evt := domain.OutboxEvent{ID: id1, EventType: "OrderCreated"}
reader := &mockStreamReader{
msgs: []domain.StreamMessage{{ID: "1-0", Event: evt}},
}
handler := &mockHandler{}
repo := &mockProcessedRepo{processed: make(map[string]bool)}

c := service.NewConsumer("test-group", "worker-1", reader, repo, handler)

if err := c.ProcessNextBatch(context.Background(), 10); err != nil {
t.Fatalf("ProcessNextBatch: %v", err)
}

if len(handler.handledEvents) != 1 {
t.Errorf("expected 1 handled event, got %d", len(handler.handledEvents))
}
if len(repo.marked) != 1 {
t.Errorf("expected 1 marked processed, got %d", len(repo.marked))
}
if len(reader.ackedIDs) != 1 || reader.ackedIDs[0] != "1-0" {
t.Errorf("expected message 1-0 to be acked")
}
}

func TestConsumer_ProcessNextBatch_AlreadyProcessed(t *testing.T) {
evt := domain.OutboxEvent{ID: id1, EventType: "OrderCreated"}
reader := &mockStreamReader{
msgs: []domain.StreamMessage{{ID: "1-0", Event: evt}},
}
handler := &mockHandler{}
// Simulate that this event is already in the DB
repo := &mockProcessedRepo{processed: map[string]bool{id1.String(): true}}

c := service.NewConsumer("test-group", "worker-1", reader, repo, handler)

if err := c.ProcessNextBatch(context.Background(), 10); err != nil {
t.Fatalf("ProcessNextBatch: %v", err)
}

// Should NOT call the handler
if len(handler.handledEvents) != 0 {
t.Errorf("expected 0 handled events, got %d", len(handler.handledEvents))
}
// Should STILL Ack the message so it doesn't get stuck in the queue
if len(reader.ackedIDs) != 1 {
t.Errorf("expected message to be acked even though skipped")
}
}

func TestConsumer_ProcessNextBatch_HandlerFails_MovesToDLQ(t *testing.T) {
evt := domain.OutboxEvent{ID: id1, EventType: "OrderCreated"}
reader := &mockStreamReader{
msgs: []domain.StreamMessage{{ID: "1-0", Event: evt}},
}
handler := &mockHandler{shouldFail: true}
repo := &mockProcessedRepo{processed: make(map[string]bool)}

c := service.NewConsumer("test-group", "worker-1", reader, repo, handler)

if err := c.ProcessNextBatch(context.Background(), 10); err != nil {
t.Fatalf("ProcessNextBatch: %v", err)
}

// Handler failed, so should NOT be marked processed
if len(repo.marked) != 0 {
t.Errorf("expected 0 marked processed, got %d", len(repo.marked))
}
// Should be moved to DLQ
if len(reader.dlqMsgs) != 1 {
t.Errorf("expected 1 DLQ message, got %d", len(reader.dlqMsgs))
}
// Should STILL Ack the message so it is removed from the Pending Entries List (PEL)
if len(reader.ackedIDs) != 1 {
t.Errorf("expected message to be acked after DLQ")
}
}
