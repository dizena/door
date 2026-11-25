package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultAndResolveDev(t *testing.T) {
	cfg := Default()
	if cfg.Addr != ":12345" {
		t.Fatalf("expected default addr :12345, got %q", cfg.Addr)
	}

	tmp := t.TempDir()
	cfg.Dev = true
	cfg.DevRoot = tmp
	if err := cfg.Resolve(); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if cfg.Paths.ServerDir != filepath.Join(tmp, "server") {
		t.Fatalf("unexpected server dir: %s", cfg.Paths.ServerDir)
	}
	if cfg.Paths.DatabaseDir != filepath.Join(tmp, "database") {
		t.Fatalf("unexpected database dir: %s", cfg.Paths.DatabaseDir)
	}
	if cfg.Paths.ProjectDir != filepath.Join(tmp, "project") {
		t.Fatalf("unexpected project dir: %s", cfg.Paths.ProjectDir)
	}
	if cfg.Paths.NginxDir != filepath.Join(tmp, "nginx") {
		t.Fatalf("unexpected nginx dir: %s", cfg.Paths.NginxDir)
	}

	if err := cfg.Paths.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs failed: %v", err)
	}
}

func TestResolveProductionPaths(t *testing.T) {
	cfg := Default()
	cfg.Dev = false
	if err := cfg.Resolve(); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if cfg.Paths.ServerDir != "/door/server" ||
		cfg.Paths.DatabaseDir != "/door/database" ||
		cfg.Paths.ProjectDir != "/door/project" ||
		cfg.Paths.NginxDir != "/door/nginx" {
		t.Fatalf("unexpected production paths: %+v", cfg.Paths)
	}
}
