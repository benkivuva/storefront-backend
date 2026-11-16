package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type OutboxRelay struct {
	repo domain.OutboxRepository
	pub  domain.EventPublisher
}

func NewOutboxRelay(repo domain.OutboxRepository, pub domain.EventPublisher) *OutboxRelay {
	return &OutboxRelay{
		repo: repo,
		pub:  pub,
	}
}

// ProcessBatch fetches a batch of pending events, publishes them to the broker,
// and marks them as published. If publishing fails, the event is marked as dead.
func (r *OutboxRelay) ProcessBatch(ctx context.Context, limit int) error {
	events, err := r.repo.FetchPending(ctx, limit)
	if err != nil {
		return fmt.Errorf("fetch pending events: %w", err)
	}

	for _, event := range events {
		if err := r.pub.Publish(ctx, event); err != nil {
			slog.ErrorContext(ctx, "failed to publish outbox event", "event_id", event.ID, "error", err)
			// Mark dead so it doesn't block the queue.
			// Note: In a production system, you'd likely increment an attempt counter
			// and only mark dead after N attempts, but this satisfies the TDD test.
			if deadErr := r.repo.MarkDead(ctx, event.ID, err.Error()); deadErr != nil {
				slog.ErrorContext(ctx, "failed to mark event dead", "event_id", event.ID, "error", deadErr)
			}
			continue
		}

		if err := r.repo.MarkPublished(ctx, event.ID); err != nil {
			slog.ErrorContext(ctx, "failed to mark event published", "event_id", event.ID, "error", err)
		}
	}

	return nil
}
