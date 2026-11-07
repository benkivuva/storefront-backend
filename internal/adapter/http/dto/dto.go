package dto

import (
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/service"
)

// User is the wire representation of a user. PasswordHash, IsActive, and
// EmailVerifiedAt are intentionally absent — the frontend needs none of them
// and their presence would invite misuse.
type User struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	Name            string    `json:"name,omitempty"`
	Role            string    `json:"role"`
	Phone           string    `json:"phone,omitempty"`
	IsEmailVerified bool      `json:"is_email_verified"`
	CreatedAt       time.Time `json:"created_at"`
}

func FromUser(u domain.User) User {
	return User{
		ID:              u.ID().String(),
		Email:           u.Email(),
		Name:            u.Name(),
		Role:            string(u.Role()),
		Phone:           u.PhoneE164(),
		IsEmailVerified: u.IsEmailVerified(),
		CreatedAt:       u.CreatedAt(),
	}
}

type Address struct {
	ID        string    `json:"id"`
	Recipient string    `json:"recipient"`
	Phone     string    `json:"phone"`
	Line1     string    `json:"line1"`
	Line2     string    `json:"line2,omitempty"`
	Town      string    `json:"town"`
	County    string    `json:"county,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func FromAddress(a domain.Address) Address {
	return Address{
		ID:        a.ID().String(),
		Recipient: a.Recipient(),
		Phone:     a.PhoneE164(),
		Line1:     a.Line1(),
		Line2:     a.Line2(),
		Town:      a.Town(),
		County:    a.County(),
		Notes:     a.Notes(),
		CreatedAt: a.CreatedAt(),
	}
}

func FromAddresses(as []domain.Address) []Address {
	out := make([]Address, len(as))
	for i, a := range as {
		out[i] = FromAddress(a)
	}
	return out
}

// Session is the wire representation of an active session. Token hashes are
// never exposed — the frontend only needs enough to identify the device.
type Session struct {
	ID        string    `json:"id"`
	UserAgent string    `json:"user_agent,omitempty"`
	IP        string    `json:"ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func FromSession(s service.SessionInfo) Session {
	return Session{
		ID:        s.ID.String(),
		UserAgent: s.UserAgent,
		IP:        s.IP,
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
	}
}

func FromSessions(ss []service.SessionInfo) []Session {
	out := make([]Session, len(ss))
	for i, s := range ss {
		out[i] = FromSession(s)
	}
	return out
}
