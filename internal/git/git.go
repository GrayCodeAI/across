package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Run runs a git command in dir.
func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Head returns HEAD SHA for path (empty if unborn/no commits).
func Head(path string) string {
	out, err := Run(path, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CommonDir returns git common dir (for worktree/bare detection).
func CommonDir(path string) string {
	out, err := Run(path, "rev-parse", "--git-common-dir")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// IsRepo reports whether path is inside a git work tree.
func IsRepo(path string) bool {
	_, err := Run(path, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// CurrentBranch returns current branch or empty if detached.
func CurrentBranch(path string) string {
	out, err := Run(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	if out == "HEAD" {
		return ""
	}
	return out
}

// ShowToplevel returns the top-level working tree dir for cwd, or "" if not in a repo.
func ShowToplevel(cwd string) string {
	out, err := Run(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
