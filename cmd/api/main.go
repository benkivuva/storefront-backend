package main

import (
"log/slog"
"os"
)

func main() {
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
logger.Info("storefront-backend api: scaffold only; HTTP server and graceful shutdown land later in Sprint 1")
}
