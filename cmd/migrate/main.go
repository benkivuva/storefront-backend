package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres/migrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: migrate <up|down|version|force <n>>")
	}

	url := os.Getenv("MIGRATION_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("MIGRATION_DATABASE_URL required (direct connection, bypass PgBouncer)")
	}

	ctx := context.Background()

	switch os.Args[1] {
	case "up":
		return migrate.Up(ctx, url)
	case "down":
		return migrate.Down(ctx, url)
	case "version":
		v, dirty, err := migrate.Version(url)
		if err != nil {
			return err
		}
		fmt.Printf("version=%d dirty=%t\n", v, dirty)
		return nil
	case "force":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: migrate force <version>")
		}
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", os.Args[2], err)
		}
		return migrate.Force(url, n)
	default:
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}
