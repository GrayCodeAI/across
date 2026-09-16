package config

import (
	"os"
	"path/filepath"
)

// DefaultHome returns ~/.local/share/across unless ACROSS_HOME is set.
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

// EnsureHome creates required subdirectories.
func EnsureHome(home string) error {
	subs := []string{"", "repositories", "mirrors", "workspaces", "plugins", "backups", "tmp", "logs"}
	for _, s := range subs {
		if err := os.MkdirAll(filepath.Join(home, s), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func DBPath(home string) string { return filepath.Join(home, "across.db") }
