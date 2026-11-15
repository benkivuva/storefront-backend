package middleware

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
	"github.com/benkivuva/storefront-backend/internal/domain"
)

type responseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

func Idempotency(repo domain.IdempotencyRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost && r.Method != http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			u, ok := UserFrom(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				dto.WriteProblem(w, http.StatusInternalServerError, "INTERNAL", "failed to read body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			hash := sha256.Sum256(bodyBytes)

			rec, err := repo.Get(r.Context(), u.UserID, key)
			if err == nil {
				if !bytes.Equal(rec.Hash, hash[:]) {
					dto.WriteProblem(w, http.StatusUnprocessableEntity, "IDEMPOTENCY_MISMATCH", "key reused with different payload")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(rec.Status)
				_, _ = w.Write(rec.Body)
				return
			}
			if !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrConflict) {
				dto.WriteProblem(w, http.StatusInternalServerError, "INTERNAL", "idempotency error")
				return
			}
			if errors.Is(err, domain.ErrConflict) {
				dto.WriteProblem(w, http.StatusConflict, "REQUEST_IN_PROGRESS", "a request with this key is currently in progress")
				return
			}

			if err := repo.Lock(r.Context(), u.UserID, key, hash[:]); err != nil {
				if errors.Is(err, domain.ErrConflict) {
					dto.WriteProblem(w, http.StatusConflict, "REQUEST_IN_PROGRESS", "a request with this key is currently in progress")
					return
				}
				dto.WriteProblem(w, http.StatusInternalServerError, "INTERNAL", "lock failed")
				return
			}

			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			if rw.status >= 200 && rw.status < 300 {
				_ = repo.Save(r.Context(), u.UserID, key, rw.status, rw.body.Bytes())
			} else {
				_ = repo.Delete(r.Context(), u.UserID, key)
			}
		})
	}
}
