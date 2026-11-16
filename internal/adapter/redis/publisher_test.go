package redis

import (
"context"
"testing"
"time"

"github.com/benkivuva/storefront-backend/internal/domain"
)

func TestEventPublisher_Publish(t *testing.T) {
c := setupRedis(t)
ctx := context.Background()

pub := NewEventPublisher(c, "events")

evt := domain.OutboxEvent{
ID:            domain.ID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
AggregateType: "order",
AggregateID:   domain.ID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20},
EventType:     "OrderCreated",
Payload:       []byte(`{"order_id":"123"}`),
CreatedAt:     time.Now(),
}

if err := pub.Publish(ctx, evt); err != nil {
t.Fatalf("Publish: %v", err)
}

// Verify the stream exists and contains our event
streamKey := "events:order"
msgs, err := c.XRange(ctx, streamKey, "-", "+").Result()
if err != nil {
t.Fatalf("XRange: %v", err)
}
if len(msgs) != 1 {
t.Fatalf("expected 1 message in %s, got %d", streamKey, len(msgs))
}

fields := msgs[0].Values
if fields["event_id"] != evt.ID.String() {
t.Errorf("event_id = %v, want %s", fields["event_id"], evt.ID)
}
if fields["event_type"] != "OrderCreated" {
t.Errorf("event_type = %v", fields["event_type"])
}
if fields["aggregate_type"] != "order" {
t.Errorf("aggregate_type = %v", fields["aggregate_type"])
}
if fields["payload"] != `{"order_id":"123"}` {
t.Errorf("payload = %v", fields["payload"])
}

// Verify the stream is different for a different aggregate type
evt2 := evt
evt2.AggregateType = "payment"
evt2.EventType = "PaymentSucceeded"
if err := pub.Publish(ctx, evt2); err != nil {
t.Fatalf("Publish evt2: %v", err)
}

payMsgs, _ := c.XRange(ctx, "events:payment", "-", "+").Result()
if len(payMsgs) != 1 {
t.Fatalf("expected 1 message in events:payment, got %d", len(payMsgs))
}
}
