package middleware

import (
	"net/http"
	"strings"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
)

// OriginCheck rejects requests whose Origin or Referer is present and not
// in allowed. Absence of both is permitted — SameSite=Strict on the refresh
// cookie is the primary defense; this is defense-in-depth for old browsers
// and non-browser clients.
func OriginCheck(allowed []string) func(http.Handler) http.Handler {
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin := r.Header.Get("Origin"); origin != "" {
				if _, ok := set[origin]; !ok {
					dto.WriteProblem(w, http.StatusForbidden, "FORBIDDEN", "origin not allowed")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if ref := r.Header.Get("Referer"); ref != "" {
				if _, ok := set[refererOrigin(ref)]; !ok {
					dto.WriteProblem(w, http.StatusForbidden, "FORBIDDEN", "referer not allowed")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// refererOrigin returns scheme://host[:port] from a URL, or "" on malformed
// input. Empty never matches an allowed origin, so malformed input fails closed.
func refererOrigin(ref string) string {
	schemeEnd := strings.Index(ref, "://")
	if schemeEnd < 0 {
		return ""
	}
	rest := ref[schemeEnd+3:]
	if slash := strings.Index(rest, "/"); slash < 0 {
		return ref
	} else {
		return ref[:schemeEnd+3+slash]
	}
}
