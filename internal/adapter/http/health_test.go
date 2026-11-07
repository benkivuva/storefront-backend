package httpsrv

import (
	"errors"
	"net/http"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	srv := start(t, HealthHandler())
	resp, err := http.Get("http://" + srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestReady_Ready(t *testing.T) {
	srv := start(t, NewReady().Handler())
	resp, err := http.Get("http://" + srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestReady_FailingCheck(t *testing.T) {
	r := NewReady(func() error { return errors.New("db down") })
	srv := start(t, r.Handler())
	resp, err := http.Get("http://" + srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestReady_MarkNotReady(t *testing.T) {
	r := NewReady()
	r.MarkNotReady()
	srv := start(t, r.Handler())
	resp, err := http.Get("http://" + srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
