package sys

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

var (
	ErrInvalidServiceName = errors.New("invalid service name")
	ErrInvalidUserName    = errors.New("invalid system user name")
	ErrPathEscape         = errors.New("path escapes allowed root")
)

var (
	serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	userNameRe    = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
)

// ServiceAction represents a lifecycle action on a managed service.
type ServiceAction string

const (
	ActionStart   ServiceAction = "start"
	ActionStop    ServiceAction = "stop"
	ActionRestart ServiceAction = "restart"
	ActionReload  ServiceAction = "reload"
	ActionEnable  ServiceAction = "enable"
	ActionDisable ServiceAction = "disable"
)

// ServiceState describes the observed status of a managed service.
type ServiceState struct {
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

// ServiceUnit defines a Systemd service configuration rendered safely via template.
type ServiceUnit struct {
	Name        string
	Description string
	User        string
	Group       string
	WorkDir     string
	Binary      string
	Args        []string
	Env         map[string]string
	StdoutLog   string
	StderrLog   string
}

// CommandSpec describes a non-shell direct process execution.
type CommandSpec struct {
	Binary  string
	Args    []string
	WorkDir string
	Env     []string
}

// CommandResult captures stdout, stderr, and exit code from a command run.
type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Driver abstracts Linux host operations so macOS dev & tests can run in a local sandbox.
type Driver interface {
	Mode() string
	EnsureServiceUser(ctx context.Context, username string) error
	WriteServiceUnit(ctx context.Context, unit ServiceUnit) error
	RemoveServiceUnit(ctx context.Context, name string) error
	ServiceAction(ctx context.Context, name string, action ServiceAction) error
	ServiceStatus(ctx context.Context, name string) (ServiceState, error)
	Exec(ctx context.Context, spec CommandSpec) (CommandResult, error)
}

// ValidateServiceName enforces an allowlist for Systemd unit names.
func ValidateServiceName(name string) error {
	if !serviceNameRe.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidServiceName, name)
	}
	return nil
}

// ValidateUserName enforces an allowlist for Linux system accounts.
func ValidateUserName(name string) error {
	if !userNameRe.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidUserName, name)
	}
	return nil
}

var unitTmpl = template.Must(template.New("systemd-unit").Parse(`[Unit]
Description={{ .Description }}
After=network.target

[Service]
Type=simple
{{- if .User }}
User={{ .User }}
{{- end }}
{{- if .Group }}
Group={{ .Group }}
{{- end }}
{{- if .WorkDir }}
WorkingDirectory={{ .WorkDir }}
{{- end }}
{{- range $k, $v := .EnvLines }}
Environment="{{ $k }}={{ $v }}"
{{- end }}
ExecStart={{ .ExecLine }}
Restart=on-failure
RestartSec=3
{{- if .StdoutLog }}
StandardOutput=append:{{ .StdoutLog }}
{{- end }}
{{- if .StderrLog }}
StandardError=append:{{ .StderrLog }}
{{- end }}

[Install]
WantedBy=multi-user.target
`))

type unitRenderData struct {
	Description string
	User        string
	Group       string
	WorkDir     string
	EnvLines    map[string]string
	ExecLine    string
	StdoutLog   string
	StderrLog   string
}

// RenderServiceUnit validates and renders a Systemd unit file safely without shell interpolation.
func RenderServiceUnit(unit ServiceUnit) (string, error) {
	if err := ValidateServiceName(unit.Name); err != nil {
		return "", err
	}
	if unit.User != "" {
		if err := ValidateUserName(unit.User); err != nil {
			return "", err
		}
	}
	if unit.Group != "" {
		if err := ValidateUserName(unit.Group); err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(unit.Binary) == "" || strings.ContainsAny(unit.Binary, "\r\n") {
		return "", fmt.Errorf("invalid binary path")
	}

	parts := make([]string, 0, 1+len(unit.Args))
	parts = append(parts, quoteSystemdArg(unit.Binary))
	for _, arg := range unit.Args {
		if strings.ContainsAny(arg, "\r\n") {
			return "", fmt.Errorf("argument contains newline")
		}
		parts = append(parts, quoteSystemdArg(arg))
	}

	cleanEnv := make(map[string]string, len(unit.Env))
	for k, v := range unit.Env {
		if strings.ContainsAny(k, "\r\n=\"") || strings.ContainsAny(v, "\r\n\"") {
			return "", fmt.Errorf("invalid env entry %q", k)
		}
		cleanEnv[k] = v
	}

	desc := strings.ReplaceAll(unit.Description, "\n", " ")
	if desc == "" {
		desc = "Door managed service " + unit.Name
	}

	var buf bytes.Buffer
	err := unitTmpl.Execute(&buf, unitRenderData{
		Description: desc,
		User:        unit.User,
		Group:       unit.Group,
		WorkDir:     unit.WorkDir,
		EnvLines:    cleanEnv,
		ExecLine:    strings.Join(parts, " "),
		StdoutLog:   unit.StdoutLog,
		StderrLog:   unit.StderrLog,
	})
	if err != nil {
		return "", fmt.Errorf("render systemd unit: %w", err)
	}
	return buf.String(), nil
}

func quoteSystemdArg(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"'\\") {
		escaped := strings.ReplaceAll(s, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		return `"` + escaped + `"`
	}
	return s
}
