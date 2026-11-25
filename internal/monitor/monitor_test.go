package monitor

import (
	"context"
	"testing"

	"door/internal/config"
)

func TestSnapshot(t *testing.T) {
	cfg := config.Default()
	cfg.Dev = true
	cfg.DevRoot = t.TempDir()
	if err := cfg.Resolve(); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if err := cfg.Paths.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs failed: %v", err)
	}

	svc := NewService("mock-local")
	snap := svc.Snapshot(context.Background(), cfg.Paths)
	if snap.CPU.Cores <= 0 {
		t.Fatalf("expected positive CPU cores, got %d", snap.CPU.Cores)
	}
	if len(snap.Directories) != 4 {
		t.Fatalf("expected 4 core directory statuses, got %d", len(snap.Directories))
	}
	for _, d := range snap.Directories {
		if !d.Exists {
			t.Fatalf("expected directory %s (%s) to exist", d.Key, d.Path)
		}
	}
}
