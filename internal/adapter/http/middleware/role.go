package middleware

import (
	"net/http"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

func RequireRole(roles ...domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFrom(r.Context())
			if !ok {
				dto.WriteProblem(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
				return
			}
			if len(roles) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			for _, want := range roles {
				if u.Role == want {
					next.ServeHTTP(w, r)
					return
				}
			}
			dto.WriteProblem(w, http.StatusForbidden, "FORBIDDEN", "insufficient role")
		})
	}
}
