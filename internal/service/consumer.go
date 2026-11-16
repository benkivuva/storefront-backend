package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type Consumer struct {
	group        string
	consumerName string
	reader       domain.StreamReader
	processed    domain.ProcessedEventRepository
	handler      domain.EventHandler
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
	msgs, err := c.reader.ReadGroup(ctx, c.group, c.consumerName, limit, 2*time.Second)
	if err != nil {
		return fmt.Errorf("read group: %w", err)
	}

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

	return nil
}
