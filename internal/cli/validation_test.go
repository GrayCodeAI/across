package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandTreeHasArgumentContracts(t *testing.T) {
	root := NewRoot()
	leaves := 0
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Runnable() {
			leaves++
			if cmd.Args == nil {
				t.Fatalf("runnable command has no argument contract: %s", cmd.CommandPath())
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	if leaves < 90 {
		t.Fatalf("unexpected command count: %d", leaves)
	}
}

func TestRequiredFlagErrorIsTyped(t *testing.T) {
	home := t.TempDir()
	err := executeForTest(t, "--home", home, "session", "start")
	if ExitCode(err) != 2 {
		t.Fatalf("exit %d: %v", ExitCode(err), err)
	}
	if !strings.Contains(FormatError(err), "--repo is required") {
		t.Fatalf("error: %s", FormatError(err))
	}
}

func TestUnknownEnumIsRejectedBeforeStoreOpen(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "events.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := executeForTest(t, "--home", home, "source", "import", "--repo", "repo_missing", "--file", file, "--format", "unknown")
	if ExitCode(err) != 2 {
		t.Fatalf("exit %d: %v", ExitCode(err), err)
	}
	if !strings.Contains(FormatError(err), "--format must be one of") {
		t.Fatalf("error: %s", FormatError(err))
	}
}

func TestUnknownAdapterFormatIsRejected(t *testing.T) {
	if _, err := agentToFormat("qwen"); err == nil {
		t.Fatal("unsupported adapter format was accepted")
	}
	if format, err := agentToFormat("cursor"); err != nil || format != "claude" {
		t.Fatalf("cursor mapping: %q %v", format, err)
	}
}

func TestMCPInventoryAndUnsupportedTools(t *testing.T) {
	expected := []string{
		"across_search",
		"across_sessions",
		"across_checkpoints",
		"across_verifications",
		"across_issues",
		"across_changes",
		"across_activity",
		"across_graph_health",
	}
	names := mcpToolNames()
	if strings.Join(names, ",") != strings.Join(expected, ",") {
		t.Fatalf("tool inventory: %v", names)
	}
	if _, isError := mcpDispatch("across_brief", nil); !isError {
		t.Fatal("placeholder tool was accepted")
	}
	if _, isError := mcpDispatch("merge", nil); !isError {
		t.Fatal("mutation tool was accepted")
	}
}

func TestErrorFormatting(t *testing.T) {
	err := WrapError(invalidArgument("bad value"))
	if ExitCode(err) != 2 || FormatError(err) != "across: invalid_argument: bad value" {
		t.Fatalf("formatted error: %d %s", ExitCode(err), FormatError(err))
	}
}

func executeForTest(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRoot()
	root.SetArgs(args)
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	return WrapError(root.Execute())
}
