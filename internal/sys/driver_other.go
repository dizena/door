//go:build !linux

package sys

// NewDriver always returns MockDriver on non-Linux development platforms (e.g. macOS).
func NewDriver(_ bool, sandboxDir string) Driver {
	return NewMockDriver(sandboxDir)
}
