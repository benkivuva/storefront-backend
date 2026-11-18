package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type EmailHandler struct {
	mailer domain.Mailer
	users  domain.UserRepository
}

func NewEmailHandler(mailer domain.Mailer, users domain.UserRepository) *EmailHandler {
	return &EmailHandler{
		mailer: mailer,
		users:  users,
	}
}

type orderCreatedPayload struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	TotalMinor int64  `json:"total_minor"`
	Currency   string `json:"currency"`
}

func (h *EmailHandler) Handle(ctx context.Context, event domain.OutboxEvent) error {
	if event.EventType != "OrderCreated" {
		return nil
	}

	var payload orderCreatedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal order created payload: %w", err)
	}

	userID, err := domain.ParseID(payload.UserID)
	if err != nil {
		return fmt.Errorf("parse user id from event: %w", err)
	}

	user, err := h.users.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("fetch user for email: %w", err)
	}

	msg := domain.MailMessage{
		To:      user.Email(),
		Subject: fmt.Sprintf("Your Storefront Order #%s", payload.ID),
		Body:    fmt.Sprintf("Thank you for your purchase. Total: %d %s", payload.TotalMinor, payload.Currency),
	}

	if err := h.mailer.Send(ctx, msg); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}
