package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DefaultHome() string {
	if v := os.Getenv("ACROSS_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".across"
	}
	return filepath.Join(home, ".local", "share", "across")
}

func EnsureHome(home string) error {
	if strings.TrimSpace(home) == "" {
		return fmt.Errorf("home path must not be empty")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	if err := EnsureDirectory(abs); err != nil {
		return err
	}
	for _, sub := range []string{"repositories", "mirrors", "workspaces", "plugins", "backups", "tmp", "logs"} {
		if err := EnsureDirectory(filepath.Join(abs, sub)); err != nil {
			return err
		}
	}
	return nil
}

func EnsureDirectory(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("directory path must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := rejectSymlinkComponents(abs); err != nil {
		return err
	}
	return ensureDirectory(abs)
}

func ResolveDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("directory path must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkComponents(abs); err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("directory path must not contain symbolic links: %s", path)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	resolvedInfo, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if resolvedInfo.Mode()&os.ModeSymlink != 0 || !resolvedInfo.IsDir() {
		return "", fmt.Errorf("directory path must be a real directory: %s", path)
	}
	return resolved, nil
}

func ensureDirectory(path string) error {
	path = filepath.Clean(path)
	missing := make([]string, 0)
	current := path
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("directory path must not contain symbolic links: %s", path)
			}
			if !info.IsDir() {
				return fmt.Errorf("path is not a directory: %s", current)
			}
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("cannot find directory parent: %s", current)
		}
		missing = append(missing, current)
		current = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(missing[i])
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory path must not contain symbolic links: %s", path)
		}
		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", missing[i])
		}
	}
	return nil
}

func rejectSymlinkComponents(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(abs)
	root := string(filepath.Separator)
	if volume != "" {
		root = volume + string(filepath.Separator)
	}
	current := root
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if current != filepath.Clean(abs) && trustedSystemPrefix(current) {
			continue
		}
		return fmt.Errorf("directory path must not contain symbolic links: %s", path)
	}
	return nil
}

func trustedSystemPrefix(component string) bool {
	roots := []string{os.TempDir(), "/tmp", "/var", "/private/var", "/private/tmp"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}
	component = filepath.Clean(component)
	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		root = filepath.Clean(root)
		if component == root || strings.HasPrefix(root, component+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func DBPath(home string) string { return filepath.Join(home, "across.db") }
