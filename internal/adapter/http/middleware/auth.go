package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type userCtxKey struct{}

type Token struct {
	UserID domain.ID
	Role   domain.Role
}

// TokenVerifier abstracts the jwt package so this middleware is testable
// without crypto.
type TokenVerifier interface {
	Verify(raw string) (Token, error)
}

func Auth(v TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" {
				writeUnauthorized(w)
				return
			}
			parts := strings.SplitN(h, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
				writeUnauthorized(w)
				return
			}
			tok, err := v.Verify(parts[1])
			if err != nil {
				writeUnauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), userCtxKey{}, tok)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserFrom(ctx context.Context) (Token, bool) {
	u, ok := ctx.Value(userCtxKey{}).(Token)
	return u, ok
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
}
