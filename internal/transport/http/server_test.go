package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"door/internal/app"
	"door/internal/auth"
	"door/internal/config"
	"door/internal/database"
	"door/internal/monitor"
	"door/internal/nginx"
	"door/internal/settings"
	"door/internal/store"
	"door/internal/sys"
	"door/internal/task"
	"door/internal/terminal"
)

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
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
	t.Cleanup(func() { _ = db.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	drv := sys.NewDriver(true, cfg.DevRoot)
	authSvc := auth.NewService(db, time.Hour)
	if err := authSvc.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	settingsSvc := settings.NewService(db)
	if err := settingsSvc.EnsureDefaults(ctx, cfg); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	monSvc := monitor.NewService(drv.Mode())
	q, err := task.NewQueue(db, cfg.Paths.LogsDir(), 1, logger)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	t.Cleanup(q.Close)

	webFS := fstest.MapFS{
		"index.html": {Data: []byte("<html>door</html>")},
	}

	srv := NewServer(Deps{
		Logger:   logger,
		Config:   cfg,
		Auth:     authSvc,
		Settings: settingsSvc,
		Monitor:  monSvc,
		Tasks:    q,
		Driver:   drv,
		Apps:     app.NewService(db, cfg.Paths, drv, q),
		Nginx:    nginx.NewService(db, cfg.Paths, drv, q),
		Database: database.NewService(db, cfg.Paths, drv, q),
		Terminal: terminal.NewHandler(cfg.Paths.ProjectDir, logger),
		WebFS:    webFS,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestAuthForcedPasswordChangeAndMenusAPI(t *testing.T) {
	ts := setupTestServer(t)

	// Unauthenticated settings request -> 401
	resp, err := http.Get(ts.URL + "/api/v1/settings")
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Login with default admin/12345678
	loginBody, _ := json.Marshal(map[string]string{
		"username": auth.DefaultUsername,
		"password": auth.DefaultPassword,
	})
	resp, err = http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	var loginRes struct {
		Token string    `json:"token"`
		User  auth.User `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginRes); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !loginRes.User.MustChangePassword || loginRes.Token == "" {
		t.Fatalf("unexpected login result: status=%d res=%+v", resp.StatusCode, loginRes)
	}

	// Protected API blocked until password changed -> 403
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/menus", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get menus before pw change: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 PASSWORD_CHANGE_REQUIRED, got %d", resp.StatusCode)
	}

	// Change password
	cpBody, _ := json.Marshal(map[string]string{
		"old_password": auth.DefaultPassword,
		"new_password": "StrongPassword#2026",
	})
	cpReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/change-password", bytes.NewReader(cpBody))
	cpReq.Header.Set("Authorization", "Bearer "+loginRes.Token)
	cpReq.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(cpReq)
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on change-password, got %d", resp.StatusCode)
	}

	// Menus API now succeeds
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/menus", nil)
	req2.Header.Set("Authorization", "Bearer "+loginRes.Token)
	resp, err = http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("get menus after pw change: %v", err)
	}
	var menusRes struct {
		Items []settings.MenuNode `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&menusRes); err != nil {
		t.Fatalf("decode menus: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(menusRes.Items) != 6 {
		t.Fatalf("expected 6 menu groups, got status=%d len=%d", resp.StatusCode, len(menusRes.Items))
	}

	// Create Go project over HTTP API
	projBody, _ := json.Marshal(map[string]any{
		"name":  "demo-go",
		"lang":  "go",
		"entry": "demo",
		"port":  9090,
	})
	projReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/projects", bytes.NewReader(projBody))
	projReq.Header.Set("Authorization", "Bearer "+loginRes.Token)
	projReq.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(projReq)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating project, got %d", resp.StatusCode)
	}

	// Create Nginx static site over HTTP API
	siteBody, _ := json.Marshal(map[string]any{
		"domain":      "web.example.com",
		"listen_port": 80,
		"site_type":   "static",
	})
	siteReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/nginx/sites", bytes.NewReader(siteBody))
	siteReq.Header.Set("Authorization", "Bearer "+loginRes.Token)
	siteReq.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(siteReq)
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating site, got %d", resp.StatusCode)
	}

	// Query uninstalled DB instance status
	dbReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/databases/redis", nil)
	dbReq.Header.Set("Authorization", "Bearer "+loginRes.Token)
	resp, err = http.DefaultClient.Do(dbReq)
	if err != nil {
		t.Fatalf("get db redis: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 getting redis status, got %d", resp.StatusCode)
	}
}
