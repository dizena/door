package nginx

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
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
	ErrInvalidNginx = errors.New("invalid nginx or site configuration")
	ErrSiteNotFound = errors.New("site not found")
	ErrCertNotFound = errors.New("ssl certificate not found")
	ErrSiteExists   = errors.New("site domain already exists")
)

var domainRe = regexp.MustCompile(`^(localhost|(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+)$`)

type ServiceInfo struct {
	Installed   bool   `json:"installed"`
	BinaryPath  string `json:"binary_path"`
	ConfigPath  string `json:"config_path"`
	Status      string `json:"status"`
	Active      bool   `json:"active"`
	ConfigText  string `json:"config_text"`
}

type Site struct {
	ID           int64     `json:"id"`
	Domain       string    `json:"domain"`
	ExtraDomains string    `json:"extra_domains"`
	ListenPort   int       `json:"listen_port"`
	SiteType     string    `json:"site_type"`
	RootDir      string    `json:"root_dir"`
	ProxyPass    string    `json:"proxy_pass"`
	SSLEnabled   bool      `json:"ssl_enabled"`
	SSLCertID    *int64    `json:"ssl_cert_id"`
	ConfigPath   string    `json:"config_path"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateSiteInput struct {
	Domain       string `json:"domain"`
	ExtraDomains string `json:"extra_domains"`
	ListenPort   int    `json:"listen_port"`
	SiteType     string `json:"site_type"`
	RootDir      string `json:"root_dir"`
	ProxyPass    string `json:"proxy_pass"`
	SSLEnabled   bool   `json:"ssl_enabled"`
	SSLCertID    *int64 `json:"ssl_cert_id"`
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

func (s *Service) mainConfigPath() string {
	return filepath.Join(s.paths.NginxDir, "conf", "nginx.conf")
}

func (s *Service) acmeWebroot() string {
	return filepath.Join(s.paths.NginxSSLDir(), "acme-webroot")
}

func (s *Service) binaryPath() string {
	local := filepath.Join(s.paths.NginxDir, "sbin", "nginx")
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		return local
	}
	if sysBin, err := exec.LookPath("nginx"); err == nil {
		return sysBin
	}
	return local
}

func (s *Service) Info(ctx context.Context) (ServiceInfo, error) {
	bin := s.binaryPath()
	_, err := os.Stat(bin)
	installed := err == nil
	st, _ := s.driver.ServiceStatus(ctx, "door-nginx")
	cfgBytes, _ := os.ReadFile(s.mainConfigPath())
	return ServiceInfo{
		Installed:  installed,
		BinaryPath: bin,
		ConfigPath: s.mainConfigPath(),
		Status:     st.Status,
		Active:     st.Active,
		ConfigText: string(cfgBytes),
	}, nil
}

// InstallNginx provisions /door/nginx directory layout, default nginx.conf, and Systemd unit.
func (s *Service) InstallNginx(ctx context.Context) (task.Task, error) {
	return s.tasks.Submit(ctx, "install_nginx", "部署原生 Nginx 服务", func(runCtx context.Context, sink task.LogSink) error {
		sink.SetProgress(20)
		for _, dir := range []string{
			filepath.Join(s.paths.NginxDir, "sbin"),
			s.paths.NginxVHostDir(),
			s.paths.NginxSSLDir(),
			s.acmeWebroot(),
			s.paths.NginxLogsDir(),
		} {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("create nginx dir %s: %w", dir, err)
			}
		}

		sink.SetProgress(45)
		binPath := filepath.Join(s.paths.NginxDir, "sbin", "nginx")
		if _, err := os.Stat(binPath); errors.Is(err, os.ErrNotExist) {
			if hostBin, lookErr := exec.LookPath("nginx"); lookErr == nil {
				sink.Logf("检测到宿主 Nginx (%s)，建立隔离软链接至 %s", hostBin, binPath)
				_ = os.Symlink(hostBin, binPath)
			} else if s.driver.Mode() == "mock-local" {
				sink.Logf("沙箱模式：生成模拟 Nginx 二进制 %s", binPath)
				mockScript := "#!/bin/sh\nif [ \"$1\" = \"-t\" ]; then echo 'nginx: configuration file syntax is ok'; exit 0; fi\necho 'nginx version: nginx/1.26.2 (door-sandbox)'\n"
				if err := os.WriteFile(binPath, []byte(mockScript), 0o755); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("nginx binary not found; please install nginx or place binary at %s", binPath)
			}
		}

		sink.SetProgress(70)
		if _, err := os.Stat(s.mainConfigPath()); errors.Is(err, os.ErrNotExist) {
			if err := s.writeMainConfig(); err != nil {
				return err
			}
		}

		if err := s.driver.EnsureServiceUser(runCtx, "www"); err != nil {
			return err
		}
		if err := s.driver.WriteServiceUnit(runCtx, sys.ServiceUnit{
			Name:        "door-nginx",
			Description: "Door Managed Nginx Web Server",
			User:        "root",
			Binary:      binPath,
			Args:        []string{"-c", s.mainConfigPath(), "-g", "daemon off;"},
			StdoutLog:   filepath.Join(s.paths.NginxLogsDir(), "service.log"),
			StderrLog:   filepath.Join(s.paths.NginxLogsDir(), "service.log"),
		}); err != nil {
			return err
		}

		sink.Logf("Nginx 已部署至 %s", s.paths.NginxDir)
		return nil
	})
}

func (s *Service) Action(ctx context.Context, action string) (ServiceInfo, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "test" {
		if err := s.testConfig(ctx); err != nil {
			return ServiceInfo{}, err
		}
		return s.Info(ctx)
	}

	var sysAct sys.ServiceAction
	switch action {
	case "start":
		if err := s.testConfig(ctx); err != nil {
			return ServiceInfo{}, err
		}
		sysAct = sys.ActionStart
	case "stop":
		sysAct = sys.ActionStop
	case "restart":
		if err := s.testConfig(ctx); err != nil {
			return ServiceInfo{}, err
		}
		sysAct = sys.ActionRestart
	case "reload":
		if err := s.testConfig(ctx); err != nil {
			return ServiceInfo{}, err
		}
		sysAct = sys.ActionReload
	default:
		return ServiceInfo{}, fmt.Errorf("%w: unsupported action %q", ErrInvalidNginx, action)
	}

	if err := s.driver.ServiceAction(ctx, "door-nginx", sysAct); err != nil {
		return ServiceInfo{}, err
	}
	return s.Info(ctx)
}

func (s *Service) UpdateMainConfig(ctx context.Context, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: nginx.conf cannot be empty", ErrInvalidNginx)
	}
	prev, hadPrev := s.readFileIfExists(s.mainConfigPath())
	if err := sys.WriteFileRoot(s.paths.NginxDir, filepath.Join("conf", "nginx.conf"), []byte(content), 0o644); err != nil {
		return err
	}
	if err := s.testConfig(ctx); err != nil {
		if hadPrev {
			_ = sys.WriteFileRoot(s.paths.NginxDir, filepath.Join("conf", "nginx.conf"), prev, 0o644)
		}
		return err
	}
	return nil
}

func (s *Service) ListSites(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, domain, extra_domains, listen_port, site_type, root_dir, proxy_pass, ssl_enabled, ssl_cert_id, config_path, status, created_at, updated_at
FROM sites ORDER BY id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("query sites: %w", err)
	}
	defer rows.Close()

	out := []Site{}
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

func (s *Service) GetSite(ctx context.Context, id int64) (Site, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, domain, extra_domains, listen_port, site_type, root_dir, proxy_pass, ssl_enabled, ssl_cert_id, config_path, status, created_at, updated_at
FROM sites WHERE id = ?
`, id)
	site, err := scanSite(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrSiteNotFound
	}
	return site, err
}

func (s *Service) CreateSite(ctx context.Context, in CreateSiteInput) (Site, error) {
	in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	in.ExtraDomains = strings.TrimSpace(in.ExtraDomains)
	in.SiteType = strings.ToLower(strings.TrimSpace(in.SiteType))
	in.ProxyPass = strings.TrimSpace(in.ProxyPass)
	if in.ListenPort == 0 {
		in.ListenPort = 80
	}

	if err := ValidateDomain(in.Domain); err != nil {
		return Site{}, err
	}
	if in.ListenPort < 1 || in.ListenPort > 65535 {
		return Site{}, fmt.Errorf("%w: invalid listen port %d", ErrInvalidNginx, in.ListenPort)
	}

	var extraList []string
	if in.ExtraDomains != "" {
		for _, d := range strings.Fields(in.ExtraDomains) {
			d = strings.ToLower(strings.TrimSpace(d))
			if err := ValidateDomain(d); err != nil {
				return Site{}, err
			}
			extraList = append(extraList, d)
		}
	}

	var rootDir string
	switch in.SiteType {
	case "proxy":
		u, err := url.Parse(in.ProxyPass)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(in.ProxyPass, ";\n\r{}") {
			return Site{}, fmt.Errorf("%w: invalid proxy_pass url %q", ErrInvalidNginx, in.ProxyPass)
		}
		rootDir = ""
	case "static":
		rel := filepath.Join("sites", strings.ReplaceAll(in.Domain, "*", "_wildcard_"))
		var err error
		rootDir, err = sys.SafeJoin(s.paths.ProjectDir, rel)
		if err != nil {
			return Site{}, err
		}
		if err := os.MkdirAll(rootDir, 0o750); err != nil {
			return Site{}, fmt.Errorf("create site root: %w", err)
		}
		indexFile := filepath.Join(rootDir, "index.html")
		if _, err := os.Stat(indexFile); errors.Is(err, os.ErrNotExist) {
			_ = os.WriteFile(indexFile, []byte("<h1>Welcome to "+in.Domain+" (Door Panel)</h1>\n"), 0o644)
		}
	default:
		return Site{}, fmt.Errorf("%w: site_type must be proxy or static", ErrInvalidNginx)
	}

	var certPath, keyPath string
	if in.SSLEnabled {
		if in.SSLCertID == nil || *in.SSLCertID <= 0 {
			return Site{}, fmt.Errorf("%w: ssl_cert_id is required when ssl_enabled is true", ErrInvalidNginx)
		}
		c, err := s.GetCert(ctx, *in.SSLCertID)
		if err != nil {
			return Site{}, err
		}
		certPath = c.CertPath
		keyPath = c.KeyPath
	}

	if _, err := os.Stat(s.mainConfigPath()); errors.Is(err, os.ErrNotExist) {
		_ = s.writeMainConfig()
	}

	confFileName := strings.ReplaceAll(in.Domain, "*", "_wildcard_") + ".conf"
	relConf := filepath.Join("conf", "vhost", confFileName)
	absConf, err := sys.SafeJoin(s.paths.NginxDir, relConf)
	if err != nil {
		return Site{}, err
	}

	rendered, err := renderSiteConfig(siteRenderData{
		Domain:      in.Domain,
		ServerNames: strings.Join(append([]string{in.Domain}, extraList...), " "),
		ListenPort:  in.ListenPort,
		SiteType:    in.SiteType,
		RootDir:     rootDir,
		ProxyPass:   in.ProxyPass,
		SSLEnabled:  in.SSLEnabled,
		CertPath:    certPath,
		KeyPath:     keyPath,
		AcmeWebroot: s.acmeWebroot(),
		AccessLog:   filepath.Join(s.paths.NginxLogsDir(), in.Domain+".access.log"),
		ErrorLog:    filepath.Join(s.paths.NginxLogsDir(), in.Domain+".error.log"),
	})
	if err != nil {
		return Site{}, err
	}

	prev, hadPrev := s.readFileIfExists(absConf)
	if err := sys.WriteFileRoot(s.paths.NginxDir, relConf, []byte(rendered), 0o644); err != nil {
		return Site{}, err
	}
	if err := s.testConfig(ctx); err != nil {
		if hadPrev {
			_ = sys.WriteFileRoot(s.paths.NginxDir, relConf, prev, 0o644)
		} else {
			_ = os.Remove(absConf)
		}
		return Site{}, err
	}

	var certIDVal sql.NullInt64
	if in.SSLCertID != nil && *in.SSLCertID > 0 {
		certIDVal = sql.NullInt64{Int64: *in.SSLCertID, Valid: true}
	}
	sslInt := 0
	if in.SSLEnabled {
		sslInt = 1
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO sites (domain, extra_domains, listen_port, site_type, root_dir, proxy_pass, ssl_enabled, ssl_cert_id, config_path, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'enabled')
`, in.Domain, strings.Join(extraList, " "), in.ListenPort, in.SiteType, rootDir, in.ProxyPass, sslInt, certIDVal, absConf)
	if err != nil {
		_ = os.Remove(absConf)
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Site{}, ErrSiteExists
		}
		return Site{}, fmt.Errorf("insert site: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Site{}, err
	}
	s.reloadIfActive(ctx)
	return s.GetSite(ctx, id)
}

func (s *Service) ReadSiteConfig(ctx context.Context, id int64) (string, error) {
	site, err := s.GetSite(ctx, id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(site.ConfigPath)
	if err != nil {
		return "", fmt.Errorf("read site config: %w", err)
	}
	return string(data), nil
}

func (s *Service) UpdateSiteConfig(ctx context.Context, id int64, raw string) error {
	site, err := s.GetSite(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%w: site config cannot be empty", ErrInvalidNginx)
	}
	relConf, err := filepath.Rel(s.paths.NginxDir, site.ConfigPath)
	if err != nil {
		return err
	}
	prev, hadPrev := s.readFileIfExists(site.ConfigPath)
	if err := sys.WriteFileRoot(s.paths.NginxDir, relConf, []byte(raw), 0o644); err != nil {
		return err
	}
	if err := s.testConfig(ctx); err != nil {
		if hadPrev {
			_ = sys.WriteFileRoot(s.paths.NginxDir, relConf, prev, 0o644)
		}
		return err
	}
	s.reloadIfActive(ctx)
	return nil
}

func (s *Service) DeleteSite(ctx context.Context, id int64) error {
	site, err := s.GetSite(ctx, id)
	if err != nil {
		return err
	}
	_ = os.Remove(site.ConfigPath)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id); err != nil {
		return err
	}
	s.reloadIfActive(ctx)
	return nil
}

func (s *Service) testConfig(ctx context.Context) error {
	bin := s.binaryPath()
	if _, err := os.Stat(bin); errors.Is(err, os.ErrNotExist) {
		if s.driver.Mode() == "mock-local" {
			return nil
		}
		return fmt.Errorf("nginx binary not installed at %s", bin)
	}
	res, err := s.driver.Exec(ctx, sys.CommandSpec{
		Binary: bin,
		Args:   []string{"-t", "-c", s.mainConfigPath()},
	})
	if err != nil {
		return fmt.Errorf("nginx -t failed: %s %s", strings.TrimSpace(res.Stderr), strings.TrimSpace(res.Stdout))
	}
	return nil
}

func (s *Service) reloadIfActive(ctx context.Context) {
	if st, err := s.driver.ServiceStatus(ctx, "door-nginx"); err == nil && st.Active {
		_ = s.driver.ServiceAction(ctx, "door-nginx", sys.ActionReload)
	}
}

func (s *Service) writeMainConfig() error {
	var buf bytes.Buffer
	err := mainConfigTmpl.Execute(&buf, map[string]string{
		"VHostGlob": filepath.Join(s.paths.NginxVHostDir(), "*.conf"),
		"ErrorLog":  filepath.Join(s.paths.NginxLogsDir(), "error.log"),
		"AccessLog": filepath.Join(s.paths.NginxLogsDir(), "access.log"),
		"PidFile":   filepath.Join(s.paths.NginxLogsDir(), "nginx.pid"),
	})
	if err != nil {
		return err
	}
	return sys.WriteFileRoot(s.paths.NginxDir, filepath.Join("conf", "nginx.conf"), buf.Bytes(), 0o644)
}

func (s *Service) readFileIfExists(p string) ([]byte, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return b, true
}

func ValidateDomain(domain string) error {
	if !domainRe.MatchString(domain) || len(domain) > 253 {
		return fmt.Errorf("%w: invalid domain %q", ErrInvalidNginx, domain)
	}
	return nil
}

var mainConfigTmpl = template.Must(template.New("nginx-main").Parse(`worker_processes auto;
pid {{ .PidFile }};
error_log {{ .ErrorLog }} warn;

events {
    worker_connections 1024;
}

http {
    include       mime.types;
    default_type  application/octet-stream;
    sendfile      on;
    keepalive_timeout 65;
    gzip on;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml;

    access_log {{ .AccessLog }};

    include {{ .VHostGlob }};
}
`))

type siteRenderData struct {
	Domain      string
	ServerNames string
	ListenPort  int
	SiteType    string
	RootDir     string
	ProxyPass   string
	SSLEnabled  bool
	CertPath    string
	KeyPath     string
	AcmeWebroot string
	AccessLog   string
	ErrorLog    string
}

var siteConfigTmpl = template.Must(template.New("nginx-site").Parse(`server {
    listen {{ .ListenPort }};
    server_name {{ .ServerNames }};

    access_log {{ .AccessLog }};
    error_log  {{ .ErrorLog }};

    location /.well-known/acme-challenge/ {
        root {{ .AcmeWebroot }};
    }
{{- if eq .SiteType "proxy" }}

    location / {
        proxy_pass {{ .ProxyPass }};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
{{- else }}

    root {{ .RootDir }};
    index index.html index.htm;

    location / {
        try_files $uri $uri/ /index.html;
    }
{{- end }}
}
{{- if .SSLEnabled }}

server {
    listen 443 ssl;
    http2 on;
    server_name {{ .ServerNames }};

    ssl_certificate     {{ .CertPath }};
    ssl_certificate_key {{ .KeyPath }};
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    access_log {{ .AccessLog }};
    error_log  {{ .ErrorLog }};
{{- if eq .SiteType "proxy" }}

    location / {
        proxy_pass {{ .ProxyPass }};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
{{- else }}

    root {{ .RootDir }};
    index index.html index.htm;

    location / {
        try_files $uri $uri/ /index.html;
    }
{{- end }}
}
{{- end }}
`))

func renderSiteConfig(d siteRenderData) (string, error) {
	var buf bytes.Buffer
	if err := siteConfigTmpl.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("render site config: %w", err)
	}
	return buf.String(), nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSite(r rowScanner) (Site, error) {
	var (
		st      Site
		sslInt  int
		certID  sql.NullInt64
	)
	err := r.Scan(
		&st.ID, &st.Domain, &st.ExtraDomains, &st.ListenPort, &st.SiteType,
		&st.RootDir, &st.ProxyPass, &sslInt, &certID, &st.ConfigPath,
		&st.Status, &st.CreatedAt, &st.UpdatedAt,
	)
	if err != nil {
		return Site{}, err
	}
	st.SSLEnabled = sslInt == 1
	if certID.Valid {
		v := certID.Int64
		st.SSLCertID = &v
	}
	return st, nil
}
