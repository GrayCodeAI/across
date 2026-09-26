package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_CLIValidationExitCodes(t *testing.T) {
	bin := buildAcross(t)
	home := filepath.Join(t.TempDir(), "home")
	command := exec.Command(bin, "--home", home, "session", "start")
	output, err := command.CombinedOutput()
	if exitCode(err) != 2 || !strings.Contains(string(output), "invalid_argument") {
		t.Fatalf("missing required flags: exit=%d output=%s", exitCode(err), output)
	}
	file := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command = exec.Command(bin, "--home", home, "source", "import", "--repo", "repo_missing", "--file", file, "--format", "bogus")
	output, err = command.CombinedOutput()
	if exitCode(err) != 2 || !strings.Contains(string(output), "--format must be one of") {
		t.Fatalf("invalid enum: exit=%d output=%s", exitCode(err), output)
	}
	command = exec.Command(bin, "--home", home, "backup", "restore", filepath.Join(home, "missing.tgz"))
	output, err = command.CombinedOutput()
	if exitCode(err) != 2 || !strings.Contains(string(output), "--target-home is required") {
		t.Fatalf("restore target validation: exit=%d output=%s", exitCode(err), output)
	}
}

func TestE2E_VerificationFailureIsNotSuccess(t *testing.T) {
	bin := buildAcross(t)
	home := filepath.Join(t.TempDir(), "home")
	work := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, "", "init", work)
	git(t, work, "config", "user.email", "test@example.com")
	git(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "initial")
	repoID := run(t, home, bin, "repo", "add", work)
	command := exec.Command(bin, "--home", home, "verify", "run", "--repo", repoID, "--name", "fails", "--", "sh", "-c", "exit 7")
	output, err := command.CombinedOutput()
	if exitCode(err) != 5 || !strings.Contains(string(output), "exit=7") || !strings.Contains(string(output), "operation_failed") {
		t.Fatalf("verification failure: exit=%d output=%s", exitCode(err), output)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return -1
}
