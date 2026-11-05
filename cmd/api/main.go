package main

import (
	"log/slog"
	"os"

	"github.com/benkivuva/storefront-backend/internal/config"
	"github.com/benkivuva/storefront-backend/internal/pkg/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("config", "err", err)
		os.Exit(1)
	}

	logger := slog.New(logging.NewJSON(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	logger.Info("storefront-backend api starting",
		"env", cfg.Env,
		"http_addr", cfg.HTTPAddr,
		"internal_addr", cfg.InternalAddr,
		"shutdown_timeout", cfg.ShutdownTimeout,
	)
}
