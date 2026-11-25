package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Name() string {
	return "mock"
}

func (p *Provider) Initiate(ctx context.Context, req domain.InitiateRequest) (domain.InitiateResult, error) {
	if req.AmountMinor <= 0 {
		return domain.InitiateResult{}, errors.New("mock: amount must be greater than zero")
	}

	if strings.HasSuffix(req.CustomerPhone, "000") {
		return domain.InitiateResult{}, errors.New("mock: STK push failed, invalid phone number")
	}

	ref := fmt.Sprintf("mock-ref-%s", req.PaymentID.String())

	return domain.InitiateResult{
		ProviderRef: ref,
		NextAction: domain.NextAction{
			Type: "redirect",
			URL:  fmt.Sprintf("https://mock-payment-gateway.local/pay?ref=%s", ref),
		},
	}, nil
}

func (p *Provider) VerifyWebhook(ctx context.Context, rawBody []byte, headers map[string]string) (domain.WebhookEvent, error) {
	var payload struct {
		EventID  string `json:"event_id"`
		Ref      string `json:"reference"`
		Status   string `json:"status"`
		Amount   int64  `json:"amount_minor"`
		Currency string `json:"currency"`
		TxnID    string `json:"transaction_id"`
		Failure  string `json:"failure_reason"`
	}

	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return domain.WebhookEvent{}, fmt.Errorf("mock: invalid webhook payload: %w", err)
	}

	return domain.WebhookEvent{
		ProviderEventID: payload.EventID,
		ProviderRef:     payload.Ref,
		Status:          domain.PaymentStatus(payload.Status),
		AmountMinor:     payload.Amount,
		Currency:        payload.Currency,
		ProviderTxnID:   payload.TxnID,
		FailureReason:   payload.Failure,
		RawPayload:      rawBody,
		SignatureValid:  true,
	}, nil
}

func (p *Provider) Status(ctx context.Context, providerRef string) (domain.WebhookEvent, error) {
	if strings.Contains(providerRef, "timeout") {
		return domain.WebhookEvent{}, errors.New("mock: provider API timeout (network error)")
	}

	if strings.Contains(providerRef, "fail") {
		return domain.WebhookEvent{
			ProviderRef:   providerRef,
			Status:        domain.PaymentFailed,
			FailureReason: "INSUFFICIENT_FUNDS",
			RawPayload:    []byte(`{"status":"failed"}`),
		}, nil
	}

	if strings.Contains(providerRef, "pending") {
		return domain.WebhookEvent{
			ProviderRef: providerRef,
			Status:      domain.PaymentPending,
			RawPayload:  []byte(`{"status":"pending"}`),
		}, nil
	}

	return domain.WebhookEvent{
		ProviderRef:   providerRef,
		Status:        domain.PaymentSucceeded,
		ProviderTxnID: fmt.Sprintf("txn-%d", time.Now().Unix()),
		RawPayload:    []byte(`{"status":"succeeded"}`),
	}, nil
}

func (p *Provider) Refund(ctx context.Context, req domain.RefundRequest) (domain.RefundResult, error) {
	if req.AmountMinor <= 0 {
		return domain.RefundResult{}, errors.New("mock: refund amount must be greater than zero")
	}
	return domain.RefundResult{
		ProviderRef: fmt.Sprintf("mock-refund-%s", req.PaymentID.String()),
		Status:      "processed",
	}, nil
}
