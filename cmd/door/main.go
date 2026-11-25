package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
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
	httptransport "door/internal/transport/http"
	"door/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "door: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}

	cfg := config.Default()
	if err := cfg.ApplyEnv(); err != nil {
		return err
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address (default :12345)")
	fs.BoolVar(&cfg.Dev, "dev", cfg.Dev, "Enable local sandbox mode for development")
	fs.StringVar(&cfg.DevRoot, "dev-root", cfg.DevRoot, "Sandbox root directory when --dev is enabled")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "Log level (debug, info, warn, error)")
	fs.Int64Var(&cfg.MemoryLimitMB, "memory-limit-mb", cfg.MemoryLimitMB, "Soft Go heap limit in MiB (0 disables)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if err := cfg.Resolve(); err != nil {
		return err
	}

	if cfg.MemoryLimitMB > 0 {
		debug.SetMemoryLimit(cfg.MemoryLimitMB * 1024 * 1024)
	}

	logger := newLogger(cfg.LogLevel)

	if err := cfg.Paths.EnsureDirs(); err != nil {
		return fmt.Errorf("initialize directories: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.Paths.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	authSvc := auth.NewService(db, cfg.SessionTTL)
	if err := authSvc.EnsureDefaultAdmin(ctx); err != nil {
		return fmt.Errorf("seed default admin: %w", err)
	}

	settingsSvc := settings.NewService(db)
	if err := settingsSvc.EnsureDefaults(ctx, cfg); err != nil {
		return fmt.Errorf("seed default settings: %w", err)
	}

	ps, err := settingsSvc.GetPanelSettings(ctx)
	if err == nil && ps.ListenAddr != "" && !flagPassed(fs, "addr") {
		cfg.Addr = ps.ListenAddr
	}
	if err == nil && ps.MemoryLimitMB > 0 && !flagPassed(fs, "memory-limit-mb") {
		debug.SetMemoryLimit(ps.MemoryLimitMB * 1024 * 1024)
	}

	drv := sys.NewDriver(cfg.Dev, cfg.DevRoot)
	monSvc := monitor.NewService(drv.Mode())
	taskQueue, err := task.NewQueue(db, cfg.Paths.LogsDir(), 2, logger)
	if err != nil {
		return fmt.Errorf("start task queue: %w", err)
	}
	defer taskQueue.Close()

	appSvc := app.NewService(db, cfg.Paths, drv, taskQueue)
	nginxSvc := nginx.NewService(db, cfg.Paths, drv, taskQueue)
	dbSvc := database.NewService(db, cfg.Paths, drv, taskQueue)
	termHandler := terminal.NewHandler(cfg.Paths.ProjectDir, logger)

	webFS, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("load embedded web assets: %w", err)
	}

	apiSrv := httptransport.NewServer(httptransport.Deps{
		Logger:   logger,
		Config:   cfg,
		Auth:     authSvc,
		Settings: settingsSvc,
		Monitor:  monSvc,
		Tasks:    taskQueue,
		Driver:   drv,
		Apps:     appSvc,
		Nginx:    nginxSvc,
		Database: dbSvc,
		Terminal: termHandler,
		WebFS:    webFS,
	})

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("door panel listening",
			"addr", cfg.Addr,
			"mode", drv.Mode(),
			"dev", cfg.Dev,
			"server_dir", cfg.Paths.ServerDir,
			"database_dir", cfg.Paths.DatabaseDir,
			"project_dir", cfg.Paths.ProjectDir,
			"nginx_dir", cfg.Paths.NginxDir,
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down door panel")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func flagPassed(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
