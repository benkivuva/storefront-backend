package middleware

import "net/http"

// SecurityHeadersConfig controls environment-dependent header values.
type SecurityHeadersConfig struct {
	// Prod enables HSTS. Leave false in local/staging to avoid poisoning
	// browser caches with HSTS for a host that isn't HTTPS yet.
	Prod bool
}

// SecurityHeaders sets the headers required by §13.2. Applied to every
// response; a downstream handler may override individual headers when it
// has a specific reason to.
func SecurityHeaders(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if cfg.Prod {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy",
				"default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
			next.ServeHTTP(w, r)
		})
	}
}

// NoStore sets Cache-Control: no-store. Applied to auth, /me, and payment
// routes so shared caches and browser back-buttons never serve stale
// personal data.
func NoStore() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		})
	}
}
