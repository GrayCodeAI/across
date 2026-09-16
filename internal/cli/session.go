package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/graycodeai/across/internal/event"
	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/redact"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

// Canonical event types (§22)
var canonicalEvents = map[string]bool{
	"SessionStart": true, "TurnStart": true, "UserPrompt": true, "AssistantMessage": true,
	"ToolUse": true, "SubagentStart": true, "SubagentEnd": true, "TurnEnd": true,
	"Compaction": true, "SessionEnd": true,
}

func newSessionCmd() *cobra.Command {
	c := &cobra.Command{Use: "session", Short: "Sessions"}
	c.AddCommand(
		&cobra.Command{Use: "start --repo ID --agent NAME [--native-id N]", Short: "Start session", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			agent, _ := cmd.Flags().GetString("agent")
			native, _ := cmd.Flags().GetString("native-id")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			if _, _, err := repoMustExist(db, repoID); err != nil {
				return err
			}
			id := store.NewID("sess")
			now := store.NowUTC()
			if _, err := db.Exec(`INSERT INTO sessions(id, repository_id, agent, native_session_id, state, started_at, last_event_at) VALUES(?,?,?,?,?,?,?)`,
				id, repoID, agent, native, "active", now, now); err != nil {
				return err
			}
			// source record for session start
			src := store.NewID("src")
			var canon string
			_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon)
			head := git.Head(canon)
			basis := "capture_time_head_not_causation"
			if head == "" {
				basis = "unknown"
			}
			_, _ = db.Exec(`INSERT INTO sources(id, repository_id, kind, origin, native_id, captured_at, revision, revision_basis) VALUES(?,?,?,?,?,?,?,?)`,
				src, repoID, "session", agent, native, now, head, basis)
			logActivity(db, "session.start", repoID, id, "session start "+agent)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "list [--repo ID]", Short: "List sessions", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			q := `SELECT id, repository_id, agent, state, started_at FROM sessions ORDER BY started_at`
			var rows interface {
			}
			_ = rows
			var r *struct{}
			_ = r
			if repoID != "" {
				rs, err := db.Query(`SELECT id, repository_id, agent, state, started_at FROM sessions WHERE repository_id=? ORDER BY started_at`, repoID)
				if err != nil {
					return err
				}
				defer rs.Close()
				for rs.Next() {
					var id, rp, ag, st, sa string
					rs.Scan(&id, &rp, &ag, &st, &sa)
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", id, rp, ag, st, sa)
				}
				return nil
			}
			rs, err := db.Query(q)
			if err != nil {
				return err
			}
			defer rs.Close()
			for rs.Next() {
				var id, rp, ag, st, sa string
				rs.Scan(&id, &rp, &ag, &st, &sa)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", id, rp, ag, st, sa)
			}
			return nil
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show session + events", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var id, rp, ag, nat, st, sa, ea, le, lcp string
			if err := db.QueryRow(`SELECT id, repository_id, agent, native_session_id, state, started_at, ended_at, last_event_at, latest_checkpoint_id FROM sessions WHERE id=?`, args[0]).Scan(&id, &rp, &ag, &nat, &st, &sa, &ea, &le, &lcp); err != nil {
				return fmt.Errorf("session not found")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nrepo: %s\nagent: %s\nnative: %s\nstate: %s\nstarted: %s\nended: %s\nlast_event: %s\nlatest_checkpoint: %s\n", id, rp, ag, nat, st, sa, ea, le, lcp)
			return nil
		}},
		&cobra.Command{Use: "close ID", Args: cobra.ExactArgs(1), Short: "Close session", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			if _, err := db.Exec(`UPDATE sessions SET state='closed', ended_at=? WHERE id=?`, store.NowUTC(), args[0]); err != nil {
				return err
			}
			logActivity(db, "session.close", "", args[0], "session close")
			fmt.Fprintln(cmd.OutOrStdout(), "closed")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("agent", "", "agent name")
	c.PersistentFlags().String("native-id", "", "native session id")
	return c
}

func newSourceCmd() *cobra.Command {
	c := &cobra.Command{Use: "source", Short: "Sources and events"}
	c.AddCommand(
		&cobra.Command{Use: "list [--repo ID]", Short: "List sources (excludes deleted)", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var rows, err2 = db.Query(`SELECT id, kind, origin, captured_at, revision, revision_basis FROM sources WHERE deleted_at='' ORDER BY captured_at`)
			if repoID != "" {
				rows, err2 = db.Query(`SELECT id, kind, origin, captured_at, revision, revision_basis FROM sources WHERE deleted_at='' AND repository_id=? ORDER BY captured_at`, repoID)
			}
			if err2 != nil {
				return err2
			}
			defer rows.Close()
			for rows.Next() {
				var id, k, o, ca, rev, basis string
				rows.Scan(&id, &k, &o, &ca, &rev, &basis)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\t%s\n", id, k, o, rev, basis, ca)
			}
			return nil
		}},
		&cobra.Command{Use: "import --repo ID --kind KIND --file F [--format across|claude|cursor|codex|gemini|opencode]", Short: "Import transcript events", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			kind, _ := cmd.Flags().GetString("kind")
			file, _ := cmd.Flags().GetString("file")
			format, _ := cmd.Flags().GetString("format")
			return importTranscript(cmd, repoID, kind, file, format, "")
		}},
		&cobra.Command{Use: "inspect SOURCE_ID", Args: cobra.ExactArgs(1), Short: "Inspect source events", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, err := db.Query(`SELECT seq, event_type, agent, tool_name, substr(body_text,1,300), occurred_at FROM source_events WHERE source_id=? ORDER BY seq`, args[0])
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var seq int
				var et, ag, tn, body, at string
				rows.Scan(&seq, &et, &ag, &tn, &body, &at)
				fmt.Fprintf(cmd.OutOrStdout(), "#%d %s agent=%s tool=%s at=%s\n  %s\n", seq, et, ag, tn, at, body)
			}
			return nil
		}},
		&cobra.Command{Use: "delete SOURCE_ID", Args: cobra.ExactArgs(1), Short: "Delete source history (tombstoned)", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			now := store.NowUTC()
			if _, err := db.Exec(`UPDATE sources SET deleted_at=? WHERE id=?`, now, args[0]); err != nil {
				return err
			}
			_, _ = db.Exec(`DELETE FROM source_events WHERE source_id=?`, args[0])
			_, _ = db.Exec(`DELETE FROM search_index WHERE ref_id=?`, args[0])
			_, _ = db.Exec(`INSERT INTO tombstones(id, target_kind, target_id, reason, created_at) VALUES(?,?,?,?,?)`, store.NewID("tmb"), "source", args[0], "user delete", now)
			fmt.Fprintln(cmd.OutOrStdout(), "deleted")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("kind", "note", "source kind")
	c.PersistentFlags().String("file", "", "JSONL file")
	c.PersistentFlags().String("format", "across", "across|claude|cursor|codex|gemini|opencode")
	return c
}

const maxJSONLLine = 1 << 20 // 1MiB §97

// importTranscript parses a transcript file (native or Across format),
// collapses streaming partials (§33), and stores an immutable source snapshot.
// If nativeID != "", older sources with the same native_id are linked via
// superseded_by (§20) and only the new snapshot participates in retrieval.
func importTranscript(cmd *cobra.Command, repoID, kind, file, format, nativeID string) error {
	return importTranscriptWithSession(cmd, repoID, kind, file, format, nativeID, "")
}

func importJSONL(cmd *cobra.Command, repoID, kind, file string) error {
	return importTranscript(cmd, repoID, kind, file, "across", "")
}

func importTranscriptWithSession(cmd *cobra.Command, repoID, kind, file, format, nativeID, sessionID string) error {
	db, _, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if _, _, err := repoMustExist(db, repoID); err != nil {
		return err
	}
	f, err := os.Open(filepath.Clean(file))
	if err != nil {
		return err
	}
	defer f.Close()
	src := store.NewID("src")
	now := store.NowUTC()
	var canon string
	_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon)
	head := git.Head(canon)
	basis := "imported_revision"
	if head == "" {
		basis = "unknown"
		head = ""
	}
	if _, err := db.Exec(`INSERT INTO sources(id, repository_id, kind, origin, captured_at, revision, revision_basis) VALUES(?,?,?,?,?,?,?)`,
		src, repoID, kind, file, now, head, basis); err != nil {
		return err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxJSONLLine+1024)
	var parsed []event.Parsed
	rawLines := 0
	for sc.Scan() {
		line := sc.Text()
		if len(line) > maxJSONLLine {
			fmt.Fprintf(cmd.OutOrStdout(), "WARN truncated oversize line %d\n", rawLines)
			line = line[:maxJSONLLine]
		}
		var p event.Parsed
		var ok bool
		switch format {
		case "claude", "cursor":
			p, ok = event.ParseClaudeJSONL(line, format)
		case "codex":
			p, ok = event.ParseCodexRollout(line)
		case "opencode":
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				continue
			}
			p, ok = event.ParseOpenCodeExport(obj)
		case "gemini":
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				continue
			}
			p, ok = event.ParseGeminiJSON(obj)
		default:
			p, ok = event.ParseLine(line)
		}
		if !ok {
			continue // skip malformed per fuzz guidance
		}
		parsed = append(parsed, p)
		rawLines++
		if rawLines > 100000 {
			break // §97 max import guard
		}
	}
	// Collapse streaming partials BEFORE storage (§33, §108): last text wins,
	// max tokens win, never summed.
	deduped := event.Dedup(parsed)
	var combined strings.Builder
	seq := 0
	agent := ""
	for _, p := range deduped {
		if p.Agent != "" {
			agent = p.Agent
		}
		red, _ := redact.Redact(p.Text)
		_, _ = db.Exec(`INSERT INTO source_events(id, source_id, seq, event_type, provider_event_type, body_text, agent, model, tool_name, turn_id, parent_session_id, input_tokens, output_tokens, cached_tokens, recorded_cost, occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			store.NewID("ev"), src, seq, p.Type, p.Provider, red, p.Agent, p.Model, p.Tool, p.TurnID, p.ParentSID, p.In, p.Out, p.Cached, p.Cost, now)
		combined.WriteString(red + "\n")
		seq++
	}
	inT, outT := event.TotalTokens(parsed)
	_ = inT
	_ = outT
	// Snapshot supersession (§20): link older live snapshots with same native_id.
	if nativeID != "" {
		rows, _ := db.Query(`SELECT id FROM sources WHERE repository_id=? AND native_id=? AND superseded_by='' AND deleted_at='' AND id != ?`, repoID, nativeID, src)
		if rows != nil {
			for rows.Next() {
				var old string
				rows.Scan(&old)
				_, _ = db.Exec(`UPDATE sources SET superseded_by=? WHERE id=?`, src, old)
				_, _ = db.Exec(`DELETE FROM search_index WHERE kind='source' AND ref_id=?`, old)
			}
			rows.Close()
		}
		_, _ = db.Exec(`UPDATE sources SET native_id=? WHERE id=?`, nativeID, src)
	}
	if sessionID != "" {
		_, _ = db.Exec(`UPDATE sessions SET last_event_at=? WHERE id=?`, now, sessionID)
	}
	_ = agent
	indexDoc(db, "source", src, repoID, kind+" import", combined.String())
	logActivity(db, "source.import", repoID, src, fmt.Sprintf("imported %d events (%d raw, format=%s)", seq, rawLines, format))
	fmt.Fprintln(cmd.OutOrStdout(), src)
	return nil
}

func intFromKey(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func intFrom(m map[string]any, k string) int { return intFromKey(m[k]) }

var _ = strings.Contains
