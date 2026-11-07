// Package jwt signs and verifies EdDSA (Ed25519) access tokens with
// kid-based key rotation.
package jwt

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"
)

const (
	jtiBytes   = 16
	leeway     = 30 * time.Second
	signingAlg = "EdDSA"
)

var (
	ErrEmptySignKID   = errors.New("jwt: sign kid is empty")
	ErrInvalidPrivKey = errors.New("jwt: invalid ed25519 private key")
	ErrEmptyIssuer    = errors.New("jwt: issuer is empty")
	ErrEmptyAudience  = errors.New("jwt: audience is empty")
	ErrEmptySubject   = errors.New("jwt: subject is empty")
	ErrEmptyRole      = errors.New("jwt: role is empty")
	ErrInvalidTTL     = errors.New("jwt: ttl must be positive")
	ErrMissingKID     = errors.New("jwt: token missing kid header")
	ErrUnknownKID     = errors.New("jwt: unknown kid")
	ErrInvalidToken   = errors.New("jwt: invalid token")
)

// PreviousKey is a public key kept for verification during rotation.
type PreviousKey struct {
	KID    string
	Public ed25519.PublicKey
}

// KeySet holds the current signing key plus any previous public keys.
type KeySet struct {
	signKID   string
	signPriv  ed25519.PrivateKey
	issuer    string
	audience  string
	verifyMap map[string]ed25519.PublicKey
}

// Token is the subset of verified claims the caller cares about.
type Token struct {
	UserID    string
	Role      string
	JTI       string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type claims struct {
	Role string `json:"role"`
	golangjwt.RegisteredClaims
}

// NewKeySet builds a KeySet from the current signing key and any previous
// keys retained for verification.
func NewKeySet(signKID string, signPriv ed25519.PrivateKey, issuer, audience string, previous []PreviousKey) (*KeySet, error) {
	if signKID == "" {
		return nil, ErrEmptySignKID
	}
	if len(signPriv) != ed25519.PrivateKeySize {
		return nil, ErrInvalidPrivKey
	}
	if issuer == "" {
		return nil, ErrEmptyIssuer
	}
	if audience == "" {
		return nil, ErrEmptyAudience
	}
	pub, ok := signPriv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, ErrInvalidPrivKey
	}
	vm := make(map[string]ed25519.PublicKey, len(previous)+1)
	vm[signKID] = pub
	for _, pk := range previous {
		if pk.KID == "" || len(pk.Public) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("jwt: invalid previous key %q", pk.KID)
		}
		vm[pk.KID] = pk.Public
	}
	return &KeySet{
		signKID:   signKID,
		signPriv:  signPriv,
		issuer:    issuer,
		audience:  audience,
		verifyMap: vm,
	}, nil
}

// Sign issues a new access token. The JTI is generated internally.
func (ks *KeySet) Sign(userID, role string, ttl time.Duration) (string, error) {
	if userID == "" {
		return "", ErrEmptySubject
	}
	if role == "" {
		return "", ErrEmptyRole
	}
	if ttl <= 0 {
		return "", ErrInvalidTTL
	}
	jti, err := newJTI()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	c := claims{
		Role: role,
		RegisteredClaims: golangjwt.RegisteredClaims{
			Issuer:    ks.issuer,
			Subject:   userID,
			Audience:  []string{ks.audience},
			ExpiresAt: golangjwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  golangjwt.NewNumericDate(now),
			ID:        jti,
		},
	}
	tok := golangjwt.NewWithClaims(golangjwt.SigningMethodEdDSA, c)
	tok.Header["kid"] = ks.signKID
	return tok.SignedString(ks.signPriv)
}

// Verify parses and validates raw, returning the claims on success.
// Any failure returns ErrInvalidToken (with a descriptive message).
func (ks *KeySet) Verify(raw string) (*Token, error) {
	keyFunc := func(t *golangjwt.Token) (interface{}, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, ErrMissingKID
		}
		pub, ok := ks.verifyMap[kid]
		if !ok {
			return nil, ErrUnknownKID
		}
		return pub, nil
	}
	var c claims
	_, err := golangjwt.ParseWithClaims(raw, &c, keyFunc,
		golangjwt.WithValidMethods([]string{signingAlg}),
		golangjwt.WithIssuer(ks.issuer),
		golangjwt.WithAudience(ks.audience),
		golangjwt.WithLeeway(leeway),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	tok := &Token{
		UserID: c.Subject,
		Role:   c.Role,
		JTI:    c.ID,
	}
	if c.IssuedAt != nil {
		tok.IssuedAt = c.IssuedAt.Time
	}
	if c.ExpiresAt != nil {
		tok.ExpiresAt = c.ExpiresAt.Time
	}
	return tok, nil
}

func newJTI() (string, error) {
	b := make([]byte, jtiBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
