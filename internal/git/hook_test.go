package git

import (
	"errors"
	"os"
	"os/exec"
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

func TestInstallHookChainsStdinOriginalBeforeAcrossPolicy(t *testing.T) {
	directory := t.TempDir()
	hookPath := filepath.Join(directory, "pre-receive")
	received := filepath.Join(t.TempDir(), "original-stdin")
	original := "#!/bin/sh\ncat > " + shellQuote(received) + "\nexit 0\n"
	if err := os.WriteFile(hookPath, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := "#!/bin/sh\n# Across pre-receive\nwhile read old new ref; do\n  case \"$ref\" in\n    refs/heads/main) echo blocked >&2; exit 1;;\n  esac\ndone\nexit 0\n"
	if err := InstallHook(directory, "pre-receive", policy); err != nil {
		t.Fatal(err)
	}
	if code := runHookForTest(t, hookPath, "0000 1111 refs/heads/dev\n"); code != 0 {
		t.Fatalf("allowed push exited %d", code)
	}
	if data, err := os.ReadFile(received); err != nil || string(data) != "0000 1111 refs/heads/dev\n" {
		t.Fatalf("original hook did not receive stdin: %q %v", data, err)
	}
	if code := runHookForTest(t, hookPath, "0000 1111 refs/heads/main\n"); code != 1 {
		t.Fatalf("Across policy did not reject protected ref: exit %d", code)
	}
	if data, err := os.ReadFile(received); err != nil || string(data) != "0000 1111 refs/heads/main\n" {
		t.Fatalf("original hook did not run before the Across policy: %q %v", data, err)
	}
	rejecting := "#!/bin/sh\ncat >/dev/null\nexit 3\n"
	if err := os.WriteFile(hookPath+".across-orig", []byte(rejecting), 0o755); err != nil {
		t.Fatal(err)
	}
	if code := runHookForTest(t, hookPath, "0000 1111 refs/heads/dev\n"); code != 3 {
		t.Fatalf("original hook rejection was not propagated: exit %d", code)
	}
}

func TestInstallHookRunsOriginalAfterAcrossForNonStdinHooks(t *testing.T) {
	directory := t.TempDir()
	hookPath := filepath.Join(directory, "post-commit")
	log := filepath.Join(t.TempDir(), "order")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho original >> "+shellQuote(log)+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	across := "#!/bin/sh\n# Across automatic checkpoint hook\necho across >> " + shellQuote(log) + "\nexit 0\n"
	if err := InstallHook(directory, "post-commit", across); err != nil {
		t.Fatal(err)
	}
	if code := runHookForTest(t, hookPath, ""); code != 0 {
		t.Fatalf("post-commit exited %d", code)
	}
	data, err := os.ReadFile(log)
	if err != nil || string(data) != "across\noriginal\n" {
		t.Fatalf("hook order: %q %v", data, err)
	}
}

func runHookForTest(t *testing.T, hookPath, stdin string) int {
	t.Helper()
	command := exec.Command("sh", hookPath)
	command.Stdin = strings.NewReader(stdin)
	output, err := command.CombinedOutput()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	t.Fatalf("run hook: %v %s", err, output)
	return -1
}
