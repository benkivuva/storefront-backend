package domain

import (
	"errors"
	"time"
)

var (
	ErrPaymentNotFound      = errors.New("domain: payment not found")
	ErrInvalidPaymentStatus = errors.New("domain: invalid payment status transition")
	ErrPaymentAlreadyOpen   = errors.New("domain: an open payment already exists for this order")
	ErrAmountMismatch       = errors.New("domain: payment amount does not match order total")
)

type PaymentStatus string

const (
	PaymentInitiated PaymentStatus = "INITIATED"
	PaymentPending   PaymentStatus = "PENDING"
	PaymentSucceeded PaymentStatus = "SUCCEEDED"
	PaymentFailed    PaymentStatus = "FAILED"
	PaymentExpired   PaymentStatus = "EXPIRED"
	PaymentOrphaned  PaymentStatus = "ORPHANED"
	PaymentRefunded  PaymentStatus = "REFUNDED"
)

type PaymentMethod string

const (
	MethodCard        PaymentMethod = "card"
	MethodAirtelMoney PaymentMethod = "airtel_money"
	MethodMpesa       PaymentMethod = "mpesa"
)

type Payment struct {
	id            ID
	orderID       ID
	provider      string
	method        PaymentMethod
	status        PaymentStatus
	amountMinor   int64
	currency      string
	providerRef   string
	providerTxnID string
	failureReason string
	createdAt     time.Time
	updatedAt     time.Time
}

func NewPayment(id, orderID ID, provider string, method PaymentMethod, amountMinor int64, currency string, at time.Time) (*Payment, error) {
	if id.IsZero() || orderID.IsZero() {
		return nil, ErrZeroUserID
	}
	if amountMinor < 0 {
		return nil, ErrAmountMismatch
	}
	return &Payment{
		id:          id,
		orderID:     orderID,
		provider:    provider,
		method:      method,
		status:      PaymentInitiated,
		amountMinor: amountMinor,
		currency:    currency,
		createdAt:   at,
		updatedAt:   at,
	}, nil
}

func (p *Payment) MarkPending(providerRef string, at time.Time) error {
	if p.status != PaymentInitiated {
		return ErrInvalidPaymentStatus
	}
	p.status = PaymentPending
	p.providerRef = providerRef
	p.updatedAt = at
	return nil
}

func (p *Payment) MarkSucceeded(providerTxnID string, at time.Time) error {
	if p.status != PaymentPending && p.status != PaymentInitiated {
		return ErrInvalidPaymentStatus
	}
	p.status = PaymentSucceeded
	p.providerTxnID = providerTxnID
	p.updatedAt = at
	return nil
}

func (p *Payment) MarkFailed(reason string, at time.Time) error {
	p.status = PaymentFailed
	p.failureReason = reason
	p.updatedAt = at
	return nil
}

func (p *Payment) MarkExpired(at time.Time) error {
	if p.status != PaymentInitiated && p.status != PaymentPending {
		return ErrInvalidPaymentStatus
	}
	p.status = PaymentExpired
	p.updatedAt = at
	return nil
}

func (p *Payment) MarkOrphaned(reason string, at time.Time) error {
	p.status = PaymentOrphaned
	p.failureReason = reason
	p.updatedAt = at
	return nil
}

// Getters
func (p *Payment) ID() ID                { return p.id }
func (p *Payment) OrderID() ID           { return p.orderID }
func (p *Payment) Provider() string      { return p.provider }
func (p *Payment) Method() PaymentMethod { return p.method }
func (p *Payment) Status() PaymentStatus { return p.status }
func (p *Payment) AmountMinor() int64    { return p.amountMinor }
func (p *Payment) Currency() string      { return p.currency }
func (p *Payment) ProviderRef() string   { return p.providerRef }
func (p *Payment) ProviderTxnID() string { return p.providerTxnID }
func (p *Payment) FailureReason() string { return p.failureReason }
func (p *Payment) CreatedAt() time.Time  { return p.createdAt }
func (p *Payment) UpdatedAt() time.Time  { return p.updatedAt }
