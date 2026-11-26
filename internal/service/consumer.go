package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

const (
	staleMessageAge   = 30 * time.Second
	reclaimInterval   = 30 * time.Second
	reclaimBatchLimit = 100
)

type Consumer struct {
	group        string
	consumerName string
	reader       domain.StreamReader
	processed    domain.ProcessedEventRepository
	handler      domain.EventHandler
	lastReclaim  time.Time
}

func NewConsumer(group, consumerName string, reader domain.StreamReader, processed domain.ProcessedEventRepository, handler domain.EventHandler) *Consumer {
	return &Consumer{
		group:        group,
		consumerName: consumerName,
		reader:       reader,
		processed:    processed,
		handler:      handler,
	}
}

func (c *Consumer) ProcessNextBatch(ctx context.Context, limit int) error {
	// Messages abandoned by a crashed consumer sit in the PEL forever unless
	// another consumer claims them. Do that periodically before reading new
	// entries.
	if c.lastReclaim.IsZero() || time.Since(c.lastReclaim) >= reclaimInterval {
		stale, err := c.reader.ReclaimStale(ctx, c.group, c.consumerName, staleMessageAge)
		if err != nil {
			slog.WarnContext(ctx, "failed to reclaim stale messages", "group", c.group, "error", err)
		} else if len(stale) > 0 {
			slog.InfoContext(ctx, "reclaimed stale messages", "group", c.group, "count", len(stale))
			c.processMessages(ctx, stale)
		}
		c.lastReclaim = time.Now()
	}

	msgs, err := c.reader.ReadGroup(ctx, c.group, c.consumerName, limit, 2*time.Second)
	if err != nil {
		return fmt.Errorf("read group: %w", err)
	}
	c.processMessages(ctx, msgs)
	return nil
}

func (c *Consumer) processMessages(ctx context.Context, msgs []domain.StreamMessage) {
	for _, msg := range msgs {
		isProcessed, err := c.processed.IsProcessed(ctx, c.group, msg.Event.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to check processed status", "event_id", msg.Event.ID, "error", err)
			continue
		}

		if isProcessed {
			_ = c.reader.Ack(ctx, c.group, msg.ID)
			continue
		}

		if err := c.handler.Handle(ctx, msg.Event); err != nil {
			slog.ErrorContext(ctx, "handler failed, moving to DLQ", "event_id", msg.Event.ID, "error", err)
			_ = c.reader.MoveToDLQ(ctx, c.group, msg, err.Error())
			_ = c.reader.Ack(ctx, c.group, msg.ID)
			continue
		}

		if err := c.processed.MarkProcessed(ctx, c.group, msg.Event.ID); err != nil {
			slog.ErrorContext(ctx, "failed to mark processed", "event_id", msg.Event.ID, "error", err)
			continue
		}

		if err := c.reader.Ack(ctx, c.group, msg.ID); err != nil {
			slog.ErrorContext(ctx, "failed to ack", "msg_id", msg.ID, "error", err)
		}
	}
}
