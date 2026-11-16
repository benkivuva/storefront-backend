package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type EventPublisher struct {
	client *goredis.Client
	prefix string
}

func NewEventPublisher(client *goredis.Client, prefix string) *EventPublisher {
	return &EventPublisher{
		client: client,
		prefix: prefix,
	}
}

func (p *EventPublisher) Publish(ctx context.Context, e domain.OutboxEvent) error {
	streamKey := fmt.Sprintf("%s:%s", p.prefix, e.AggregateType)

	args := &goredis.XAddArgs{
		Stream: streamKey,
		MaxLen: 100000,
		Approx: true,
		Values: map[string]interface{}{
			"event_id":       e.ID.String(),
			"event_type":     e.EventType,
			"aggregate_type": e.AggregateType,
			"aggregate_id":   e.AggregateID.String(),
			"payload":        string(e.Payload),
			"created_at":     e.CreatedAt.Format(time.RFC3339Nano),
		},
	}

	if err := p.client.XAdd(ctx, args).Err(); err != nil {
		return fmt.Errorf("xadd stream %s: %w", streamKey, err)
	}
	return nil
}
