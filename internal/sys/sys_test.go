package sys

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeJoinAndWriteFileRoot(t *testing.T) {
	base := t.TempDir()

	p, err := SafeJoin(base, "sub/test.conf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != filepath.Join(base, "sub", "test.conf") {
		t.Fatalf("unexpected joined path: %s", p)
	}

	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "sub/../../escape"} {
		if _, err := SafeJoin(base, bad); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("expected ErrPathEscape for %q, got %v", bad, err)
		}
	}

	if err := WriteFileRoot(base, "vhost/site.conf", []byte("server {}"), 0o640); err != nil {
		t.Fatalf("WriteFileRoot failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(base, "vhost", "site.conf"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != "server {}" {
		t.Fatalf("unexpected file contents: %q", string(got))
	}

	if err := WriteFileRoot(base, "../escape.txt", []byte("bad"), 0o640); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape for traversal write, got %v", err)
	}
}

func TestRenderServiceUnitAndMockDriver(t *testing.T) {
	sandbox := t.TempDir()
	drv := NewDriver(true, sandbox)
	ctx := context.Background()

	if err := drv.EnsureServiceUser(ctx, "www"); err != nil {
		t.Fatalf("EnsureServiceUser failed: %v", err)
	}
	if err := drv.EnsureServiceUser(ctx, "bad;user"); !errors.Is(err, ErrInvalidUserName) {
		t.Fatalf("expected ErrInvalidUserName, got %v", err)
	}

	unit := ServiceUnit{
		Name:        "door-demo",
		Description: "Demo App",
		User:        "www",
		Group:       "www",
		WorkDir:     "/door/project/demo",
		Binary:      "/door/project/demo/server",
		Args:        []string{"--port", "9000"},
		Env:         map[string]string{"APP_ENV": "prod"},
	}
	if err := drv.WriteServiceUnit(ctx, unit); err != nil {
		t.Fatalf("WriteServiceUnit failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(sandbox, "systemd", "door-demo.service"))
	if err != nil {
		t.Fatalf("expected sandbox unit file: %v", err)
	}
	if !strings.Contains(string(data), "ExecStart=/door/project/demo/server --port 9000") {
		t.Fatalf("unexpected unit content:\n%s", string(data))
	}

	if err := drv.ServiceAction(ctx, "door-demo", ActionStart); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	st, err := drv.ServiceStatus(ctx, "door-demo")
	if err != nil || !st.Active {
		t.Fatalf("expected active state, got %+v err=%v", st, err)
	}
}
