package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"

	migratelib "github.com/golang-migrate/migrate/v4"
	pgxdriver "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Up applies all pending migrations. Returns nil if there was nothing to do.
func Up(ctx context.Context, databaseURL string) error {
	m, err := open(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrate(m)
	if err := m.Up(); err != nil && !errors.Is(err, migratelib.ErrNoChange) {
		return fmt.Errorf("up: %w", err)
	}
	return nil
}

// Down reverts the most recently applied migration. Once 0002+ migrations
// land, this reverts only the last step — the safer default.
func Down(ctx context.Context, databaseURL string) error {
	m, err := open(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrate(m)
	if err := m.Steps(-1); err != nil && !errors.Is(err, migratelib.ErrNoChange) {
		return fmt.Errorf("down: %w", err)
	}
	return nil
}

// DownAll reverts every applied migration, one step at a time. Explicit
// Steps(-1) in a loop is used instead of m.Down() because Down() does not
// reliably reverse order when there are multiple migrations.
func DownAll(ctx context.Context, databaseURL string) error {
	m, err := open(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrate(m)
	for {
		v, _, err := m.Version()
		if errors.Is(err, migratelib.ErrNilVersion) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("version: %w", err)
		}
		if v == 0 {
			return nil
		}
		if err := m.Steps(-1); err != nil && !errors.Is(err, migratelib.ErrNoChange) {
			return fmt.Errorf("down step at version %d: %w", v, err)
		}
	}
}

// Version returns the current migration version and dirty flag. A fresh
// database with no migrations applied returns (0, false, nil).
func Version(databaseURL string) (uint, bool, error) {
	m, err := open(databaseURL)
	if err != nil {
		return 0, false, err
	}
	defer closeMigrate(m)
	v, dirty, err := m.Version()
	if errors.Is(err, migratelib.ErrNilVersion) {
		return 0, false, nil
	}
	return v, dirty, err
}

// Force sets the migration version without running any migrations. Use this
// to clear the dirty flag after a failed migration. version = -1 clears it
// entirely.
func Force(databaseURL string, version int) error {
	m, err := open(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrate(m)
	if err := m.Force(version); err != nil {
		return fmt.Errorf("force: %w", err)
	}
	return nil
}

func open(databaseURL string) (*migratelib.Migrate, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	driver, err := pgxdriver.WithInstance(db, &pgxdriver.Config{})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pgx driver: %w", err)
	}
	m, err := migratelib.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return m, nil
}

func closeMigrate(m *migratelib.Migrate) {
	_, _ = m.Close()
}
