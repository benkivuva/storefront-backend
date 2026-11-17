package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type EmailHandler struct {
	mailer domain.Mailer
}

func NewEmailHandler(mailer domain.Mailer) *EmailHandler {
	return &EmailHandler{mailer: mailer}
}

type orderCreatedPayload struct {
	ID            string `json:"id"`
	CustomerEmail string `json:"customer_email"`
	TotalMinor    int64  `json:"total_minor"`
	Currency      string `json:"currency"`
}

func (h *EmailHandler) Handle(ctx context.Context, event domain.OutboxEvent) error {
	if event.EventType != "OrderCreated" {
		return nil
	}

	var payload orderCreatedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal order created payload: %w", err)
	}

	msg := domain.MailMessage{
		To:      payload.CustomerEmail,
		Subject: fmt.Sprintf("Your Storefront Order #%s", payload.ID),
		Body:    fmt.Sprintf("Thank you for your purchase. Total: %d %s", payload.TotalMinor, payload.Currency),
	}

	if err := h.mailer.Send(ctx, msg); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}
