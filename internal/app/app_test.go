package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"door/internal/config"
	"door/internal/store"
	"door/internal/sys"
	"door/internal/task"
)

func TestRuntimeAndProjectLifecycle(t *testing.T) {
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

	// Install Java 21 runtime asynchronously.
	tsk, err := svc.InstallRuntime(ctx, "java", "21")
	if err != nil {
		t.Fatalf("InstallRuntime failed: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := q.Get(ctx, tsk.ID)
		if got.Status == task.StatusSuccess {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	runtimes, err := svc.ListRuntimes(ctx)
	if err != nil || len(runtimes) != 1 {
		t.Fatalf("expected 1 installed runtime, got %d err=%v", len(runtimes), err)
	}

	// Reject shell injection in entry/args.
	if _, err := svc.CreateProject(ctx, CreateProjectInput{
		Name:  "bad-app",
		Lang:  "go",
		Entry: "server;rm -rf /",
	}); !errors.Is(err, ErrInvalidApp) {
		t.Fatalf("expected ErrInvalidApp for injection entry, got %v", err)
	}

	// Create Go project & start/stop it.
	rtID := runtimes[0].ID
	proj, err := svc.CreateProject(ctx, CreateProjectInput{
		Name:      "orders-api",
		Lang:      "java",
		Entry:     "orders.jar",
		Args:      "--server.port=8088",
		RuntimeID: &rtID,
		Port:      8088,
		Env:       map[string]string{"SPRING_PROFILES_ACTIVE": "prod"},
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "systemd", "door-app-orders-api.service")); err != nil {
		t.Fatalf("expected generated systemd unit: %v", err)
	}

	started, err := svc.ActionProject(ctx, proj.ID, "start")
	if err != nil || started.Status != "active" {
		t.Fatalf("expected active after start, got %+v err=%v", started, err)
	}

	stopped, err := svc.ActionProject(ctx, proj.ID, "stop")
	if err != nil || stopped.Status != "inactive" {
		t.Fatalf("expected inactive after stop, got %+v err=%v", stopped, err)
	}

	if err := svc.DeleteProject(ctx, proj.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
}
