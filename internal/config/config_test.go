package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureHomeRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(link, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHome(home); err == nil {
		t.Fatal("home beneath a user symlink was accepted")
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
