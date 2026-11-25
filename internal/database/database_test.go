package database

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"door/internal/config"
	"door/internal/store"
	"door/internal/sys"
	"door/internal/task"
)

func TestDatabaseInstallSchemaAndBackup(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	cfg := config.Default()
	cfg.Dev = true
	cfg.DevRoot = tmp
	if err := cfg.Resolve(); err != nil {
		t.Fatalf("resolve cfg: %v", err)
	}
	if err := cfg.Paths.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	db, err := store.Open(ctx, cfg.Paths.DBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := task.NewQueue(db, cfg.Paths.LogsDir(), 1, logger)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	defer q.Close()

	drv := sys.NewDriver(true, cfg.DevRoot)
	svc := NewService(db, cfg.Paths, drv, q)

	// Install MySQL asynchronously.
	tsk, err := svc.Install(ctx, "mysql", InstallInput{Version: "8.0", Port: 3306})
	if err != nil {
		t.Fatalf("Install mysql failed: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := q.Get(ctx, tsk.ID)
		if got.Status == task.StatusSuccess {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	inst, err := svc.GetInstance(ctx, "mysql")
	if err != nil || !inst.Installed || inst.RootPassword == "" {
		t.Fatalf("expected installed mysql instance, got %+v err=%v", inst, err)
	}

	// Reject invalid schema name.
	if _, err := svc.CreateSchema(ctx, "mysql", CreateSchemaInput{DBName: "bad;drop"}); !errors.Is(err, ErrInvalidDB) {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}

	sc, err := svc.CreateSchema(ctx, "mysql", CreateSchemaInput{DBName: "app_prod", DBUser: "app_user"})
	if err != nil || sc.DBName != "app_prod" {
		t.Fatalf("CreateSchema failed: %+v err=%v", sc, err)
	}

	bk, err := svc.BackupDatabase(ctx, "mysql", "app_prod")
	if err != nil {
		t.Fatalf("BackupDatabase failed: %v", err)
	}
	if _, err := os.Stat(bk.FilePath); err != nil {
		t.Fatalf("expected backup file at %s: %v", bk.FilePath, err)
	}

	if err := svc.DeleteSchema(ctx, "mysql", sc.ID); err != nil {
		t.Fatalf("DeleteSchema failed: %v", err)
	}
}
