package integration

import (
"context"
"database/sql"
"testing"

_ "github.com/jackc/pgx/v5/stdlib"
"github.com/testcontainers/testcontainers-go"
tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
"github.com/testcontainers/testcontainers-go/wait"

"github.com/benkivuva/storefront-backend/internal/adapter/postgres/migrate"
)

func TestMigrations_RoundTrip(t *testing.T) {
if testing.Short() {
t.Skip("integration test requires Docker")
}
ctx := context.Background()

ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
tcpostgres.WithDatabase("test"),
tcpostgres.WithUsername("test"),
tcpostgres.WithPassword("test"),
testcontainers.WithWaitStrategy(
wait.ForLog("database system is ready to accept connections").
WithOccurrence(2),
),
)
if err != nil {
t.Fatalf("start postgres: %v", err)
}
t.Cleanup(func() { _ = ctr.Terminate(ctx) })

url, err := ctr.ConnectionString(ctx, "sslmode=disable")
if err != nil {
t.Fatalf("connection string: %v", err)
}

wantTables := []string{"categories", "inventory", "products", "tax_rates", "users"}

assertTables(t, url, nil)

if err := migrate.Up(ctx, url); err != nil {
t.Fatalf("up: %v", err)
}
assertTables(t, url, wantTables)

v, dirty, err := migrate.Version(url)
if err != nil {
t.Fatalf("version: %v", err)
}
if v != 1 || dirty {
t.Fatalf("version=%d dirty=%t, want 1 false", v, dirty)
}

db, err := sql.Open("pgx", url)
if err != nil {
t.Fatalf("open: %v", err)
}
defer func() { _ = db.Close() }()

var n int
if err := db.QueryRow("SELECT count(*) FROM tax_rates").Scan(&n); err != nil {
t.Fatalf("tax_rates count: %v", err)
}
if n != 3 {
t.Fatalf("tax_rates count = %d, want 3", n)
}

if err := migrate.Down(ctx, url); err != nil {
t.Fatalf("down: %v", err)
}
assertTables(t, url, nil)

if err := migrate.Up(ctx, url); err != nil {
t.Fatalf("up again: %v", err)
}
assertTables(t, url, wantTables)
}

func assertTables(t *testing.T, url string, want []string) {
t.Helper()
db, err := sql.Open("pgx", url)
if err != nil {
t.Fatalf("open: %v", err)
}
defer func() { _ = db.Close() }()

rows, err := db.Query(`
SELECT table_name FROM information_schema.tables
WHERE table_schema = 'public' AND table_name != 'schema_migrations'
ORDER BY table_name`)
if err != nil {
t.Fatalf("query: %v", err)
}
defer func() { _ = rows.Close() }()

var got []string
for rows.Next() {
var name string
if err := rows.Scan(&name); err != nil {
t.Fatalf("scan: %v", err)
}
got = append(got, name)
}

if len(got) != len(want) {
t.Fatalf("tables = %v, want %v", got, want)
}
for i := range want {
if got[i] != want[i] {
t.Fatalf("tables = %v, want %v", got, want)
}
}
}
