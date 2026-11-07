package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyLimit_UnderLimit(t *testing.T) {
	var got string
	h := BodyLimit(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("hello")))

	if got != "hello" {
		t.Fatalf("body = %q, want hello", got)
	}
}

func TestBodyLimit_ContentLengthExceedsLimit_413(t *testing.T) {
	called := false
	h := BodyLimit(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("way too long body")))

	if called {
		t.Fatal("handler called despite Content-Length exceeding limit")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	var p map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal problem: %v", err)
	}
	if p["code"] != "BODY_TOO_LARGE" {
		t.Errorf("code = %v, want BODY_TOO_LARGE", p["code"])
	}
}

func TestBodyLimit_ExactlyAtLimit(t *testing.T) {
	var got string
	h := BodyLimit(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("0123456789")))

	if got != "0123456789" {
		t.Fatalf("body = %q, want 0123456789", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestBodyLimit_ChunkedExceeds_HandlerSeesErrBodyTooLarge(t *testing.T) {
	var readErr error
	h := BodyLimit(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequest("POST", "/",
		strings.NewReader("this body is definitely longer than ten bytes"))
	req.ContentLength = -1 // simulate chunked / unknown length
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if readErr == nil {
		t.Fatal("expected read error when chunked body exceeds limit")
	}
	if !BodyTooLarge(readErr) {
		t.Fatalf("BodyTooLarge(readErr) = false; err = %v", readErr)
	}
}

func TestBodyLimit_ChunkedUnder_NoError(t *testing.T) {
	var got string
	h := BodyLimit(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))
	req := httptest.NewRequest("POST", "/", strings.NewReader("short"))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got != "short" {
		t.Fatalf("body = %q, want short", got)
	}
}

func TestWriteBodyTooLarge(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteBodyTooLarge(rec, 1048576)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
	var p map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p["max_bytes"] != float64(1048576) {
		t.Errorf("max_bytes = %v", p["max_bytes"])
	}
}
