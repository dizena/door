package sys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafeJoin resolves rel inside baseDir and rejects any attempt to escape baseDir.
func SafeJoin(baseDir, rel string) (string, error) {
	if baseDir == "" {
		return "", fmt.Errorf("%w: empty base directory", ErrPathEscape)
	}
	absBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}
	if rel == "" || rel == "." {
		return absBase, nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: absolute path not allowed (%s)", ErrPathEscape, rel)
	}

	joined := filepath.Clean(filepath.Join(absBase, rel))
	diff, err := filepath.Rel(absBase, joined)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	if diff == ".." || strings.HasPrefix(diff, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrPathEscape, rel)
	}
	return joined, nil
}

// WriteFileRoot writes data to relPath confined within baseDir using Go's os.Root.
func WriteFileRoot(baseDir, relPath string, data []byte, perm os.FileMode) error {
	cleanRel := filepath.Clean(relPath)
	if filepath.IsAbs(cleanRel) || cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s", ErrPathEscape, relPath)
	}

	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return fmt.Errorf("create base directory: %w", err)
	}

	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return fmt.Errorf("open root %s: %w", baseDir, err)
	}
	defer root.Close()

	dir := filepath.Dir(cleanRel)
	if dir != "." {
		if err := mkdirAllRoot(root, dir); err != nil {
			return err
		}
	}

	f, err := root.OpenFile(cleanRel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open confined file %s: %w", cleanRel, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write confined file %s: %w", cleanRel, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close confined file %s: %w", cleanRel, err)
	}
	return nil
}

func mkdirAllRoot(root *os.Root, dir string) error {
	parts := strings.Split(filepath.Clean(dir), string(filepath.Separator))
	cur := ""
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if cur == "" {
			cur = part
		} else {
			cur = filepath.Join(cur, part)
		}
		if err := root.Mkdir(cur, 0o750); err != nil && !os.IsExist(err) {
			return fmt.Errorf("mkdir inside root %s: %w", cur, err)
		}
	}
	return nil
}
