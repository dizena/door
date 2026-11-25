package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"door/internal/config"
	"door/internal/sys"
	"door/internal/task"
)

var (
	ErrInvalidApp    = errors.New("invalid application configuration")
	ErrNotFound      = errors.New("application or runtime not found")
	ErrAlreadyExists = errors.New("application or runtime already exists")
)

var (
	projectNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,48}$`)
	versionRe     = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,24}$`)
	shellMetaRe   = regexp.MustCompile(`[;&|$` + "`" + `<>\\!\n\r]`)
)

type Runtime struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	Version    string    `json:"version"`
	InstallDir string    `json:"install_dir"`
	BinaryPath string    `json:"binary_path"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type Project struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Lang        string            `json:"lang"`
	RootDir     string            `json:"root_dir"`
	Entry       string            `json:"entry"`
	Args        string            `json:"args"`
	RuntimeID   *int64            `json:"runtime_id"`
	Port        int               `json:"port"`
	RunUser     string            `json:"run_user"`
	Env         map[string]string `json:"env"`
	ServiceName string            `json:"service_name"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type CreateProjectInput struct {
	Name      string            `json:"name"`
	Lang      string            `json:"lang"`
	Entry     string            `json:"entry"`
	Args      string            `json:"args"`
	RuntimeID *int64            `json:"runtime_id"`
	Port      int               `json:"port"`
	RunUser   string            `json:"run_user"`
	Env       map[string]string `json:"env"`
}

// Service manages multi-language runtimes (Java/Python/Go) and Systemd-hosted projects.
type Service struct {
	db     *sql.DB
	paths  config.Paths
	driver sys.Driver
	tasks  *task.Queue
}

func NewService(db *sql.DB, paths config.Paths, driver sys.Driver, tasks *task.Queue) *Service {
	return &Service{db: db, paths: paths, driver: driver, tasks: tasks}
}

func (s *Service) ListRuntimes(ctx context.Context) ([]Runtime, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, kind, version, install_dir, binary_path, status, created_at
FROM runtimes ORDER BY kind ASC, version DESC
`)
	if err != nil {
		return nil, fmt.Errorf("query runtimes: %w", err)
	}
	defer rows.Close()

	out := []Runtime{}
	for rows.Next() {
		var r Runtime
		if err := rows.Scan(&r.ID, &r.Kind, &r.Version, &r.InstallDir, &r.BinaryPath, &r.Status, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan runtime: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InstallRuntime asynchronously provisions a Java, Python, or Go runtime under /door/server.
func (s *Service) InstallRuntime(ctx context.Context, kind, version string) (task.Task, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	version = strings.TrimSpace(version)
	switch kind {
	case "java", "python", "go":
	default:
		return task.Task{}, fmt.Errorf("%w: unsupported runtime kind %q", ErrInvalidApp, kind)
	}
	if !versionRe.MatchString(version) || strings.Contains(version, "..") {
		return task.Task{}, fmt.Errorf("%w: invalid version %q", ErrInvalidApp, version)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM runtimes WHERE kind = ? AND version = ?`, kind, version).Scan(&count); err != nil {
		return task.Task{}, err
	}
	if count > 0 {
		return task.Task{}, ErrAlreadyExists
	}

	baseRoot := s.paths.ServerDir
	switch kind {
	case "java":
		baseRoot = s.paths.JavaDir()
	case "python":
		baseRoot = s.paths.PythonDir()
	case "go":
		baseRoot = filepath.Join(s.paths.ServerDir, "go")
	}

	installDir, err := sys.SafeJoin(baseRoot, kind+"-"+version)
	if err != nil {
		return task.Task{}, err
	}

	return s.tasks.Submit(ctx, "install_runtime", fmt.Sprintf("安装 %s %s 运行环境", strings.ToUpper(kind), version), func(runCtx context.Context, sink task.LogSink) error {
		sink.SetProgress(15)
		sink.Logf("准备隔离安装目录: %s", installDir)
		if err := os.MkdirAll(filepath.Join(installDir, "bin"), 0o750); err != nil {
			return fmt.Errorf("create runtime dir: %w", err)
		}

		binName := kind
		if kind == "python" {
			binName = "python3"
		}
		binPath := filepath.Join(installDir, "bin", binName)

		sink.SetProgress(50)
		if s.driver.Mode() == "mock-local" {
			sink.Logf("当前处于本地沙箱模式 (%s)，生成本地托管运行时链接", s.driver.Mode())
			if hostBin, err := exec.LookPath(binName); err == nil {
				_ = os.Remove(binPath)
				if err := os.Symlink(hostBin, binPath); err != nil {
					if writeErr := os.WriteFile(binPath, []byte("#!/bin/sh\necho '"+kind+" "+version+"'\n"), 0o755); writeErr != nil {
						return writeErr
					}
				}
			} else {
				if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho '"+kind+" "+version+"'\n"), 0o755); err != nil {
					return err
				}
			}
		} else {
			sink.Logf("检测宿主环境或准备 %s-%s 运行时二进制", kind, version)
			if hostBin, err := exec.LookPath(binName); err == nil {
				_ = os.Remove(binPath)
				if err := os.Symlink(hostBin, binPath); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("binary %s not found on host; place runtime tarball under %s", binName, installDir)
			}
		}

		sink.SetProgress(85)
		_, err := s.db.ExecContext(runCtx, `
INSERT INTO runtimes (kind, version, install_dir, binary_path, status)
VALUES (?, ?, ?, ?, 'installed')
`, kind, version, installDir, binPath)
		if err != nil {
			return fmt.Errorf("persist runtime: %w", err)
		}
		sink.Logf("%s %s 运行环境已就绪: %s", kind, version, binPath)
		return nil
	})
}

func (s *Service) DeleteRuntime(ctx context.Context, id int64) error {
	var installDir string
	err := s.db.QueryRowContext(ctx, `SELECT install_dir FROM runtimes WHERE id = ?`, id).Scan(&installDir)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM runtimes WHERE id = ?`, id); err != nil {
		return err
	}
	_ = os.RemoveAll(installDir)
	return nil
}

func (s *Service) ListProjects(ctx context.Context, langFilter string) ([]Project, error) {
	query := `
SELECT id, name, lang, root_dir, entry, args, runtime_id, port, run_user, env_json, service_name, status, created_at, updated_at
FROM projects`
	var args []any
	if langFilter != "" {
		query += ` WHERE lang = ?`
		args = append(args, strings.ToLower(strings.TrimSpace(langFilter)))
	}
	query += ` ORDER BY id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		if st, err := s.driver.ServiceStatus(ctx, p.ServiceName); err == nil && st.Status != "not-found" {
			p.Status = st.Status
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) GetProject(ctx context.Context, id int64) (Project, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, lang, root_dir, entry, args, runtime_id, port, run_user, env_json, service_name, status, created_at, updated_at
FROM projects WHERE id = ?
`, id)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	if st, err := s.driver.ServiceStatus(ctx, p.ServiceName); err == nil && st.Status != "not-found" {
		p.Status = st.Status
	}
	return p, nil
}

func (s *Service) CreateProject(ctx context.Context, in CreateProjectInput) (Project, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Lang = strings.ToLower(strings.TrimSpace(in.Lang))
	in.Entry = strings.TrimSpace(in.Entry)
	in.Args = strings.TrimSpace(in.Args)
	in.RunUser = strings.TrimSpace(in.RunUser)
	if in.RunUser == "" {
		in.RunUser = "www"
	}

	if !projectNameRe.MatchString(in.Name) || strings.Contains(in.Name, "..") {
		return Project{}, fmt.Errorf("%w: invalid project name %q", ErrInvalidApp, in.Name)
	}
	switch in.Lang {
	case "go", "java", "python":
	default:
		return Project{}, fmt.Errorf("%w: lang must be go, java, or python", ErrInvalidApp)
	}
	if in.Entry == "" || strings.Contains(in.Entry, "..") || shellMetaRe.MatchString(in.Entry) {
		return Project{}, fmt.Errorf("%w: invalid entry %q", ErrInvalidApp, in.Entry)
	}
	if shellMetaRe.MatchString(in.Args) {
		return Project{}, fmt.Errorf("%w: args contain forbidden shell characters", ErrInvalidApp)
	}
	if in.Port < 0 || in.Port > 65535 {
		return Project{}, fmt.Errorf("%w: invalid port %d", ErrInvalidApp, in.Port)
	}
	if err := sys.ValidateUserName(in.RunUser); err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrInvalidApp, err)
	}

	rootDir, err := sys.SafeJoin(s.paths.ProjectDir, in.Name)
	if err != nil {
		return Project{}, err
	}
	if err := os.MkdirAll(rootDir, 0o750); err != nil {
		return Project{}, fmt.Errorf("create project root: %w", err)
	}
	if err := os.MkdirAll(s.paths.LogsDir(), 0o750); err != nil {
		return Project{}, fmt.Errorf("create logs dir: %w", err)
	}

	binary, cmdArgs, err := s.resolveCommand(ctx, in, rootDir)
	if err != nil {
		return Project{}, err
	}

	if err := s.driver.EnsureServiceUser(ctx, in.RunUser); err != nil {
		return Project{}, fmt.Errorf("ensure service user: %w", err)
	}

	serviceName := "door-app-" + in.Name
	logFile := filepath.Join(s.paths.LogsDir(), "app-"+in.Name+".log")
	unit := sys.ServiceUnit{
		Name:        serviceName,
		Description: fmt.Sprintf("Door %s Project (%s)", strings.ToUpper(in.Lang), in.Name),
		User:        in.RunUser,
		Group:       in.RunUser,
		WorkDir:     rootDir,
		Binary:      binary,
		Args:        cmdArgs,
		Env:         in.Env,
		StdoutLog:   logFile,
		StderrLog:   logFile,
	}
	if err := s.driver.WriteServiceUnit(ctx, unit); err != nil {
		return Project{}, fmt.Errorf("write systemd unit: %w", err)
	}

	envBytes, _ := json.Marshal(in.Env)
	if len(envBytes) == 0 {
		envBytes = []byte("{}")
	}

	var runtimeID sql.NullInt64
	if in.RuntimeID != nil && *in.RuntimeID > 0 {
		runtimeID = sql.NullInt64{Int64: *in.RuntimeID, Valid: true}
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO projects (name, lang, root_dir, entry, args, runtime_id, port, run_user, env_json, service_name, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'stopped')
`, in.Name, in.Lang, rootDir, in.Entry, in.Args, runtimeID, in.Port, in.RunUser, string(envBytes), serviceName)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Project{}, ErrAlreadyExists
		}
		return Project{}, fmt.Errorf("insert project: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Project{}, err
	}
	return s.GetProject(ctx, id)
}

func (s *Service) ActionProject(ctx context.Context, id int64, action string) (Project, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return Project{}, err
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
		return Project{}, fmt.Errorf("%w: unsupported action %q", ErrInvalidApp, action)
	}

	if err := s.driver.ServiceAction(ctx, p.ServiceName, sysAct); err != nil {
		return Project{}, err
	}

	newStatus := "active"
	if sysAct == sys.ActionStop {
		newStatus = "inactive"
	}
	if st, err := s.driver.ServiceStatus(ctx, p.ServiceName); err == nil && st.Status != "not-found" {
		newStatus = st.Status
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE projects SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, newStatus, id)
	return s.GetProject(ctx, id)
}

func (s *Service) ReadProjectLog(ctx context.Context, id int64) (string, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return "", err
	}
	logPath := filepath.Join(s.paths.LogsDir(), "app-"+p.Name+".log")
	f, err := os.Open(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Sprintf("[door] 暂无运行日志 (%s)\n", logPath), nil
	}
	if err != nil {
		return "", fmt.Errorf("open project log: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, 512*1024))
	if err != nil {
		return "", fmt.Errorf("read project log: %w", err)
	}
	return string(data), nil
}

func (s *Service) DeleteProject(ctx context.Context, id int64) error {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return err
	}
	_ = s.driver.RemoveServiceUnit(ctx, p.ServiceName)
	_, err = s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (s *Service) resolveCommand(ctx context.Context, in CreateProjectInput, rootDir string) (string, []string, error) {
	extraArgs := strings.Fields(in.Args)

	var runtimeBin string
	if in.RuntimeID != nil && *in.RuntimeID > 0 {
		err := s.db.QueryRowContext(ctx, `SELECT binary_path FROM runtimes WHERE id = ?`, *in.RuntimeID).Scan(&runtimeBin)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, fmt.Errorf("%w: runtime %d not found", ErrInvalidApp, *in.RuntimeID)
		}
		if err != nil {
			return "", nil, err
		}
	}

	switch in.Lang {
	case "go":
		binPath, err := sys.SafeJoin(rootDir, in.Entry)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", ErrInvalidApp, err)
		}
		return binPath, extraArgs, nil
	case "java":
		javaBin := runtimeBin
		if javaBin == "" {
			javaBin = "/usr/bin/java"
		}
		jarPath, err := sys.SafeJoin(rootDir, in.Entry)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", ErrInvalidApp, err)
		}
		args := append([]string{"-jar", jarPath}, extraArgs...)
		return javaBin, args, nil
	case "python":
		venvDir := filepath.Join(rootDir, "venv")
		_ = os.MkdirAll(filepath.Join(venvDir, "bin"), 0o750)
		pyBin := runtimeBin
		if pyBin == "" {
			pyBin = filepath.Join(venvDir, "bin", "python3")
		}
		args := append([]string{in.Entry}, extraArgs...)
		return pyBin, args, nil
	default:
		return "", nil, ErrInvalidApp
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProject(r rowScanner) (Project, error) {
	var (
		p       Project
		rtID    sql.NullInt64
		envJSON string
	)
	err := r.Scan(
		&p.ID, &p.Name, &p.Lang, &p.RootDir, &p.Entry, &p.Args,
		&rtID, &p.Port, &p.RunUser, &envJSON, &p.ServiceName, &p.Status,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return Project{}, err
	}
	if rtID.Valid {
		v := rtID.Int64
		p.RuntimeID = &v
	}
	p.Env = make(map[string]string)
	if envJSON != "" {
		_ = json.Unmarshal([]byte(envJSON), &p.Env)
	}
	return p, nil
}
