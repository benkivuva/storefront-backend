package config

import (
"errors"
"log/slog"
"testing"
"time"
)

func TestLoad_Defaults(t *testing.T) {
// Nothing in the environment; every field falls back to envDefault.
c, err := Load()
if err != nil {
t.Fatalf("Load(): %v", err)
}
if c.Env != EnvLocal {
t.Errorf("Env = %q, want %q", c.Env, EnvLocal)
}
if c.HTTPAddr != ":8080" || c.InternalAddr != ":9090" {
t.Errorf("addrs = %q / %q", c.HTTPAddr, c.InternalAddr)
}
if c.LogLevel != slog.LevelInfo {
t.Errorf("LogLevel = %v, want info", c.LogLevel)
}
if c.ShutdownTimeout != 30*time.Second {
t.Errorf("ShutdownTimeout = %s, want 30s", c.ShutdownTimeout)
}
}

func TestLoad_Overrides(t *testing.T) {
t.Setenv("ENV", "staging")
t.Setenv("HTTP_ADDR", ":18080")
t.Setenv("INTERNAL_ADDR", ":19090")
t.Setenv("LOG_LEVEL", "debug")
t.Setenv("SHUTDOWN_TIMEOUT", "5s")

c, err := Load()
if err != nil {
t.Fatalf("Load(): %v", err)
}
if c.Env != EnvStaging || c.HTTPAddr != ":18080" || c.InternalAddr != ":19090" {
t.Errorf("fields = %+v", c)
}
if c.LogLevel != slog.LevelDebug {
t.Errorf("LogLevel = %v, want debug", c.LogLevel)
}
if c.ShutdownTimeout != 5*time.Second {
t.Errorf("ShutdownTimeout = %s", c.ShutdownTimeout)
}
}

func TestLoad_Invalid(t *testing.T) {
cases := []struct {
name string
env  map[string]string
}{
{"unknown ENV", map[string]string{"ENV": "prod"}},
{"bogus log level", map[string]string{"LOG_LEVEL": "trace"}},
{"zero shutdown", map[string]string{"SHUTDOWN_TIMEOUT": "0s"}},
{"negative shutdown", map[string]string{"SHUTDOWN_TIMEOUT": "-1s"}},
{"same addrs", map[string]string{"HTTP_ADDR": ":8080", "INTERNAL_ADDR": ":8080"}},
}
for _, tc := range cases {
t.Run(tc.name, func(t *testing.T) {
for k, v := range tc.env {
t.Setenv(k, v)
}
_, err := Load()
if err == nil {
t.Fatalf("expected error")
}
if !errors.Is(err, err) { // ensures err is non-nil, no panic
t.Fatalf("unexpected")
}
})
}
}

// Empty environment variables fall through to envDefault, per caarlos0/env's
// contract. An operator who sets HTTP_ADDR="" gets ":8080", not an error.
// This is not a bug; it is documented behavior we rely on.
func TestLoad_EmptyEnvUsesDefault(t *testing.T) {
t.Setenv("HTTP_ADDR", "")
c, err := Load()
if err != nil {
t.Fatalf("Load(): %v", err)
}
if c.HTTPAddr != ":8080" {
t.Fatalf("HTTP_ADDR = %q, want :8080 (default)", c.HTTPAddr)
}
}

func TestLoad_ShutdownPropagation(t *testing.T) {
t.Setenv("SHUTDOWN_PROPAGATION", "0s")
c, err := Load()
if err != nil {
t.Fatalf("Load(): %v", err)
}
if c.ShutdownPropagation != 0 {
t.Fatalf("ShutdownPropagation = %s, want 0s", c.ShutdownPropagation)
}

t.Setenv("SHUTDOWN_PROPAGATION", "-1s")
if _, err := Load(); err == nil {
t.Fatalf("expected error for negative propagation")
}
}
