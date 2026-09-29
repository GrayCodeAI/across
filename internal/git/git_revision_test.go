package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveRevision(t *testing.T) {
	work := t.TempDir()
	run := func(args ...string) {
		command := exec.Command("git", args...)
		command.Dir = work
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	resolved, err := ResolveRevision(work, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if resolved == "" || resolved != Head(work) {
		t.Fatalf("resolved revision: %q head=%q", resolved, Head(work))
	}
	if _, err := ResolveRevision(work, "missing-revision"); err == nil {
		t.Fatal("unknown revision was accepted")
	}
}
