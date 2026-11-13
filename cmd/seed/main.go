package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/adapter/postgres"
	"github.com/benkivuva/storefront-backend/internal/domain"
	"github.com/benkivuva/storefront-backend/internal/pkg/clock"
	"github.com/benkivuva/storefront-backend/internal/pkg/idgen"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	filePath := flag.String("file", "", "Path to the catalog CSV file (required)")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Database URL")
	flag.Parse()

	if *filePath == "" {
		return fmt.Errorf("-file is required")
	}
	if *dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	f, err := os.Open(*filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer func() { _ = f.Close() }()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	catRepo := postgres.NewCategoryRepo(pool)
	prodRepo := postgres.NewProductRepo(pool)
	invRepo := postgres.NewInventoryRepo(pool)

	gen := idgen.New()
	clk := clock.New()

	// 1. Load existing categories to avoid duplicate inserts
	existingCats, err := catRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("list categories: %w", err)
	}
	catMap := make(map[string]domain.ID)
	for _, c := range existingCats {
		catMap[c.Slug()] = c.ID()
	}

	// 2. Parse CSV
	r := csv.NewReader(f)
	// Skip header
	if _, err := r.Read(); err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	successCount := 0
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read csv: %w", err)
		}

		sku := strings.TrimSpace(record[0])
		name := strings.TrimSpace(record[1])
		catSlug := strings.TrimSpace(record[2])
		priceMinor, _ := strconv.ParseInt(record[3], 10, 64)
		taxClass := domain.TaxClass(record[4])
		stock, _ := strconv.Atoi(record[5])

		// Find or create category
		catID, ok := catMap[catSlug]
		if !ok {
			newCat, err := domain.NewCategory(gen.NewID(), domain.ID{}, catSlug, strings.ToUpper(catSlug[:1])+catSlug[1:])
			if err != nil {
				return fmt.Errorf("invalid category %q: %w", catSlug, err)
			}
			if err := catRepo.Create(ctx, newCat); err != nil {
				return fmt.Errorf("create category %q: %w", catSlug, err)
			}
			catID = newCat.ID()
			catMap[catSlug] = catID
		}

		// Create Product
		price, err := domain.New(priceMinor, domain.KES)
		if err != nil {
			return fmt.Errorf("sku %q invalid price: %w", sku, err)
		}

		p, err := domain.NewProduct(gen.NewID(), sku, name, "", catID, price, taxClass, "", clk.Now())
		if err != nil {
			return fmt.Errorf("sku %q invalid product: %w", sku, err)
		}

		if err := prodRepo.Create(ctx, p); err != nil {
			return fmt.Errorf("insert product %q: %w", sku, err)
		}

		// Create Inventory
		inv, err := domain.NewInventory(p.ID(), stock, 10, clk.Now())
		if err != nil {
			return fmt.Errorf("sku %q invalid stock: %w", sku, err)
		}
		if err := invRepo.Create(ctx, inv); err != nil {
			return fmt.Errorf("insert inventory %q: %w", sku, err)
		}

		// Record ledger entry for initial stock
		if stock > 0 {
			movement, err := domain.NewStockMovement(gen.NewID(), p.ID(), stock, domain.ReasonSeed, domain.ID{}, domain.ID{}, clk.Now())
			if err != nil {
				return fmt.Errorf("sku %q invalid movement: %w", sku, err)
			}
			if err := invRepo.AdjustStock(ctx, movement); err != nil {
				return fmt.Errorf("insert movement %q: %w", sku, err)
			}
		}

		successCount++
	}

	fmt.Printf("Successfully seeded %d products.\n", successCount)
	return nil
}
