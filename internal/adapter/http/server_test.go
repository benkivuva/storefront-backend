package httpsrv

import (
"context"
"io"
"log/slog"
"net/http"
"testing"
"time"
)

func testLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func start(t *testing.T, h http.Handler) *Server {
t.Helper()
srv, err := New("127.0.0.1:0", h, testLogger())
if err != nil {
t.Fatalf("New: %v", err)
}
go func() { _ = srv.Serve() }()
t.Cleanup(func() {
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
_ = srv.Shutdown(ctx)
})
return srv
}

func TestServer_Serves(t *testing.T) {
srv := start(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
_, _ = w.Write([]byte("hello"))
}))
resp, err := http.Get("http://" + srv.Addr())
if err != nil {
t.Fatal(err)
}
defer func() { _ = resp.Body.Close() }()
body, _ := io.ReadAll(resp.Body)
if string(body) != "hello" {
t.Fatalf("body = %q", body)
}
}

func TestServer_ShutdownStopsServing(t *testing.T) {
srv := start(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
url := "http://" + srv.Addr()
if r, err := http.Get(url); err != nil {
t.Fatal(err)
} else {
_ = r.Body.Close()
}
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
if err := srv.Shutdown(ctx); err != nil {
t.Fatal(err)
}
if _, err := http.Get(url); err == nil {
t.Fatal("expected error after shutdown")
}
}
