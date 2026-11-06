package domain

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidEmail      = errors.New("domain: invalid email")
	ErrInvalidPhone      = errors.New("domain: invalid phone number")
	ErrInvalidName       = errors.New("domain: invalid name")
	ErrInvalidRole       = errors.New("domain: invalid role")
	ErrEmptyPasswordHash = errors.New("domain: user requires a password hash")
	ErrZeroUserID        = errors.New("domain: user id is zero")
	ErrZeroUserTime      = errors.New("domain: user timestamp is zero")
)

const maxNameLen = 255

type Role string

const (
	RoleCustomer Role = "customer"
	RoleAdmin    Role = "admin"
)

func (r Role) IsValid() bool {
	switch r {
	case RoleCustomer, RoleAdmin:
		return true
	}
	return false
}

var phoneE164KE = regexp.MustCompile(`^\+254[17][0-9]{8}$`)

// User is a storefront account. Fields are unexported; the only constructors
// are NewUser and RehydrateUser, both of which validate. Mutations go through
// With* methods that return a new value rather than mutating in place.
//
// On error, every constructor and With* method returns the zero User.
type User struct {
	id              ID
	email           string
	passwordHash    string
	name            string
	role            Role
	phoneE164       string
	isActive        bool
	emailVerifiedAt time.Time
	createdAt       time.Time
	updatedAt       time.Time
}

// UserRecord is the argument shape for RehydrateUser — exported so the
// postgres adapter can build it from a row scan without a 10-arg call.
type UserRecord struct {
	ID              ID
	Email           string
	PasswordHash    string
	Name            string
	Role            Role
	PhoneE164       string
	IsActive        bool
	EmailVerifiedAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewUser creates an active, unverified user. passwordHash must be the
// output of the hashing package; the domain never sees plaintext. name may
// be empty, meaning "not set."
func NewUser(id ID, email, passwordHash, name string, role Role, phoneE164 string, at time.Time) (User, error) {
	if id.IsZero() {
		return User{}, ErrZeroUserID
	}
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if passwordHash == "" {
		return User{}, ErrEmptyPasswordHash
	}
	normalizedPhone, err := normalizePhone(phoneE164)
	if err != nil {
		return User{}, err
	}
	normalizedName, err := normalizeName(name)
	if err != nil {
		return User{}, err
	}
	if !role.IsValid() {
		return User{}, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	if at.IsZero() {
		return User{}, ErrZeroUserTime
	}
	return User{
		id:           id,
		email:        normalizedEmail,
		passwordHash: passwordHash,
		name:         normalizedName,
		role:         role,
		phoneE164:    normalizedPhone,
		isActive:     true,
		createdAt:    at,
		updatedAt:    at,
	}, nil
}

// RehydrateUser rebuilds a User from persisted state. Same validation as
// NewUser, plus the fields only the DB round-trips (isActive,
// emailVerifiedAt). Pass the zero time for EmailVerifiedAt when the DB
// column is NULL.
func RehydrateUser(r UserRecord) (User, error) {
	u, err := NewUser(r.ID, r.Email, r.PasswordHash, r.Name, r.Role, r.PhoneE164, r.CreatedAt)
	if err != nil {
		return User{}, err
	}
	u.isActive = r.IsActive
	u.emailVerifiedAt = r.EmailVerifiedAt
	if !r.UpdatedAt.IsZero() {
		u.updatedAt = r.UpdatedAt
	}
	return u, nil
}

// WithName returns a copy with a new name. Empty string clears it.
func (u User) WithName(name string, at time.Time) (User, error) {
	if at.IsZero() {
		return User{}, ErrZeroUserTime
	}
	normalized, err := normalizeName(name)
	if err != nil {
		return User{}, err
	}
	u.name = normalized
	u.updatedAt = at
	return u, nil
}

// WithPhone returns a copy with a new phone number. Empty string clears it.
func (u User) WithPhone(phoneE164 string, at time.Time) (User, error) {
	if at.IsZero() {
		return User{}, ErrZeroUserTime
	}
	normalized, err := normalizePhone(phoneE164)
	if err != nil {
		return User{}, err
	}
	u.phoneE164 = normalized
	u.updatedAt = at
	return u, nil
}

// WithPasswordHash returns a copy with a new password hash. Used by the
// rehash-on-login path.
func (u User) WithPasswordHash(hash string, at time.Time) (User, error) {
	if hash == "" {
		return User{}, ErrEmptyPasswordHash
	}
	if at.IsZero() {
		return User{}, ErrZeroUserTime
	}
	u.passwordHash = hash
	u.updatedAt = at
	return u, nil
}

func normalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || len(s) > 254 {
		return "", fmt.Errorf("%w: %q", ErrInvalidEmail, s)
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return "", fmt.Errorf("%w: %q", ErrInvalidEmail, s)
	}
	at := strings.LastIndex(s, "@")
	if at < 0 || at == len(s)-1 {
		return "", fmt.Errorf("%w: %q", ErrInvalidEmail, s)
	}
	domain := s[at+1:]
	if !strings.Contains(domain, ".") ||
		strings.HasPrefix(domain, ".") ||
		strings.HasSuffix(domain, ".") {
		return "", fmt.Errorf("%w: %q", ErrInvalidEmail, s)
	}
	return s, nil
}

func normalizePhone(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if !phoneE164KE.MatchString(s) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPhone, s)
	}
	return s, nil
}

func normalizeName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > maxNameLen {
		return "", fmt.Errorf("%w: length %d > %d", ErrInvalidName, len(s), maxNameLen)
	}
	return s, nil
}

func (u User) ID() ID                     { return u.id }
func (u User) Email() string              { return u.email }
func (u User) PasswordHash() string       { return u.passwordHash }
func (u User) Name() string               { return u.name }
func (u User) Role() Role                 { return u.role }
func (u User) PhoneE164() string          { return u.phoneE164 }
func (u User) IsActive() bool             { return u.isActive }
func (u User) IsEmailVerified() bool      { return !u.emailVerifiedAt.IsZero() }
func (u User) EmailVerifiedAt() time.Time { return u.emailVerifiedAt }
func (u User) CreatedAt() time.Time       { return u.createdAt }
func (u User) UpdatedAt() time.Time       { return u.updatedAt }