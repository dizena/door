package nginx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"door/internal/config"
	"door/internal/store"
	"door/internal/sys"
	"door/internal/task"
)

func TestNginxSitesAndSelfSignedSSL(t *testing.T) {
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

	// Install Nginx asynchronously.
	tsk, err := svc.InstallNginx(ctx)
	if err != nil {
		t.Fatalf("InstallNginx failed: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := q.Get(ctx, tsk.ID)
		if got.Status == task.StatusSuccess {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	info, err := svc.Info(ctx)
	if err != nil || !info.Installed {
		t.Fatalf("expected nginx installed, got %+v err=%v", info, err)
	}

	// Issue self-signed SSL cert.
	cert, err := svc.IssueSSL(ctx, IssueSSLInput{
		Domain:   "api.example.com",
		Provider: "selfsigned",
	})
	if err != nil {
		t.Fatalf("IssueSSL failed: %v", err)
	}
	if _, err := os.Stat(cert.CertPath); err != nil {
		t.Fatalf("expected cert file at %s: %v", cert.CertPath, err)
	}

	// Reject invalid domain or proxy injection.
	if _, err := svc.CreateSite(ctx, CreateSiteInput{
		Domain:    "bad domain!",
		SiteType:  "proxy",
		ProxyPass: "http://127.0.0.1:8080",
	}); !errors.Is(err, ErrInvalidNginx) {
		t.Fatalf("expected ErrInvalidNginx for bad domain, got %v", err)
	}

	// Create reverse proxy site with SSL.
	site, err := svc.CreateSite(ctx, CreateSiteInput{
		Domain:     "api.example.com",
		ListenPort: 80,
		SiteType:   "proxy",
		ProxyPass:  "http://127.0.0.1:8088",
		SSLEnabled: true,
		SSLCertID:  &cert.ID,
	})
	if err != nil {
		t.Fatalf("CreateSite failed: %v", err)
	}

	confText, err := svc.ReadSiteConfig(ctx, site.ID)
	if err != nil || !strings.Contains(confText, "proxy_pass http://127.0.0.1:8088;") || !strings.Contains(confText, "listen 443 ssl;") {
		t.Fatalf("unexpected site config:\n%s", confText)
	}

	if err := svc.DeleteSite(ctx, site.ID); err != nil {
		t.Fatalf("DeleteSite failed: %v", err)
	}
}
