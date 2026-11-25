package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "door.db")

	db, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	tables := []string{"users", "sessions", "settings", "menus", "tasks", "audit_logs", "schema_migrations"}
	for _, tbl := range tables {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil || name != tbl {
			t.Fatalf("expected table %s to exist, err=%v", tbl, err)
		}
	}

	// Opening again should be idempotent.
	db2, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	_ = db2.Close()
}
