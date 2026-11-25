//go:build linux

package sys

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

const systemdUnitDir = "/etc/systemd/system"

// LinuxDriver implements Driver using native Linux Systemd and useradd.
type LinuxDriver struct{}

// NewDriver returns MockDriver in dev mode or LinuxDriver on production Linux.
func NewDriver(dev bool, sandboxDir string) Driver {
	if dev {
		return NewMockDriver(sandboxDir)
	}
	return &LinuxDriver{}
}

func (l *LinuxDriver) Mode() string { return "linux-systemd" }

func (l *LinuxDriver) EnsureServiceUser(ctx context.Context, username string) error {
	if err := ValidateUserName(username); err != nil {
		return err
	}
	if _, err := user.Lookup(username); err == nil {
		return nil
	}
	res, err := l.Exec(ctx, CommandSpec{
		Binary: "useradd",
		Args:   []string{"-r", "-s", "/sbin/nologin", username},
	})
	if err != nil {
		return fmt.Errorf("create system user %s: %w (%s)", username, err, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (l *LinuxDriver) WriteServiceUnit(ctx context.Context, unit ServiceUnit) error {
	content, err := RenderServiceUnit(unit)
	if err != nil {
		return err
	}
	if err := WriteFileRoot(systemdUnitDir, unit.Name+".service", []byte(content), 0o644); err != nil {
		return err
	}
	_, err = l.Exec(ctx, CommandSpec{Binary: "systemctl", Args: []string{"daemon-reload"}})
	return err
}

func (l *LinuxDriver) RemoveServiceUnit(ctx context.Context, name string) error {
	if err := ValidateServiceName(name); err != nil {
		return err
	}
	_ = l.ServiceAction(ctx, name, ActionStop)
	_ = l.ServiceAction(ctx, name, ActionDisable)
	target, err := SafeJoin(systemdUnitDir, name+".service")
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = l.Exec(ctx, CommandSpec{Binary: "systemctl", Args: []string{"daemon-reload"}})
	return err
}

func (l *LinuxDriver) ServiceAction(ctx context.Context, name string, action ServiceAction) error {
	if err := ValidateServiceName(name); err != nil {
		return err
	}
	switch action {
	case ActionStart, ActionStop, ActionRestart, ActionReload, ActionEnable, ActionDisable:
	default:
		return fmt.Errorf("unsupported service action: %s", action)
	}
	res, err := l.Exec(ctx, CommandSpec{
		Binary: "systemctl",
		Args:   []string{string(action), name + ".service"},
	})
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w (%s)", action, name, err, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (l *LinuxDriver) ServiceStatus(ctx context.Context, name string) (ServiceState, error) {
	if err := ValidateServiceName(name); err != nil {
		return ServiceState{}, err
	}
	activeRes, _ := l.Exec(ctx, CommandSpec{
		Binary: "systemctl",
		Args:   []string{"is-active", name + ".service"},
	})
	enabledRes, _ := l.Exec(ctx, CommandSpec{
		Binary: "systemctl",
		Args:   []string{"is-enabled", name + ".service"},
	})
	status := strings.TrimSpace(activeRes.Stdout)
	if status == "" {
		status = "unknown"
	}
	return ServiceState{
		Name:    name,
		Active:  status == "active",
		Enabled: strings.TrimSpace(enabledRes.Stdout) == "enabled",
		Status:  status,
	}, nil
}

func (l *LinuxDriver) Exec(ctx context.Context, spec CommandSpec) (CommandResult, error) {
	if spec.Binary == "" {
		return CommandResult{}, fmt.Errorf("empty binary path")
	}
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	if spec.WorkDir != "" {
		cmd.Dir = spec.WorkDir
	}
	if len(spec.Env) > 0 {
		cmd.Env = spec.Env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return CommandResult{}, fmt.Errorf("run %s: %w", spec.Binary, err)
		}
	}
	return CommandResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, err
}
