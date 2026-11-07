package middleware

import (
	"errors"
	"fmt"

	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/jwt"
)

// JWTVerifier adapts *jwt.KeySet to the TokenVerifier interface. It parses
// the string claims returned by jwt.Verify into the typed domain.Token the
// middleware expects. Two failure modes are adapter-specific: an unparseable
// user ID or an invalid role. Both indicate a signing bug (a caller signed
// a token with bad claims) and fail closed.
type JWTVerifier struct {
	ks *jwt.KeySet
}

func NewJWTVerifier(ks *jwt.KeySet) *JWTVerifier {
	return &JWTVerifier{ks: ks}
}

func (v *JWTVerifier) Verify(raw string) (Token, error) {
	tok, err := v.ks.Verify(raw)
	if err != nil {
		return Token{}, err
	}
	id, err := domain.ParseID(tok.UserID)
	if err != nil {
		return Token{}, fmt.Errorf("jwt adapter: invalid sub: %w", err)
	}
	role := domain.Role(tok.Role)
	if !role.IsValid() {
		return Token{}, errors.New("jwt adapter: invalid role in token")
	}
	return Token{UserID: id, Role: role}, nil
}
