package dto

import (
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type InitiatePaymentRequest struct {
	Method string `json:"method"`
	Phone  string `json:"phone,omitempty"`
}

type NextAction struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

type InitiatePaymentResponse struct {
	PaymentID         string     `json:"payment_id"`
	ProviderReference string     `json:"provider_reference,omitempty"`
	Provider          string     `json:"provider,omitempty"`
	Status            string     `json:"status"`
	NextAction        NextAction `json:"next_action"`
}

// PaymentStatus is the polling response for GET /orders/{id}/payments/status.
type PaymentStatus struct {
	PaymentID         string     `json:"payment_id"`
	OrderID           string     `json:"order_id"`
	Provider          string     `json:"provider"`
	ProviderReference string     `json:"provider_reference,omitempty"`
	Method            string     `json:"method"`
	Status            string     `json:"status"`
	AmountMinor       int64      `json:"amount_minor"`
	Currency          string     `json:"currency"`
	FailureReason     string     `json:"failure_reason,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	NextAction        NextAction `json:"next_action,omitempty"`
}

func FromPayment(p *domain.Payment) PaymentStatus {
	return PaymentStatus{
		PaymentID:         p.ID().String(),
		OrderID:           p.OrderID().String(),
		Provider:          p.Provider(),
		ProviderReference: p.ProviderRef(),
		Method:            string(p.Method()),
		Status:            string(p.Status()),
		AmountMinor:       p.AmountMinor(),
		Currency:          p.Currency(),
		FailureReason:     p.FailureReason(),
		CreatedAt:         p.CreatedAt(),
		UpdatedAt:         p.UpdatedAt(),
	}
}
