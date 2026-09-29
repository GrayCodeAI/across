package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureHomeResolvesUserChosenSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	beneath := filepath.Join(link, "home")
	if err := EnsureHome(beneath); err != nil {
		t.Fatalf("home beneath a symlinked ancestor was rejected: %v", err)
	}
	if info, err := os.Stat(filepath.Join(outside, "home", "repositories")); err != nil || !info.IsDir() {
		t.Fatalf("home was not created at the resolved location: %v", err)
	}
	homeLink := filepath.Join(root, "home-link")
	if err := os.Symlink(filepath.Join(outside, "home"), homeLink); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHome(homeLink); err != nil {
		t.Fatalf("symlinked home was rejected: %v", err)
	}
}

func TestEnsureHomeRejectsSymlinkedManagedDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "tmp")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHome(home); err == nil {
		t.Fatal("symlinked managed directory inside the home was accepted")
	}
}

func TestEnsureHomeDoesNotCreateLogsDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, "logs")); !os.IsNotExist(err) {
		t.Fatalf("unused logs directory created: %v", err)
	}
}

func TestResolveDirectoryRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(link, "hooks")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDirectory(directory); err == nil {
		t.Fatal("directory beneath a user symlink was accepted")
	}
}
