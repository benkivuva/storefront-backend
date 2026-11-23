package domain

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

var (
	ErrNotFound = errors.New("domain: not found")
	ErrConflict = errors.New("domain: conflict")
)

type Clock interface {
	Now() time.Time
}

type RawTokenGenerator interface {
	NewRawToken() ([]byte, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encoded string) (match, needsRehash bool, err error)
}

type TokenSigner interface {
	Sign(userID ID, role Role, ttl time.Duration) (string, error)
}

type UserRepository interface {
	Create(ctx context.Context, u User) error
	FindByEmail(ctx context.Context, email string) (User, error)
	FindByID(ctx context.Context, id ID) (User, error)
	UpdateProfile(ctx context.Context, id ID, name, phone *string, at time.Time) error
	UpdatePasswordHash(ctx context.Context, id ID, hash string, at time.Time) error
}

type RefreshTokenRepository interface {
	Create(ctx context.Context, t RefreshToken) error
	FindByHash(ctx context.Context, hash []byte) (RefreshToken, error)
	Rotate(ctx context.Context, parent, child RefreshToken) error
	RevokeFamily(ctx context.Context, familyID ID, at time.Time) (int, error)
	ListByUser(ctx context.Context, userID ID) ([]RefreshToken, error)
	RevokeByUser(ctx context.Context, userID, tokenID ID, at time.Time) error
}

type AddressRepository interface {
	ListByUser(ctx context.Context, userID ID) ([]Address, error)
	Create(ctx context.Context, a Address) error
	FindByID(ctx context.Context, id ID) (Address, error)
}

type AuditEntry struct {
	Actor      ID
	Action     string
	EntityType string
	EntityID   string
	Before     []byte
	After      []byte
	IP         netip.Addr
	At         time.Time
}

type AuditLog interface {
	Append(ctx context.Context, e AuditEntry) error
}

type LockoutStore interface {
	Backoff(ctx context.Context, userID ID, ipPrefix string) (time.Duration, error)
	RecordFailedAttempt(ctx context.Context, userID ID, ipPrefix string) (time.Duration, error)
	Reset(ctx context.Context, userID ID, ipPrefix string) error
}

type ListFilters struct {
	CategoryID string
	Query      string
	MinPrice   int64
	MaxPrice   int64
	InStock    bool
}

type ProductRepository interface {
	Create(ctx context.Context, p Product) error
	GetByID(ctx context.Context, id ID) (Product, error)
	Update(ctx context.Context, p Product) error
	Delete(ctx context.Context, id ID, at time.Time) error
	List(ctx context.Context, filters ListFilters, limit int, cursor time.Time) ([]Product, error)
}

type CategoryRepository interface {
	Create(ctx context.Context, c Category) error
	List(ctx context.Context) ([]Category, error)
}

type InventoryRepository interface {
	AdjustStock(ctx context.Context, m StockMovement) error
	Create(ctx context.Context, inv Inventory) error
	ListLowStock(ctx context.Context) ([]Inventory, error)
}

type Cache interface {
	GetOrFetch(ctx context.Context, key string, ttl time.Duration, fetchFn func() ([]byte, error)) ([]byte, error)
	Delete(ctx context.Context, keys ...string) error
}

type OutboxEvent struct {
	ID            ID
	AggregateType string
	AggregateID   ID
	EventType     string
	Payload       []byte
	TraceContext  string
	Attempts      int
	LastError     string
	CreatedAt     time.Time
}

type OutboxRepository interface {
	Create(ctx context.Context, e OutboxEvent) error
	FetchPending(ctx context.Context, limit int) ([]OutboxEvent, error)
	MarkPublished(ctx context.Context, id ID) error
	MarkDead(ctx context.Context, id ID, reason string) error
	FetchPublishedInRange(ctx context.Context, from, to time.Time, limit int) ([]OutboxEvent, error)
	DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
	OldestPending(ctx context.Context) (time.Time, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, event OutboxEvent) error
}

type CartRepository interface {
	GetActiveByUser(ctx context.Context, userID ID) (Cart, error)
	Create(ctx context.Context, c Cart) error
	Update(ctx context.Context, c Cart) error
}

type CheckoutRepository interface {
	Execute(ctx context.Context, cartID ID, o *Order, idempotencyKey string) error
}

type OrderRepository interface {
	Create(ctx context.Context, o *Order, idempotencyKey string) error
	GetByID(ctx context.Context, id ID) (*Order, error)
	UpdateStatus(ctx context.Context, o *Order) error
	ListByUser(ctx context.Context, userID ID) ([]*Order, error)
}

type IdempotencyRecord struct {
	Status int
	Body   []byte
	Hash   []byte
}

type IdempotencyRepository interface {
	Get(ctx context.Context, userID ID, key string) (IdempotencyRecord, error)
	Lock(ctx context.Context, userID ID, key string, requestHash []byte) error
	Save(ctx context.Context, userID ID, key string, status int, body []byte) error
	Delete(ctx context.Context, userID ID, key string) error
}

type ProcessedEventRepository interface {
	IsProcessed(ctx context.Context, consumer string, eventID ID) (bool, error)
	MarkProcessed(ctx context.Context, consumer string, eventID ID) error
}

type StreamMessage struct {
	ID    string
	Event OutboxEvent
}

type StreamReader interface {
	ReadGroup(ctx context.Context, group, consumer string, count int, block time.Duration) ([]StreamMessage, error)
	Ack(ctx context.Context, group string, msgID string) error
	MoveToDLQ(ctx context.Context, group string, msg StreamMessage, reason string) error
	ReclaimStale(ctx context.Context, group, consumer string, minIdle time.Duration) ([]StreamMessage, error)
}

type EventHandler interface {
	Handle(ctx context.Context, event OutboxEvent) error
}

type MailMessage struct {
	To      string
	Subject string
	Body    string
}

type Mailer interface {
	Send(ctx context.Context, msg MailMessage) error
}

type InitiateRequest struct {
	PaymentID     ID
	OrderID       ID
	AmountMinor   int64
	Currency      string
	Method        PaymentMethod
	CustomerPhone string
	Email         string
	Reference     string
	CallbackURL   string
}

type NextAction struct {
	Type string // "redirect", "await_prompt", "none"
	URL  string // For redirect
}

type InitiateResult struct {
	ProviderRef string
	NextAction  NextAction
}

type WebhookEvent struct {
	ProviderEventID string
	ProviderRef     string
	Status          PaymentStatus
	AmountMinor     int64
	Currency        string
	ProviderTxnID   string
	FailureReason   string
	RawPayload      []byte
	SignatureValid  bool
}

type RefundRequest struct {
	PaymentID   ID
	AmountMinor int64
	Reason      string
}

type RefundResult struct {
	ProviderRef string
	Status      string
}

// PaymentProvider is the port for all payment gateways (Paystack, Daraja, Mock).
type PaymentProvider interface {
	Name() string
	Initiate(ctx context.Context, req InitiateRequest) (InitiateResult, error)
	VerifyWebhook(ctx context.Context, rawBody []byte, headers map[string]string) (WebhookEvent, error)
	Status(ctx context.Context, providerRef string) (WebhookEvent, error)
	Refund(ctx context.Context, req RefundRequest) (RefundResult, error)
}

// PaymentRepository is the database port for payments.
type PaymentRepository interface {
	Create(ctx context.Context, p *Payment) error
	GetByID(ctx context.Context, id ID) (*Payment, error)
	GetByProviderRef(ctx context.Context, provider, ref string) (*Payment, error)
	UpdateStatus(ctx context.Context, p *Payment) error
}

type WebhookRecord struct {
	ID              ID
	Provider        string
	ProviderEventID string
	SignatureValid  bool
	RawBody         []byte
	SourceIP        string
	ReceivedAt      time.Time
}

// WebhookRepository handles the atomic transaction required when processing a webhook.
type WebhookRepository interface {
	IsProcessed(ctx context.Context, provider, eventID string) (bool, error)
	// ProcessWebhookTx atomically saves the webhook event, updates the payment, updates the order (if not nil), and writes the outbox event (if not nil).
	ProcessWebhookTx(ctx context.Context, webhook WebhookRecord, payment *Payment, order *Order, outbox *OutboxEvent) error
}
