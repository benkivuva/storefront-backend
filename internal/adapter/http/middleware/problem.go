package middleware

import (
	"encoding/json"
	"net/http"
)

// WriteProblem emits an RFC 7807 document. Phase 4 will replace this with
// typed error mapping; today it exists so Auth and RequireRole share a body.
func WriteProblem(w http.ResponseWriter, status int, code, title string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  title,
		"status": status,
		"code":   code,
	})
}
