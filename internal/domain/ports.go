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

// UserRepository: Find* return ErrNotFound; Create returns ErrConflict on a
// duplicate email.
type UserRepository interface {
	Create(ctx context.Context, u User) error
	FindByEmail(ctx context.Context, email string) (User, error)
	FindByID(ctx context.Context, id ID) (User, error)
	// UpdateProfile writes only the non-nil fields.
	UpdateProfile(ctx context.Context, id ID, name, phone *string, at time.Time) error
	UpdatePasswordHash(ctx context.Context, id ID, hash string, at time.Time) error
}

// RefreshTokenRepository: Rotate must, in one transaction, run
// "UPDATE refresh_tokens SET revoked_at = ..., replaced_by = ...
// WHERE id = $parent AND revoked_at IS NULL" and insert the child. If that
// UPDATE affects no rows (a concurrent rotation) it returns ErrConflict.
// RevokeByUser returns ErrNotFound if the token is not the user's.
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

// AuditEntry.IP is persisted as NULL when it is the zero netip.Addr.
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

// LockoutStore applies escalating backoff to failed logins, keyed on
// (userID, ipPrefix) so one network cannot lock the owner out of others.
// ipPrefix is a /24 (IPv4) or /64 (IPv6).
type LockoutStore interface {
	// Backoff returns how long the caller must still wait; zero means none.
	Backoff(ctx context.Context, userID ID, ipPrefix string) (time.Duration, error)
	// RecordFailedAttempt returns the backoff that applies from now.
	RecordFailedAttempt(ctx context.Context, userID ID, ipPrefix string) (time.Duration, error)
	Reset(ctx context.Context, userID ID, ipPrefix string) error
}

// ProductRepository persists Products.
type ProductRepository interface {
	Create(ctx context.Context, p Product) error
	GetByID(ctx context.Context, id ID) (Product, error)
	Update(ctx context.Context, p Product) error
	List(ctx context.Context, limit int, cursor time.Time) ([]Product, error)
}

// CategoryRepository persists Categories.
type CategoryRepository interface {
	Create(ctx context.Context, c Category) error
	List(ctx context.Context) ([]Category, error)
}

// InventoryRepository adjusts stock and records ledger entries.
type InventoryRepository interface {
	AdjustStock(ctx context.Context, m StockMovement) error
}

// Cache is a generic byte cache (implemented by internal/adapter/redis).
type Cache interface {
	GetOrFetch(ctx context.Context, key string, ttl time.Duration, fetchFn func() ([]byte, error)) ([]byte, error)
	Delete(ctx context.Context, keys ...string) error
}
