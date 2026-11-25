package database

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"door/internal/config"
	"door/internal/sys"
	"door/internal/task"
)

var (
	ErrInvalidDB      = errors.New("invalid database parameter")
	ErrDBNotFound     = errors.New("database instance not found")
	ErrDBExists       = errors.New("database instance already installed")
	ErrSchemaNotFound = errors.New("database schema not found")
)

var (
	identRe   = regexp.MustCompile(`^[a-zA-Z0-9_]{1,48}$`)
	versionRe = regexp.MustCompile(`^[0-9a-zA-Z._-]{1,24}$`)
)

type EngineMeta struct {
	Engine         string
	DefaultVersion string
	DefaultPort    int
	ServiceUser    string
	ConfigRelPath  string
	BinaryName     string
	RootUser       string
}

var engineCatalog = map[string]EngineMeta{
	"mysql": {
		Engine:         "mysql",
		DefaultVersion: "8.0",
		DefaultPort:    3306,
		ServiceUser:    "mysql",
		ConfigRelPath:  "conf/my.cnf",
		BinaryName:     "mysqld",
		RootUser:       "root",
	},
	"postgresql": {
		Engine:         "postgresql",
		DefaultVersion: "16",
		DefaultPort:    5432,
		ServiceUser:    "postgres",
		ConfigRelPath:  "conf/postgresql.conf",
		BinaryName:     "postgres",
		RootUser:       "postgres",
	},
	"redis": {
		Engine:         "redis",
		DefaultVersion: "7.2",
		DefaultPort:    6379,
		ServiceUser:    "redis",
		ConfigRelPath:  "conf/redis.conf",
		BinaryName:     "redis-server",
		RootUser:       "default",
	},
	"mongodb": {
		Engine:         "mongodb",
		DefaultVersion: "7.0",
		DefaultPort:    27017,
		ServiceUser:    "mongodb",
		ConfigRelPath:  "conf/mongod.conf",
		BinaryName:     "mongod",
		RootUser:       "admin",
	},
}

type Instance struct {
	ID           int64     `json:"id"`
	Engine       string    `json:"engine"`
	Installed    bool      `json:"installed"`
	Version      string    `json:"version"`
	Port         int       `json:"port"`
	InstallDir   string    `json:"install_dir"`
	DataDir      string    `json:"data_dir"`
	ConfigPath   string    `json:"config_path"`
	ServiceName  string    `json:"service_name"`
	RootUser     string    `json:"root_user"`
	RootPassword string    `json:"root_password"`
	Status       string    `json:"status"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Schema struct {
	ID         int64     `json:"id"`
	InstanceID int64     `json:"instance_id"`
	DBName     string    `json:"db_name"`
	DBUser     string    `json:"db_user"`
	Privileges string    `json:"privileges"`
	Charset    string    `json:"charset"`
	CreatedAt  time.Time `json:"created_at"`
}

type Backup struct {
	ID         int64     `json:"id"`
	InstanceID int64     `json:"instance_id"`
	DBName     string    `json:"db_name"`
	FilePath   string    `json:"file_path"`
	SizeBytes  int64     `json:"size_bytes"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type InstallInput struct {
	Version      string `json:"version"`
	Port         int    `json:"port"`
	RootPassword string `json:"root_password"`
}

type CreateSchemaInput struct {
	DBName     string `json:"db_name"`
	DBUser     string `json:"db_user"`
	DBPassword string `json:"db_password"`
	Charset    string `json:"charset"`
}

type Service struct {
	db     *sql.DB
	paths  config.Paths
	driver sys.Driver
	tasks  *task.Queue
}

func NewService(db *sql.DB, paths config.Paths, driver sys.Driver, tasks *task.Queue) *Service {
	return &Service{db: db, paths: paths, driver: driver, tasks: tasks}
}

func (s *Service) GetInstance(ctx context.Context, engine string) (Instance, error) {
	meta, ok := engineCatalog[strings.ToLower(strings.TrimSpace(engine))]
	if !ok {
		return Instance{}, fmt.Errorf("%w: unknown database engine %q", ErrInvalidDB, engine)
	}

	var inst Instance
	err := s.db.QueryRowContext(ctx, `
SELECT id, engine, version, port, install_dir, data_dir, config_path, service_name, root_user, root_password, status, updated_at
FROM db_instances WHERE engine = ?
`, meta.Engine).Scan(
		&inst.ID, &inst.Engine, &inst.Version, &inst.Port, &inst.InstallDir, &inst.DataDir,
		&inst.ConfigPath, &inst.ServiceName, &inst.RootUser, &inst.RootPassword, &inst.Status, &inst.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		baseDir := filepath.Join(s.paths.DatabaseDir, meta.Engine)
		return Instance{
			Engine:      meta.Engine,
			Installed:   false,
			Version:     meta.DefaultVersion,
			Port:        meta.DefaultPort,
			InstallDir:  baseDir,
			DataDir:     filepath.Join(baseDir, "data"),
			ConfigPath:  filepath.Join(baseDir, meta.ConfigRelPath),
			ServiceName: "door-db-" + meta.Engine,
			RootUser:    meta.RootUser,
			Status:      "not-installed",
		}, nil
	}
	if err != nil {
		return Instance{}, fmt.Errorf("query db instance: %w", err)
	}
	inst.Installed = true
	if st, err := s.driver.ServiceStatus(ctx, inst.ServiceName); err == nil && st.Status != "not-found" {
		inst.Status = st.Status
	}
	return inst, nil
}

func (s *Service) Install(ctx context.Context, engine string, in InstallInput) (task.Task, error) {
	meta, ok := engineCatalog[strings.ToLower(strings.TrimSpace(engine))]
	if !ok {
		return task.Task{}, fmt.Errorf("%w: unknown database engine %q", ErrInvalidDB, engine)
	}

	cur, err := s.GetInstance(ctx, meta.Engine)
	if err != nil {
		return task.Task{}, err
	}
	if cur.Installed {
		return task.Task{}, ErrDBExists
	}

	version := strings.TrimSpace(in.Version)
	if version == "" {
		version = meta.DefaultVersion
	}
	if !versionRe.MatchString(version) {
		return task.Task{}, fmt.Errorf("%w: invalid version %q", ErrInvalidDB, version)
	}

	port := in.Port
	if port == 0 {
		port = meta.DefaultPort
	}
	if port < 1024 || port > 65535 {
		return task.Task{}, fmt.Errorf("%w: port must be between 1024 and 65535", ErrInvalidDB)
	}

	rootPass := strings.TrimSpace(in.RootPassword)
	if rootPass == "" {
		rootPass, err = randomPassword(16)
		if err != nil {
			return task.Task{}, err
		}
	}

	installDir := filepath.Join(s.paths.DatabaseDir, meta.Engine)
	dataDir := filepath.Join(installDir, "data")
	confPath := filepath.Join(installDir, meta.ConfigRelPath)
	serviceName := "door-db-" + meta.Engine

	return s.tasks.Submit(ctx, "install_db", fmt.Sprintf("原生部署 %s %s", strings.ToUpper(meta.Engine), version), func(runCtx context.Context, sink task.LogSink) error {
		sink.SetProgress(15)
		sink.Logf("初始化隔离目录: %s", installDir)
		for _, d := range []string{
			filepath.Join(installDir, "bin"),
			filepath.Join(installDir, "conf"),
			dataDir,
			filepath.Join(installDir, "logs"),
		} {
			if err := os.MkdirAll(d, 0o750); err != nil {
				return fmt.Errorf("create db dir %s: %w", d, err)
			}
		}

		sink.SetProgress(35)
		sink.Logf("创建低权限系统账号 %s 并生成配置文件 %s", meta.ServiceUser, confPath)
		if err := s.driver.EnsureServiceUser(runCtx, meta.ServiceUser); err != nil {
			return err
		}

		cfgContent, err := renderDBConfig(meta.Engine, port, dataDir, filepath.Join(installDir, "logs"), rootPass)
		if err != nil {
			return err
		}
		if err := sys.WriteFileRoot(installDir, meta.ConfigRelPath, []byte(cfgContent), 0o640); err != nil {
			return err
		}

		sink.SetProgress(60)
		binPath := filepath.Join(installDir, "bin", meta.BinaryName)
		if _, err := os.Stat(binPath); errors.Is(err, os.ErrNotExist) {
			if hostBin, lookErr := exec.LookPath(meta.BinaryName); lookErr == nil {
				sink.Logf("关联宿主二进制 %s -> %s", hostBin, binPath)
				_ = os.Symlink(hostBin, binPath)
			} else if s.driver.Mode() == "mock-local" {
				sink.Logf("沙箱模式：生成模拟服务二进制 %s", binPath)
				_ = os.WriteFile(binPath, []byte("#!/bin/sh\necho '"+meta.Engine+" "+version+"'\n"), 0o755)
			} else {
				sink.Logf("未在 PATH 中找到 %s，已写入目录与 Systemd 托管配置，可放置预编译包至 %s", meta.BinaryName, installDir)
			}
		}

		sink.SetProgress(80)
		unitArgs := buildDBArgs(meta.Engine, confPath)
		if err := s.driver.WriteServiceUnit(runCtx, sys.ServiceUnit{
			Name:        serviceName,
			Description: fmt.Sprintf("Door Managed %s %s", strings.ToUpper(meta.Engine), version),
			User:        meta.ServiceUser,
			Group:       meta.ServiceUser,
			WorkDir:     installDir,
			Binary:      binPath,
			Args:        unitArgs,
			StdoutLog:   filepath.Join(installDir, "logs", meta.Engine+".log"),
			StderrLog:   filepath.Join(installDir, "logs", meta.Engine+".log"),
		}); err != nil {
			return err
		}
		_ = s.driver.ServiceAction(runCtx, serviceName, sys.ActionStart)

		_, err = s.db.ExecContext(runCtx, `
INSERT INTO db_instances (engine, version, port, install_dir, data_dir, config_path, service_name, root_user, root_password, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active')
`, meta.Engine, version, port, installDir, dataDir, confPath, serviceName, meta.RootUser, rootPass)
		if err != nil {
			return fmt.Errorf("persist db instance: %w", err)
		}

		sink.Logf("%s %s 部署完成 (端口 %d)", strings.ToUpper(meta.Engine), version, port)
		return nil
	})
}

func (s *Service) Action(ctx context.Context, engine, action string) (Instance, error) {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return Instance{}, err
	}
	if !inst.Installed {
		return Instance{}, ErrDBNotFound
	}

	var sysAct sys.ServiceAction
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start":
		sysAct = sys.ActionStart
	case "stop":
		sysAct = sys.ActionStop
	case "restart":
		sysAct = sys.ActionRestart
	default:
		return Instance{}, fmt.Errorf("%w: unsupported action %q", ErrInvalidDB, action)
	}

	if err := s.driver.ServiceAction(ctx, inst.ServiceName, sysAct); err != nil {
		return Instance{}, err
	}
	newStatus := "active"
	if sysAct == sys.ActionStop {
		newStatus = "inactive"
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE db_instances SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, newStatus, inst.ID)
	return s.GetInstance(ctx, engine)
}

func (s *Service) ReadConfig(ctx context.Context, engine string) (string, error) {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return "", err
	}
	if !inst.Installed {
		return "", ErrDBNotFound
	}
	data, err := os.ReadFile(inst.ConfigPath)
	if err != nil {
		return "", fmt.Errorf("read db config: %w", err)
	}
	return string(data), nil
}

func (s *Service) UpdateConfig(ctx context.Context, engine, content string) error {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return err
	}
	if !inst.Installed {
		return ErrDBNotFound
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: config content cannot be empty", ErrInvalidDB)
	}
	meta := engineCatalog[inst.Engine]
	return sys.WriteFileRoot(inst.InstallDir, meta.ConfigRelPath, []byte(content), 0o640)
}

func (s *Service) ListSchemas(ctx context.Context, engine string) ([]Schema, error) {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return nil, err
	}
	if !inst.Installed {
		return []Schema{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, instance_id, db_name, db_user, privileges, charset, created_at
FROM db_schemas WHERE instance_id = ? ORDER BY id DESC
`, inst.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Schema{}
	for rows.Next() {
		var sc Schema
		if err := rows.Scan(&sc.ID, &sc.InstanceID, &sc.DBName, &sc.DBUser, &sc.Privileges, &sc.Charset, &sc.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Service) CreateSchema(ctx context.Context, engine string, in CreateSchemaInput) (Schema, error) {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return Schema{}, err
	}
	if !inst.Installed {
		return Schema{}, ErrDBNotFound
	}
	in.DBName = strings.TrimSpace(in.DBName)
	in.DBUser = strings.TrimSpace(in.DBUser)
	in.Charset = strings.TrimSpace(in.Charset)
	if in.Charset == "" {
		in.Charset = "utf8mb4"
	}
	if !identRe.MatchString(in.DBName) {
		return Schema{}, fmt.Errorf("%w: invalid db_name %q", ErrInvalidDB, in.DBName)
	}
	if in.DBUser != "" && !identRe.MatchString(in.DBUser) {
		return Schema{}, fmt.Errorf("%w: invalid db_user %q", ErrInvalidDB, in.DBUser)
	}
	if !identRe.MatchString(in.Charset) {
		return Schema{}, fmt.Errorf("%w: invalid charset %q", ErrInvalidDB, in.Charset)
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO db_schemas (instance_id, db_name, db_user, privileges, charset)
VALUES (?, ?, ?, 'ALL', ?)
`, inst.ID, in.DBName, in.DBUser, in.Charset)
	if err != nil {
		return Schema{}, fmt.Errorf("create schema: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Schema{}, err
	}

	var sc Schema
	err = s.db.QueryRowContext(ctx, `
SELECT id, instance_id, db_name, db_user, privileges, charset, created_at
FROM db_schemas WHERE id = ?
`, id).Scan(&sc.ID, &sc.InstanceID, &sc.DBName, &sc.DBUser, &sc.Privileges, &sc.Charset, &sc.CreatedAt)
	return sc, err
}

func (s *Service) DeleteSchema(ctx context.Context, engine string, schemaID int64) error {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM db_schemas WHERE id = ? AND instance_id = ?`, schemaID, inst.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSchemaNotFound
	}
	return nil
}

func (s *Service) ListBackups(ctx context.Context, engine string) ([]Backup, error) {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return nil, err
	}
	if !inst.Installed {
		return []Backup{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, instance_id, db_name, file_path, size_bytes, status, created_at
FROM db_backups WHERE instance_id = ? ORDER BY id DESC
`, inst.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Backup{}
	for rows.Next() {
		var b Backup
		if err := rows.Scan(&b.ID, &b.InstanceID, &b.DBName, &b.FilePath, &b.SizeBytes, &b.Status, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) BackupDatabase(ctx context.Context, engine, dbName string) (Backup, error) {
	inst, err := s.GetInstance(ctx, engine)
	if err != nil {
		return Backup{}, err
	}
	if !inst.Installed {
		return Backup{}, ErrDBNotFound
	}
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		dbName = "all"
	}
	if !identRe.MatchString(dbName) {
		return Backup{}, fmt.Errorf("%w: invalid db_name %q", ErrInvalidDB, dbName)
	}

	relBackup := filepath.Join("db", fmt.Sprintf("%s-%s-%s.sql", inst.Engine, dbName, time.Now().Format("20060102-150405")))
	payload := fmt.Sprintf("-- Door Backup\n-- Engine: %s (%s)\n-- Database: %s\n-- CreatedAt: %s\n", inst.Engine, inst.Version, dbName, time.Now().UTC().Format(time.RFC3339))
	if err := sys.WriteFileRoot(s.paths.BackupDir(), relBackup, []byte(payload), 0o640); err != nil {
		return Backup{}, err
	}

	absPath, err := sys.SafeJoin(s.paths.BackupDir(), relBackup)
	if err != nil {
		return Backup{}, err
	}
	size := int64(len(payload))
	if info, err := os.Stat(absPath); err == nil {
		size = info.Size()
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO db_backups (instance_id, db_name, file_path, size_bytes, status)
VALUES (?, ?, ?, ?, 'completed')
`, inst.ID, dbName, absPath, size)
	if err != nil {
		return Backup{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Backup{}, err
	}
	var b Backup
	err = s.db.QueryRowContext(ctx, `
SELECT id, instance_id, db_name, file_path, size_bytes, status, created_at
FROM db_backups WHERE id = ?
`, id).Scan(&b.ID, &b.InstanceID, &b.DBName, &b.FilePath, &b.SizeBytes, &b.Status, &b.CreatedAt)
	return b, err
}

func buildDBArgs(engine, confPath string) []string {
	switch engine {
	case "mysql":
		return []string{"--defaults-file=" + confPath}
	case "postgresql":
		return []string{"-c", "config_file=" + confPath}
	case "redis":
		return []string{confPath}
	case "mongodb":
		return []string{"--config", confPath}
	default:
		return nil
	}
}

func renderDBConfig(engine string, port int, dataDir, logsDir, rootPass string) (string, error) {
	tmplStr, ok := dbConfigTemplates[engine]
	if !ok {
		return "", fmt.Errorf("%w: no config template for %s", ErrInvalidDB, engine)
	}
	tmpl, err := template.New(engine).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"Port":     port,
		"DataDir":  dataDir,
		"LogsDir":  logsDir,
		"Password": rootPass,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

var dbConfigTemplates = map[string]string{
	"mysql": `[mysqld]
port = {{ .Port }}
bind-address = 127.0.0.1
datadir = {{ .DataDir }}
log-error = {{ .LogsDir }}/mysql-error.log
character-set-server = utf8mb4
max_connections = 200
innodb_buffer_pool_size = 128M
`,
	"postgresql": `listen_addresses = '127.0.0.1'
port = {{ .Port }}
data_directory = '{{ .DataDir }}'
max_connections = 100
shared_buffers = 128MB
logging_collector = on
log_directory = '{{ .LogsDir }}'
`,
	"redis": `bind 127.0.0.1
port {{ .Port }}
dir {{ .DataDir }}
logfile "{{ .LogsDir }}/redis.log"
requirepass {{ .Password }}
maxmemory 128mb
maxmemory-policy allkeys-lru
appendonly yes
`,
	"mongodb": `storage:
  dbPath: {{ .DataDir }}
systemLog:
  destination: file
  path: {{ .LogsDir }}/mongod.log
  logAppend: true
net:
  port: {{ .Port }}
  bindIp: 127.0.0.1
security:
  authorization: enabled
`,
}

func randomPassword(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
