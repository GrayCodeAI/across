package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/graycodeai/across/internal/adapter"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	c := &cobra.Command{Use: "agent", Short: "Agent protocol shells"}
	c.AddCommand(
		&cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List adapters (no execution)", RunE: func(cmd *cobra.Command, args []string) error {
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
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", n, p)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t(not installed)\n", n)
				}
			}
			return nil
		}},
		&cobra.Command{Use: "info NAME", Args: cobra.ExactArgs(1), Short: "Query adapter capabilities", RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			bin, err := lookupAdapter(name)
			if err != nil {
				return err
			}
			out, err := runBounded(bin, []string{"capabilities"})
			if err != nil {
				return fmt.Errorf("query adapter %q: %w", name, err)
			}
			var caps adapter.Capabilities
			if err := json.Unmarshal([]byte(out), &caps); err != nil {
				return fmt.Errorf("decode adapter %q capabilities: %w", name, err)
			}
			if caps.Name != name {
				return fmt.Errorf("adapter identity mismatch: requested %q, received %q", name, caps.Name)
			}
			if caps.Protocol != adapter.Protocol {
				return fmt.Errorf("unsupported adapter protocol %q", caps.Protocol)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(caps)
		}},
		&cobra.Command{Use: "import-session --agent NAME --repo ID --session SID --file FILE", Args: cobra.NoArgs, Short: "Import a supported session export", PreRunE: requiredFlags("agent", "repo", "session", "file"), RunE: func(cmd *cobra.Command, args []string) error {
			agent, _ := cmd.Flags().GetString("agent")
			repoID, _ := cmd.Flags().GetString("repo")
			sess, _ := cmd.Flags().GetString("session")
			file, _ := cmd.Flags().GetString("file")
			format, err := agentToFormat(agent)
			if err != nil {
				return err
			}
			resolved, err := requireExistingFile(file)
			if err != nil {
				return err
			}
			fi, err := os.Stat(resolved)
			if err != nil {
				return err
			}
			if fi.Size() > 32<<20 {
				return fmt.Errorf("transcript exceeds 32MiB import bound")
			}
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
			return importTranscriptWithSessionAt(cmd, repoID, "transcript", staged, format, sess, sess, resolved)
		}},
	)
	c.PersistentFlags().String("agent", "", "agent or export format")
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("session", "", "session id (used as native_id for supersession)")
	c.PersistentFlags().String("file", "", "provider export file")
	return c
}

func agentToFormat(agent string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "across":
		return "across", nil
	case "claude-code", "claude", "cursor":
		return "claude", nil
	case "codex":
		return "codex", nil
	case "gemini":
		return "gemini", nil
	case "opencode":
		return "opencode", nil
	default:
		return "", invalidArgument("unsupported import format %q", agent)
	}
}

func lookupAdapter(name string) (string, error) {
	if !adapter.IsKnownProvider(name) {
		return "", invalidArgument("unknown adapter %q", name)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, "across-agent-"+name)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", notFound("adapter %q is not installed", name)
}
