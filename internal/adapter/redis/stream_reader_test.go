package redis

import (
"context"
"testing"
"time"

goredis "github.com/redis/go-redis/v9"

"github.com/benkivuva/storefront-backend/internal/domain"
)

func TestRedisStreamReader_Lifecycle(t *testing.T) {
c := setupRedis(t)
ctx := context.Background()

streamName := "events:test"
group := "test-group"
consumer := "worker-1"
dlqName := "events:dlq"

reader := NewStreamReader(c, streamName, dlqName)

// Create the consumer group so ReadGroup doesn't fail with NOGROUP
if err := c.XGroupCreateMkStream(ctx, streamName, group, "0").Err(); err != nil {
t.Fatalf("XGroupCreateMkStream: %v", err)
}

// 1. Push a message to the stream
evtID := domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
_, err := c.XAdd(ctx, &goredis.XAddArgs{
Stream: streamName,
Values: map[string]interface{}{
"event_id":       evtID.String(),
"event_type":     "OrderCreated",
"aggregate_type": "order",
"aggregate_id":   evtID.String(),
"payload":        `{"order_id":"123"}`,
"created_at":     time.Now().Format(time.RFC3339),
},
}).Result()
if err != nil {
t.Fatalf("XAdd: %v", err)
}

// 2. Read the message
msgs, err := reader.ReadGroup(ctx, group, consumer, 10, 100*time.Millisecond)
if err != nil {
t.Fatalf("ReadGroup: %v", err)
}
if len(msgs) != 1 {
t.Fatalf("expected 1 message, got %d", len(msgs))
}

msg := msgs[0]
if msg.Event.ID != evtID {
t.Errorf("expected event ID %s, got %s", evtID, msg.Event.ID)
}
if msg.Event.EventType != "OrderCreated" {
t.Errorf("expected OrderCreated, got %s", msg.Event.EventType)
}

// 3. Move to DLQ (this should internally ACK the message)
if err := reader.MoveToDLQ(ctx, group, msg, "test failure"); err != nil {
t.Fatalf("MoveToDLQ: %v", err)
}

// 4. Verify PEL (Pending Entries List) is empty (Ack was sent)
pending, err := c.XPending(ctx, streamName, group).Result()
if err != nil {
t.Fatalf("XPending: %v", err)
}
if pending.Count != 0 {
t.Fatalf("expected 0 pending messages, got %d", pending.Count)
}

// 5. Verify the DLQ stream has the message
dlqMsgs, err := c.XRange(ctx, dlqName, "-", "+").Result()
if err != nil {
t.Fatalf("XRange DLQ: %v", err)
}
if len(dlqMsgs) != 1 {
t.Fatalf("expected 1 message in DLQ, got %d", len(dlqMsgs))
}
if dlqMsgs[0].Values["reason"] != "test failure" {
t.Errorf("expected DLQ reason 'test failure', got %v", dlqMsgs[0].Values["reason"])
}
}
