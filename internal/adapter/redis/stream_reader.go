package redis

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type StreamReader struct {
	client     *goredis.Client
	streamName string
	dlqName    string
}

func NewStreamReader(client *goredis.Client, streamName, dlqName string) *StreamReader {
	return &StreamReader{
		client:     client,
		streamName: streamName,
		dlqName:    dlqName,
	}
}

func (r *StreamReader) ReadGroup(ctx context.Context, group, consumer string, count int, block time.Duration) ([]domain.StreamMessage, error) {
	streams, err := r.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{r.streamName, ">"},
		Count:    int64(count),
		Block:    block,
	}).Result()

	if err == goredis.Nil {
		return nil, nil // Timeout, no messages
	}
	if err != nil {
		return nil, fmt.Errorf("xreadgroup: %w", err)
	}

	var msgs []domain.StreamMessage
	for _, stream := range streams {
		for _, msg := range stream.Messages {
			var e domain.OutboxEvent

			idStr, _ := msg.Values["event_id"].(string)
			e.ID = parseUUID(idStr)

			e.EventType, _ = msg.Values["event_type"].(string)
			e.AggregateType, _ = msg.Values["aggregate_type"].(string)

			aggIDStr, _ := msg.Values["aggregate_id"].(string)
			e.AggregateID = parseUUID(aggIDStr)

			payloadStr, _ := msg.Values["payload"].(string)
			e.Payload = []byte(payloadStr)

			msgs = append(msgs, domain.StreamMessage{
				ID:    msg.ID,
				Event: e,
			})
		}
	}
	return msgs, nil
}

func (r *StreamReader) Ack(ctx context.Context, group string, msgID string) error {
	if err := r.client.XAck(ctx, r.streamName, group, msgID).Err(); err != nil {
		return fmt.Errorf("xack: %w", err)
	}
	return nil
}

func (r *StreamReader) MoveToDLQ(ctx context.Context, group string, msg domain.StreamMessage, reason string) error {
	vals := map[string]interface{}{
		"event_id":       msg.Event.ID.String(),
		"event_type":     msg.Event.EventType,
		"aggregate_type": msg.Event.AggregateType,
		"aggregate_id":   msg.Event.AggregateID.String(),
		"payload":        string(msg.Event.Payload),
		"reason":         reason,
		"original_id":    msg.ID,
	}

	if err := r.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: r.dlqName,
		Values: vals,
	}).Err(); err != nil {
		return fmt.Errorf("xadd to dlq: %w", err)
	}

	return r.Ack(ctx, group, msg.ID)
}

func parseUUID(s string) domain.ID {
	s = strings.ReplaceAll(s, "-", "")
	b, err := hex.DecodeString(s)
	if err != nil {
		return domain.ID{}
	}
	var id domain.ID
	copy(id[:], b)
	return id
}
