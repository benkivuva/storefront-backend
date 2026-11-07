package middleware

import (
	"errors"
	"io"
	"net/http"

	"github.com/benkivuva/storefront-backend/internal/adapter/http/dto"
)

// ErrBodyTooLarge is returned by reads that exceed the limit installed by
// BodyLimit. Handlers detect it via BodyTooLarge.
var ErrBodyTooLarge = errors.New("middleware: request body too large")

// BodyLimit caps the number of bytes readable from r.Body. A request whose
// Content-Length exceeds maxBytes is rejected before the handler runs; a
// chunked request is wrapped so an over-limit read returns ErrBodyTooLarge.
// Handlers are responsible for calling WriteBodyTooLarge in response.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				WriteBodyTooLarge(w, maxBytes)
				return
			}
			r.Body = &limitedReader{rc: r.Body, remaining: maxBytes}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyTooLarge reports whether err came from a read that exceeded the limit.
func BodyTooLarge(err error) bool {
	return errors.Is(err, ErrBodyTooLarge)
}

// WriteBodyTooLarge writes an RFC 7807 problem response for a 413.
func WriteBodyTooLarge(w http.ResponseWriter, maxBytes int64) {
	dto.WriteProblemExtra(w, http.StatusRequestEntityTooLarge,
		"BODY_TOO_LARGE", "Request body too large",
		map[string]any{"max_bytes": maxBytes})
}

// limitedReader reads at most `remaining` bytes from the underlying reader.
// If more bytes are requested than remain, it truncates the result and
// returns ErrBodyTooLarge — matching the behavior of http.MaxBytesReader,
// but using a package-local error so handlers can dispatch via BodyTooLarge
// and write the problem response themselves.
type limitedReader struct {
	rc        io.ReadCloser
	remaining int64
	err       error
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if int64(len(p)) > l.remaining+1 {
		p = p[:l.remaining+1]
	}
	n, err := l.rc.Read(p)
	if int64(n) <= l.remaining {
		l.remaining -= int64(n)
		return n, err
	}
	n = int(l.remaining)
	l.remaining = 0
	l.err = ErrBodyTooLarge
	return n, l.err
}

func (l *limitedReader) Close() error {
	return l.rc.Close()
}
