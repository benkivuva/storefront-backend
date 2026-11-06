package domain

import (
"crypto/sha256"
"errors"
"fmt"
"time"
)

var (
ErrZeroRefreshTokenID       = errors.New("domain: refresh token id is zero")
ErrZeroRefreshTokenUserID   = errors.New("domain: refresh token user id is zero")
ErrZeroFamilyID             = errors.New("domain: refresh token family id is zero")
ErrEmptyRefreshTokenHash    = errors.New("domain: refresh token hash is empty")
ErrZeroRefreshTokenTime     = errors.New("domain: refresh token timestamp is zero")
ErrRefreshTokenAlreadyRev   = errors.New("domain: refresh token already revoked")
ErrRefreshTokenRotationSelf = errors.New("domain: refresh token cannot replace itself")
)

// RefreshToken is a single-use credential. On each use, a new token is
// issued with the same family_id and this token's revoked_at is set. Raw
// token bytes never live in the domain — only their hash.
type RefreshToken struct {
id         ID
userID     ID
familyID   ID
tokenHash  []byte
expiresAt  time.Time
revokedAt  time.Time
replacedBy ID
userAgent  string
ip         string
createdAt  time.Time
}

// RefreshTokenRecord is the argument shape for RehydrateRefreshToken.
type RefreshTokenRecord struct {
ID         ID
UserID     ID
FamilyID   ID
TokenHash  []byte
ExpiresAt  time.Time
RevokedAt  time.Time
ReplacedBy ID
UserAgent  string
IP         string
CreatedAt  time.Time
}

// NewRefreshToken is the first token in a family (issued at login).
func NewRefreshToken(id, userID, familyID ID, tokenHash []byte, expiresAt, createdAt time.Time, userAgent, ip string) (RefreshToken, error) {
if id.IsZero() {
return RefreshToken{}, ErrZeroRefreshTokenID
}
if userID.IsZero() {
return RefreshToken{}, ErrZeroRefreshTokenUserID
}
if familyID.IsZero() {
return RefreshToken{}, ErrZeroFamilyID
}
if len(tokenHash) != sha256.Size {
return RefreshToken{}, ErrEmptyRefreshTokenHash
}
if expiresAt.IsZero() || createdAt.IsZero() {
return RefreshToken{}, ErrZeroRefreshTokenTime
}
if !expiresAt.After(createdAt) {
return RefreshToken{}, fmt.Errorf("%w: expires_at must be after created_at", ErrZeroRefreshTokenTime)
}
return RefreshToken{
id:        id,
userID:    userID,
familyID:  familyID,
tokenHash: append([]byte(nil), tokenHash...),
expiresAt: expiresAt,
userAgent: userAgent,
ip:        ip,
createdAt: createdAt,
}, nil
}

// RehydrateRefreshToken rebuilds from persisted state.
func RehydrateRefreshToken(r RefreshTokenRecord) (RefreshToken, error) {
t, err := NewRefreshToken(r.ID, r.UserID, r.FamilyID, r.TokenHash, r.ExpiresAt, r.CreatedAt, r.UserAgent, r.IP)
if err != nil {
return RefreshToken{}, err
}
t.revokedAt = r.RevokedAt
t.replacedBy = r.ReplacedBy
return t, nil
}

// Rotate marks this token revoked and returns a new token in the same family.
// The returned token is valid from now (created at `at`) until `expiresAt`.
// This token's revoked_at is set to `at`, and its replaced_by points at the
// new token's id.
func (t RefreshToken) Rotate(newID ID, newHash []byte, expiresAt, at time.Time) (RefreshToken, RefreshToken, error) {
if t.IsRevoked() {
return RefreshToken{}, RefreshToken{}, ErrRefreshTokenAlreadyRev
}
if newID == t.id {
return RefreshToken{}, RefreshToken{}, ErrRefreshTokenRotationSelf
}
child, err := NewRefreshToken(newID, t.userID, t.familyID, newHash, expiresAt, at, t.userAgent, t.ip)
if err != nil {
return RefreshToken{}, RefreshToken{}, err
}
parent := t
parent.revokedAt = at
parent.replacedBy = newID
return parent, child, nil
}

// Revoke returns a copy with revoked_at set to at. Idempotent: already-revoked
// tokens are returned unchanged. Used both for explicit logout and for
// revoking every token in a family when reuse is detected.
func (t RefreshToken) Revoke(at time.Time) RefreshToken {
if !t.revokedAt.IsZero() {
return t
}
t.revokedAt = at
return t
}

// HashRefreshToken returns the SHA-256 hash of the raw token, suitable for
// storage. The raw value never touches the database.
func HashRefreshToken(raw []byte) []byte {
sum := sha256.Sum256(raw)
return sum[:]
}

func (t RefreshToken) ID() ID            { return t.id }
func (t RefreshToken) UserID() ID        { return t.userID }
func (t RefreshToken) FamilyID() ID      { return t.familyID }
func (t RefreshToken) ExpiresAt() time.Time { return t.expiresAt }
func (t RefreshToken) RevokedAt() time.Time { return t.revokedAt }
func (t RefreshToken) ReplacedBy() ID    { return t.replacedBy }
func (t RefreshToken) UserAgent() string { return t.userAgent }
func (t RefreshToken) IP() string        { return t.ip }
func (t RefreshToken) CreatedAt() time.Time { return t.createdAt }

func (t RefreshToken) TokenHash() []byte {
return append([]byte(nil), t.tokenHash...)
}

func (t RefreshToken) IsRevoked() bool { return !t.revokedAt.IsZero() }

func (t RefreshToken) IsExpired(now time.Time) bool { return !now.Before(t.expiresAt) }

// IsActive reports whether the token can be used: not revoked and not
// expired at `now`.
func (t RefreshToken) IsActive(now time.Time) bool {
return !t.IsRevoked() && !t.IsExpired(now)
}
