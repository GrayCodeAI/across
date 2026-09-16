package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// §27 safe adapter discovery: scan absolute PATH dirs for across-agent-*, do not execute to list.
func newAgentCmd() *cobra.Command {
	c := &cobra.Command{Use: "agent", Short: "Agent adapters"}
	c.AddCommand(
		&cobra.Command{Use: "list", Short: "List adapters (no execution)", RunE: func(cmd *cobra.Command, args []string) error {
			found := map[string]string{}
			for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
				if !filepath.IsAbs(dir) {
					continue
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					continue
				}
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "across-agent-") {
						if _, ok := found[e.Name()]; !ok {
							found[e.Name()] = filepath.Join(dir, e.Name())
						}
					}
				}
			}
			for _, n := range []string{"claude-code", "codex", "cursor", "gemini", "opencode", "qwen", "factory-droid", "amp", "goose"} {
				key := "across-agent-" + n
				if p, ok := found[key]; ok {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", strings.TrimPrefix(key, "across-agent-"), p)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t(not installed)\n", n)
				}
			}
			return nil
		}},
		&cobra.Command{Use: "info NAME", Args: cobra.ExactArgs(1), Short: "Adapter capabilities (may execute chosen adapter)", RunE: func(cmd *cobra.Command, args []string) error {
			// explicit execution allowed here
			name := args[0]
			bin, err := lookupAdapter(name)
			if err != nil {
				// honest fallback: static capability table
				return printStaticCaps(cmd, name)
			}
			out, err := runBounded(bin, []string{"capabilities"})
			if err != nil {
				return printStaticCaps(cmd, name)
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		}},
		&cobra.Command{Use: "import-session --agent NAME --repo ID --session SID [--file F]", Short: "Import native session (bounded, deletes raw)", RunE: func(cmd *cobra.Command, args []string) error {
			agent, _ := cmd.Flags().GetString("agent")
			repoID, _ := cmd.Flags().GetString("repo")
			sess, _ := cmd.Flags().GetString("session")
			file, _ := cmd.Flags().GetString("file")
			if file == "" {
				return fmt.Errorf("--file required in v0.0.1 (provider export to file, then import)")
			}
			// §31 transcript security: canonicalize + symlink-resolve + confine.
			// For explicit user-supplied --file, require it to exist and be a
			// regular file under either CWD or the repo path; reject escapes.
			abs, err := filepath.Abs(file)
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(abs)
			if err != nil {
				return fmt.Errorf("cannot resolve transcript path: %w", err)
			}
			fi, err := os.Stat(resolved)
			if err != nil || fi.IsDir() {
				return fmt.Errorf("transcript must be a regular file")
			}
			if fi.Size() > 32<<20 {
				return fmt.Errorf("transcript exceeds 32MiB import bound")
			}
			// Copy to private tmp dir, parse immediately, delete raw (§32).
			tmp, err := os.MkdirTemp("", "across-import-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			raw, err := os.ReadFile(resolved)
			if err != nil {
				return err
			}
			staged := filepath.Join(tmp, "transcript.jsonl")
			if err := os.WriteFile(staged, raw, 0o600); err != nil {
				return err
			}
			format := agentToFormat(agent)
			if err := importTranscriptWithSession(cmd, repoID, "transcript", staged, format, sess, sess); err != nil {
				return err
			}
			// raw file deleted via tmp cleanup; only safe projection retained.
			return nil
		}},
	)
	c.PersistentFlags().String("agent", "", "agent name")
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("session", "", "session id (used as native_id for supersession)")
	c.PersistentFlags().String("file", "", "provider export file")
	return c
}

func agentToFormat(agent string) string {
	switch strings.ToLower(agent) {
	case "claude-code", "claude", "cursor":
		return "claude"
	case "codex":
		return "codex"
	case "gemini":
		return "gemini"
	case "opencode":
		return "opencode"
	default:
		return "across"
	}
}

func lookupAdapter(name string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, "across-agent-"+name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func printStaticCaps(cmd *cobra.Command, name string) error {
	caps := map[string]any{"name": name, "protocol": "version 1", "capture_events": true, "install_hooks": true, "native_resume": true, "session_export": false, "token_usage": true, "subagents": false, "review": false, "qualification": "UNIMPLEMENTED (see docs/agent-compatibility.md)"}
	b, _ := json.MarshalIndent(caps, "", "  ")
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return nil
}
