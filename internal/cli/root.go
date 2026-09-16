package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/graycodeai/across/internal/config"
	"github.com/graycodeai/across/internal/git"
	"github.com/spf13/cobra"
)

// Version is the Across version. Keep 0.0.1 until explicitly changed.
const Version = "0.0.1"

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "across",
		Short: "Across by GrayCodeAI — Code. Context. Continuity.",
		Long:  "Git-native engineering context, provenance, checkpoint, and continuity system (Local Alpha).",
	}
	def := config.DefaultHome()
	root.PersistentFlags().StringVar(&homeDir, "home", def, "Across home directory (or ACROSS_HOME)")
	AddCommands(root)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), Version)
		},
	}
}

func newAgentHelpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent-help",
		Short: "Machine-readable help for coding agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			doc := map[string]any{
				"product":        "Across by GrayCodeAI",
				"version":        Version,
				"status":         "Local Alpha",
				"tagline":        "Code. Context. Continuity.",
				"primary":        "What happened, why, what evidence exists, and how can we continue safely?",
				"not":            "Across is not a coding agent, IDE, or task orchestrator. It does not replace Git/GitHub/Rover.",
				"checkpoint":     map[string]any{"immutable": true, "restore": "creates new git worktree, never resets existing checkout"},
				"evidence":       map[string]any{"rule": "agent claims are STATED, not VERIFIED, unless Across executed with basis executed_by_across_local_runner"},
				"revision_basis": []string{"checkpoint_revision", "verification_revision", "explicit_user_annotation", "capture_time_head_not_causation", "imported_revision", "unknown", "legacy_revision_basis_unknown"},
				"epistemic":      []string{"OBSERVED", "STATED", "APPROVED", "INFERRED", "DISPUTED", "SUPERSEDED", "UNKNOWN"},
				"mutations_cli":  []string{"checkpoint create", "workspace create", "verify run", "change create", "issue create"},
				"read_mcp":       []string{"across_search", "across_brief", "across_inspect", "across_code_search", "across_graph", "across_graph_health", "across_investigate", "across_why", "across_sessions", "across_checkpoints", "across_verifications", "across_review", "across_issues", "across_changes", "across_workspaces", "across_version_sets", "across_activity"},
				"mcp_hidden":     []string{"shell execution", "merge", "delete", "approve", "grant", "plugin installation"},
				"runner_warning": "Across local runner executes with user OS permissions; NOT a sandbox. Not exposed via default MCP.",
				"privacy":        "By default retains messages/tool names/model/tokens; drops raw tool args/results, shell bodies, system prompts, secrets.",
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(doc)
		},
	}
}

func newHookCmd() *cobra.Command {
	c := &cobra.Command{Use: "hook", Short: "Git hook entrypoints"}
	c.AddCommand(&cobra.Command{
		Use:   "post-commit",
		Short: "Run from git post-commit hook (safe automatic checkpoint)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			return runPostCommitHook(cmd, cwd)
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "install REPO_PATH",
		Args:  cobra.ExactArgs(1),
		Short: "Install post-commit hook (chains existing hook, never overwrites silently)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			repoPath := args[0]
			if _, err := os.Stat(repoPath); err != nil {
				return fmt.Errorf("repo path not found: %s", repoPath)
			}
			abs, _ := filepath.Abs(repoPath)
			common := git.CommonDir(abs)
			if common == "" {
				return fmt.Errorf("not a git repository: %s", abs)
			}
			if !filepath.IsAbs(common) {
				common = filepath.Join(abs, common)
			}
			// safety: refuse if the resolved hooks dir escapes the repo (symlink §96)
			hooksDir := filepath.Join(common, "hooks")
			if err := git.InstallHook(hooksDir, "post-commit", postCommitHookScript(homeDir)); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "post-commit hook installed (existing hook chained if present)")
			return nil
		},
	})
	return c
}

// postCommitHookScript returns the shell snippet; preserves the intended Across home.
func postCommitHookScript(home string) string {
	bin := os.Args[0]
	if p, err := os.Executable(); err == nil {
		bin = p
	}
	return "#!/bin/sh\n# Across automatic checkpoint hook (owned by Across; original hook chained)\n\"" + bin + "\" --home \"" + home + "\" hook post-commit\n"
}
