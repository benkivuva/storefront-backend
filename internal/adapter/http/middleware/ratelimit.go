package middleware

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

type RateLimiter interface {
	Allow(ctx context.Context, key string, cost int) (Decision, error)
}

type KeyFunc func(r *http.Request) string

type RateLimitConfig struct {
	Limiter  RateLimiter
	Fallback RateLimiter
	Prefix   string
	Capacity int
	Key      KeyFunc
}

func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	if cfg.Limiter == nil {
		panic("middleware: RateLimit requires Limiter")
	}
	if cfg.Key == nil {
		panic("middleware: RateLimit requires Key")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := cfg.Prefix + ":" + cfg.Key(r)
			d, err := cfg.Limiter.Allow(r.Context(), key, 1)
			if err != nil {
				if cfg.Fallback != nil {
					slog.WarnContext(r.Context(),
						"rate limiter primary failed; using fallback",
						"error", err, "key", key)
					d, err = cfg.Fallback.Allow(r.Context(), key, 1)
				}
				if err != nil {
					slog.ErrorContext(r.Context(),
						"rate limiter unavailable", "error", err, "key", key)
					WriteProblem(w, http.StatusServiceUnavailable,
						"RATE_LIMITER_UNAVAILABLE",
						"rate limiter temporarily unavailable")
					return
				}
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.Capacity))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(d.Remaining))
			if !d.Allowed {
				secs := int(math.Ceil(d.RetryAfter.Seconds()))
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				WriteProblem(w, http.StatusTooManyRequests,
					"RATE_LIMITED", "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func IPKey(trusted []netip.Prefix) KeyFunc {
	return func(r *http.Request) string {
		return "ip:" + ClientPrefix(r, trusted)
	}
}

func AccountIPKey(trusted []netip.Prefix) KeyFunc {
	return func(r *http.Request) string {
		p := ClientPrefix(r, trusted)
		u, ok := UserFrom(r.Context())
		if !ok {
			return "anon:" + p
		}
		return "acct:" + u.UserID.String() + "|" + p
	}
}
