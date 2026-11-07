package migrate

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEveryUpHasDown(t *testing.T) {
	ups, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	if err != nil || len(ups) == 0 {
		t.Fatalf("no up migrations found: %v", err)
	}
	for _, up := range ups {
		down := strings.TrimSuffix(up, ".up.sql") + ".down.sql"
		if _, err := fs.Stat(migrationsFS, down); err != nil {
			t.Errorf("%s has no matching down migration", up)
		}
	}
}

func TestEveryDownHasUp(t *testing.T) {
	downs, err := fs.Glob(migrationsFS, "migrations/*.down.sql")
	if err != nil || len(downs) == 0 {
		t.Fatalf("no down migrations found: %v", err)
	}
	for _, down := range downs {
		up := strings.TrimSuffix(down, ".down.sql") + ".up.sql"
		if _, err := fs.Stat(migrationsFS, up); err != nil {
			t.Errorf("%s has no matching up migration", down)
		}
	}
}

func TestUpAndDownCountsMatch(t *testing.T) {
	ups, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	if err != nil {
		t.Fatalf("glob up: %v", err)
	}
	downs, err := fs.Glob(migrationsFS, "migrations/*.down.sql")
	if err != nil {
		t.Fatalf("glob down: %v", err)
	}
	if len(ups) != len(downs) {
		t.Fatalf("ups=%d downs=%d", len(ups), len(downs))
	}
}
