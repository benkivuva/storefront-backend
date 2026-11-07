package httpsrv

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// ReadinessCheck returns nil if a dependency is up. Sprint 1 has none; the
// Postgres and Redis checks land with their adapters.
type ReadinessCheck func() error

// Ready tracks liveness of dependencies and a "shutting down" flag. The
// shutdown path calls MarkNotReady so /readyz starts returning 503 before
// in-flight traffic drains.
type Ready struct {
	ready  atomic.Bool
	checks []ReadinessCheck
}

func NewReady(checks ...ReadinessCheck) *Ready {
	r := &Ready{checks: checks}
	r.ready.Store(true)
	return r
}

func (r *Ready) MarkNotReady() { r.ready.Store(false) }

func (r *Ready) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reason := ""
		if !r.ready.Load() {
			reason = "shutting down"
		} else {
			for _, c := range r.checks {
				if err := c(); err != nil {
					reason = err.Error()
					break
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if reason != "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": reason})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
}

// HealthHandler reports liveness only. No dependency checks; failure means
// the process is wedged and should be restarted.
func HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
}
