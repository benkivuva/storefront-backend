package domain

import (
"context"
"errors"
"time"
)

var (
ErrNotFound = errors.New("domain: not found")
ErrConflict = errors.New("domain: conflict")
)

// Clock supplies the current time. Services take this instead of calling
// time.Now directly, so tests are deterministic.
type Clock interface {
Now() time.Time
}

// RawTokenGenerator produces cryptographically random bytes for refresh and
// reset tokens. The returned bytes are what the client holds; only their
// SHA-256 hash is stored.
type RawTokenGenerator interface {
NewRawToken() ([]byte, error)
}

// PasswordHasher wraps argon2id. The concrete adapter owns the params and
// the encoded format; the service sees only the two operations.
type PasswordHasher interface {
Hash(password string) (string, error)
Verify(password, encoded string) (match, needsRehash bool, err error)
}

// UserRepository persists Users. Find* return ErrNotFound when the user does
// not exist. Create returns ErrConflict on duplicate email.
type UserRepository interface {
Create(ctx context.Context, u User) error
FindByEmail(ctx context.Context, email string) (User, error)
FindByID(ctx context.Context, id ID) (User, error)
Update(ctx context.Context, u User) error
}

// RefreshTokenRepository persists RefreshTokens. Rotate must be atomic:
// parent revoked and child created in one transaction. If either fails, both
// roll back — otherwise a user could be locked out or double-issued.
type RefreshTokenRepository interface {
Create(ctx context.Context, t RefreshToken) error
FindByHash(ctx context.Context, hash []byte) (RefreshToken, error)
Rotate(ctx context.Context, parent, child RefreshToken) error
Revoke(ctx context.Context, t RefreshToken) error
RevokeFamily(ctx context.Context, familyID ID, at time.Time) (int, error)
}

type AddressRepository interface {
ListByUser(ctx context.Context, userID ID) ([]Address, error)
Create(ctx context.Context, a Address) error
}

// AuditEntry is a single row in the audit log. Before/After are pre-marshaled
// JSON, may be nil for actions without state changes (login, logout).
type AuditEntry struct {
Actor      ID
Action     string
EntityType string
EntityID   string
Before     []byte
After      []byte
IP         string
At         time.Time
}

type AuditLog interface {
Append(ctx context.Context, e AuditEntry) error
}

// LockoutStore tracks failed-login attempts per account, backed by Redis.
// IsLocked is consulted before verifying a password; RecordFailedAttempt
// increments and reports whether the account is now locked; Reset clears the
// counter after a successful login.
type LockoutStore interface {
IsLocked(ctx context.Context, userID ID) (bool, error)
RecordFailedAttempt(ctx context.Context, userID ID, ip string) (remaining int, locked bool, err error)
Reset(ctx context.Context, userID ID) error
}
