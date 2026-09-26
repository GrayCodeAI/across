package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHookRepeatedUpgradePreservesOriginal(t *testing.T) {
	directory := t.TempDir()
	hookPath := filepath.Join(directory, "post-commit")
	original := []byte("#!/bin/sh\nprintf original\n")
	if err := os.WriteFile(hookPath, original, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallHook(directory, "post-commit", "#!/bin/sh\n# Across automatic checkpoint hook v1\n"); err != nil {
		t.Fatal(err)
	}
	originalPath := hookPath + ".across-orig"
	preserved, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != string(original) {
		t.Fatalf("original changed: %q", preserved)
	}
	if err := InstallHook(directory, "post-commit", "#!/bin/sh\n# Across automatic checkpoint hook v2\n"); err != nil {
		t.Fatal(err)
	}
	upgraded, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(upgraded), "v2") || !strings.Contains(string(upgraded), acrossHookMarker) {
		t.Fatalf("upgraded hook: %q", upgraded)
	}
	preserved, err = os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != string(original) {
		t.Fatalf("original changed after upgrade: %q", preserved)
	}
	if err := UninstallHook(directory, "post-commit"); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Fatalf("uninstall did not restore original: %q", restored)
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Fatalf("original sidecar remains: %v", err)
	}
}

func TestInstallHookRejectsSymlinkDestination(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(directory, "post-commit")
	if err := os.Symlink(outside, hookPath); err != nil {
		t.Fatal(err)
	}
	if err := InstallHook(directory, "post-commit", "#!/bin/sh\n"); err == nil {
		t.Fatal("symlink hook destination was accepted")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("symlink target changed: %q %v", data, err)
	}
}

func TestInstallHookRejectsSymlinkDirectory(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "hooks")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := InstallHook(link, "post-commit", "#!/bin/sh\n"); err == nil {
		t.Fatal("symlink hooks directory was accepted")
	}
	linkedParent := filepath.Join(parent, "linked-parent")
	if err := os.Symlink(real, linkedParent); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(linkedParent, "hooks")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallHook(nested, "post-commit", "#!/bin/sh\n"); err == nil {
		t.Fatal("hooks directory beneath a user symlink was accepted")
	}
}

func TestInstallHookRejectsPathEscapes(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"../post-commit", "hooks/post-commit", `post-commit\other`, `C:\post-commit`} {
		t.Run(name, func(t *testing.T) {
			if err := InstallHook(directory, name, "#!/bin/sh\n"); err == nil {
				t.Fatalf("hook name %q was accepted", name)
			}
		})
	}
}
