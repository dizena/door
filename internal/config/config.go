package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Paths holds the four core Door directory roots and derived locations.
type Paths struct {
	ServerDir   string `json:"server_dir"`
	DatabaseDir string `json:"database_dir"`
	ProjectDir  string `json:"project_dir"`
	NginxDir    string `json:"nginx_dir"`
}

// Config holds runtime configuration for the Door panel server.
type Config struct {
	Addr          string
	Dev           bool
	DevRoot       string
	LogLevel      string
	MemoryLimitMB int64
	SessionTTL    time.Duration
	Paths         Paths
}

// Default returns the production-aligned defaults with port 12345 and /door/* paths.
func Default() Config {
	return Config{
		Addr:          ":12345",
		Dev:           runtime.GOOS != "linux",
		DevRoot:       ".door-dev",
		LogLevel:      "info",
		MemoryLimitMB: 128,
		SessionTTL:    24 * time.Hour,
		Paths: Paths{
			ServerDir:   "/door/server",
			DatabaseDir: "/door/database",
			ProjectDir:  "/door/project",
			NginxDir:    "/door/nginx",
		},
	}
}

// ApplyEnv overrides configuration values from DOOR_* environment variables.
func (c *Config) ApplyEnv() error {
	if v := os.Getenv("DOOR_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("DOOR_DEV"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid DOOR_DEV value %q: %w", v, err)
		}
		c.Dev = b
	}
	if v := os.Getenv("DOOR_DEV_ROOT"); v != "" {
		c.DevRoot = v
	}
	if v := os.Getenv("DOOR_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("DOOR_MEMORY_LIMIT_MB"); v != "" {
		mb, err := strconv.ParseInt(v, 10, 64)
		if err != nil || mb < 0 {
			return fmt.Errorf("invalid DOOR_MEMORY_LIMIT_MB value %q", v)
		}
		c.MemoryLimitMB = mb
	}
	if v := os.Getenv("DOOR_SERVER_DIR"); v != "" {
		c.Paths.ServerDir = v
	}
	if v := os.Getenv("DOOR_DATABASE_DIR"); v != "" {
		c.Paths.DatabaseDir = v
	}
	if v := os.Getenv("DOOR_PROJECT_DIR"); v != "" {
		c.Paths.ProjectDir = v
	}
	if v := os.Getenv("DOOR_NGINX_DIR"); v != "" {
		c.Paths.NginxDir = v
	}
	return nil
}

// Resolve finalizes paths (remapping /door/* into DevRoot when Dev is true) and cleans them.
func (c *Config) Resolve() error {
	if c.Dev {
		root := c.DevRoot
		if root == "" {
			root = ".door-dev"
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("resolve dev root: %w", err)
		}
		c.DevRoot = absRoot
		if c.Paths.ServerDir == "/door/server" {
			c.Paths.ServerDir = filepath.Join(absRoot, "server")
		}
		if c.Paths.DatabaseDir == "/door/database" {
			c.Paths.DatabaseDir = filepath.Join(absRoot, "database")
		}
		if c.Paths.ProjectDir == "/door/project" {
			c.Paths.ProjectDir = filepath.Join(absRoot, "project")
		}
		if c.Paths.NginxDir == "/door/nginx" {
			c.Paths.NginxDir = filepath.Join(absRoot, "nginx")
		}
	}

	c.Paths.ServerDir = filepath.Clean(c.Paths.ServerDir)
	c.Paths.DatabaseDir = filepath.Clean(c.Paths.DatabaseDir)
	c.Paths.ProjectDir = filepath.Clean(c.Paths.ProjectDir)
	c.Paths.NginxDir = filepath.Clean(c.Paths.NginxDir)

	if c.Addr == "" {
		c.Addr = ":12345"
	}
	return nil
}

// DataDir returns the SQLite and panel runtime data directory.
func (p Paths) DataDir() string { return filepath.Join(p.ServerDir, "data") }

// DBPath returns the SQLite database file path.
func (p Paths) DBPath() string { return filepath.Join(p.DataDir(), "door.db") }

// BackupDir returns the archive backup directory.
func (p Paths) BackupDir() string { return filepath.Join(p.ServerDir, "backup") }

// JavaDir returns the multi-version JDK runtime root.
func (p Paths) JavaDir() string { return filepath.Join(p.ServerDir, "java") }

// PythonDir returns the managed Python runtime root.
func (p Paths) PythonDir() string { return filepath.Join(p.ServerDir, "python") }

// LogsDir returns the panel and managed app logs directory.
func (p Paths) LogsDir() string { return filepath.Join(p.ServerDir, "logs") }

// NginxVHostDir returns the virtual host configuration directory.
func (p Paths) NginxVHostDir() string { return filepath.Join(p.NginxDir, "conf", "vhost") }

// NginxSSLDir returns the SSL certificate storage directory.
func (p Paths) NginxSSLDir() string { return filepath.Join(p.NginxDir, "ssl") }

// NginxLogsDir returns the Nginx access and error logs directory.
func (p Paths) NginxLogsDir() string { return filepath.Join(p.NginxDir, "logs") }

// EnsureDirs creates all standard Door directories if they do not exist.
func (p Paths) EnsureDirs() error {
	dirs := []string{
		p.ServerDir,
		p.DataDir(),
		p.BackupDir(),
		p.JavaDir(),
		p.PythonDir(),
		p.LogsDir(),
		p.DatabaseDir,
		filepath.Join(p.DatabaseDir, "mysql"),
		filepath.Join(p.DatabaseDir, "postgresql"),
		filepath.Join(p.DatabaseDir, "redis"),
		filepath.Join(p.DatabaseDir, "mongodb"),
		p.ProjectDir,
		p.NginxDir,
		filepath.Join(p.NginxDir, "sbin"),
		p.NginxVHostDir(),
		p.NginxSSLDir(),
		p.NginxLogsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}
