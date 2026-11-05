package main

import (
"context"
"errors"
"fmt"
"log/slog"
"net/http"
"os"
"os/signal"
"syscall"
"time"

"github.com/go-chi/chi/v5"
"github.com/prometheus/client_golang/prometheus/promhttp"

httpsrv "github.com/benkivuva/storefront-backend/internal/adapter/http"
"github.com/benkivuva/storefront-backend/internal/config"
"github.com/benkivuva/storefront-backend/internal/pkg/logging"
)

func main() {
if err := run(); err != nil {
slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("fatal", "err", err)
os.Exit(1)
}
}

func run() error {
cfg, err := config.Load()
if err != nil {
return fmt.Errorf("config: %w", err)
}

logger := slog.New(logging.NewJSON(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
slog.SetDefault(logger)

publicMux := chi.NewRouter()
publicMux.NotFound(func(w http.ResponseWriter, _ *http.Request) {
http.Error(w, "not found", http.StatusNotFound)
})
publicSrv, err := httpsrv.New(cfg.HTTPAddr, publicMux, logger)
if err != nil {
return fmt.Errorf("public: %w", err)
}

ready := httpsrv.NewReady()
internalMux := chi.NewRouter()
internalMux.Method(http.MethodGet, "/healthz", httpsrv.HealthHandler())
internalMux.Method(http.MethodGet, "/readyz", ready.Handler())
internalMux.Method(http.MethodGet, "/metrics", promhttp.Handler())
internalSrv, err := httpsrv.New(cfg.InternalAddr, internalMux, logger)
if err != nil {
return fmt.Errorf("internal: %w", err)
}

errCh := make(chan error, 2)
go func() { errCh <- publicSrv.Serve() }()
go func() { errCh <- internalSrv.Serve() }()

sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

select {
case sig := <-sigCh:
logger.Info("signal received", "signal", sig.String())
case err := <-errCh:
return fmt.Errorf("server: %w", err)
}

ready.MarkNotReady()
logger.Info("marked not-ready", "propagation", cfg.ShutdownPropagation)
if cfg.ShutdownPropagation > 0 {
time.Sleep(cfg.ShutdownPropagation)
}

ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
defer cancel()

var shutdownErr error
for _, srv := range []*httpsrv.Server{publicSrv, internalSrv} {
if err := srv.Shutdown(ctx); err != nil {
shutdownErr = errors.Join(shutdownErr, err)
}
}
if shutdownErr != nil {
return fmt.Errorf("shutdown: %w", shutdownErr)
}
logger.Info("shutdown complete")
return nil
}
