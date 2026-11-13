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

// --- Catalog Interfaces ---

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
	CreatedAt     time.Time
}

type OutboxRepository interface {
	Create(ctx context.Context, e OutboxEvent) error
}
