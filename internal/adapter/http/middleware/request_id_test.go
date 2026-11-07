package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	var got string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFrom(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got == "" {
		t.Fatal("context has no request ID")
	}
	if len(got) != 32 {
		t.Fatalf("generated ID length = %d, want 32", len(got))
	}
	if rec.Header().Get(requestIDHeader) != got {
		t.Fatalf("response header %q != context %q",
			rec.Header().Get(requestIDHeader), got)
	}
}

func TestRequestID_EchoesValidClientID(t *testing.T) {
	client := "req-abc-1234567"
	var got string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFrom(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(requestIDHeader, client)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if got != client {
		t.Fatalf("context %q, want %q", got, client)
	}
	if rec.Header().Get(requestIDHeader) != client {
		t.Fatalf("response header %q, want %q", rec.Header().Get(requestIDHeader), client)
	}
}

func TestRequestID_RejectsInvalidClientID(t *testing.T) {
	cases := map[string]string{
		"too short":          "short",
		"too long":           strings.Repeat("a", 200),
		"contains space":     "has space",
		"contains newline":   "has\nnewline",
		"contains semicolon": "a;b;c;d;e",
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var got string
			h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = RequestIDFrom(r.Context())
			}))
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set(requestIDHeader, c)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			if got == c {
				t.Fatalf("invalid client ID %q was accepted", c)
			}
			if got == "" {
				t.Fatal("no ID generated for invalid client input")
			}
		})
	}
}

func TestRequestID_HeaderSetEvenOnPanic(t *testing.T) {
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	func() {
		defer func() { _ = recover() }()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	}()

	if rec.Header().Get(requestIDHeader) == "" {
		t.Fatal("X-Request-ID missing after handler panic")
	}
}
