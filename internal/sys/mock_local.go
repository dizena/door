package sys

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
)

// MockDriver provides an in-memory and sandbox-filesystem Driver for macOS dev and unit tests.
type MockDriver struct {
	mu         sync.RWMutex
	sandboxDir string
	users      map[string]struct{}
	units      map[string]string
	states     map[string]ServiceState
}

// NewMockDriver creates a local sandbox driver writing unit files under sandboxDir/systemd.
func NewMockDriver(sandboxDir string) *MockDriver {
	return &MockDriver{
		sandboxDir: sandboxDir,
		users:      make(map[string]struct{}),
		units:      make(map[string]string),
		states:     make(map[string]ServiceState),
	}
}

func (m *MockDriver) Mode() string { return "mock-local" }

func (m *MockDriver) EnsureServiceUser(_ context.Context, username string) error {
	if err := ValidateUserName(username); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[username] = struct{}{}
	return nil
}

func (m *MockDriver) WriteServiceUnit(_ context.Context, unit ServiceUnit) error {
	content, err := RenderServiceUnit(unit)
	if err != nil {
		return err
	}

	if m.sandboxDir != "" {
		if err := WriteFileRoot(filepath.Join(m.sandboxDir, "systemd"), unit.Name+".service", []byte(content), 0o644); err != nil {
			return err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.units[unit.Name] = content
	if _, ok := m.states[unit.Name]; !ok {
		m.states[unit.Name] = ServiceState{Name: unit.Name, Status: "inactive"}
	}
	return nil
}

func (m *MockDriver) RemoveServiceUnit(_ context.Context, name string) error {
	if err := ValidateServiceName(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.units, name)
	delete(m.states, name)
	return nil
}

func (m *MockDriver) ServiceAction(_ context.Context, name string, action ServiceAction) error {
	if err := ValidateServiceName(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[name]
	st.Name = name
	switch action {
	case ActionStart, ActionRestart:
		st.Active = true
		st.Status = "active"
	case ActionStop:
		st.Active = false
		st.Status = "inactive"
	case ActionReload:
		if st.Active {
			st.Status = "active"
		}
	case ActionEnable:
		st.Enabled = true
	case ActionDisable:
		st.Enabled = false
	default:
		return fmt.Errorf("unsupported service action: %s", action)
	}
	m.states[name] = st
	return nil
}

func (m *MockDriver) ServiceStatus(_ context.Context, name string) (ServiceState, error) {
	if err := ValidateServiceName(name); err != nil {
		return ServiceState{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.states[name]
	if !ok {
		return ServiceState{Name: name, Status: "not-found"}, nil
	}
	return st, nil
}

func (m *MockDriver) Exec(ctx context.Context, spec CommandSpec) (CommandResult, error) {
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
		if ee, ok := err.(*exec.ExitError); ok {
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
