package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
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
		&cobra.Command{Use: "start --repo ID --agent NAME [--native-id N]", Short: "Start session", PreRunE: requiredFlags("repo", "agent"), RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			agent, _ := cmd.Flags().GetString("agent")
			native, _ := cmd.Flags().GetString("native-id")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			canon, _, err := repoMustExist(db, repoID)
			if err != nil {
				return err
			}
			if native != "" {
				if err := rejectTombstonedSource(db, repoID, "session", agent, native); err != nil {
					return err
				}
			}
			id := store.NewID("sess")
			now := store.NowUTC()
			head := git.Head(canon)
			basis := "capture_time_head_not_causation"
			if head == "" {
				basis = "unknown"
			}
			src := store.NewID("src")
			if err := withTx(db, func(tx sqlRunner) error {
				if native != "" {
					if err := rejectTombstonedSource(tx, repoID, "session", agent, native); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(`INSERT INTO sessions(id, repository_id, agent, native_session_id, state, started_at, last_event_at, parent_session_id, fork_type, event_cursor, provider, lineage_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
					id, repoID, agent, native, "active", now, now, "", "root", 0, agent, 1); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO sources(id, repository_id, kind, origin, native_id, session_id, captured_at, revision, revision_basis, parser_version, redaction_status, import_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
					src, repoID, "session", agent, native, id, now, head, basis, "session-v1", "not_applicable", "complete"); err != nil {
					return err
				}
				return logActivityTx(tx, "session.start", repoID, id, "session start "+agent)
			}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "fork PARENT --repo ID --agent NAME [--native-id N]", Args: cobra.ExactArgs(1), Short: "Fork a session lineage", PreRunE: requiredFlags("repo", "agent"), RunE: func(cmd *cobra.Command, args []string) error {
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
			parent, err := sessionMustBelong(db, repoID, args[0], false)
			if err != nil {
				return err
			}
			id := store.NewID("sess")
			now := store.NowUTC()
			if err := withTx(db, func(tx sqlRunner) error {
				if _, err := tx.Exec(`INSERT INTO sessions(id, repository_id, agent, native_session_id, state, started_at, last_event_at, parent_session_id, fork_type, event_cursor, provider, lineage_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
					id, repoID, agent, native, "active", now, now, parent.id, "fork", 0, agent, 2); err != nil {
					return err
				}
				return logActivityTx(tx, "session.fork", repoID, id, "forked from "+parent.id)
			}); err != nil {
				return err
			}
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
			var id, rp, ag, nat, st, sa, ea, le, lcp, parent, fork, provider string
			var cursor, lineage int
			if err := db.QueryRow(`SELECT id, repository_id, agent, native_session_id, state, started_at, ended_at, last_event_at, latest_checkpoint_id, parent_session_id, fork_type, event_cursor, provider, lineage_version FROM sessions WHERE id=?`, args[0]).Scan(&id, &rp, &ag, &nat, &st, &sa, &ea, &le, &lcp, &parent, &fork, &cursor, &provider, &lineage); err != nil {
				return notFound("session %q not found", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nrepo: %s\nagent: %s\nnative: %s\nstate: %s\nstarted: %s\nended: %s\nlast_event: %s\nlatest_checkpoint: %s\nparent: %s\nfork_type: %s\nevent_cursor: %d\nprovider: %s\nlineage_version: %d\n", id, rp, ag, nat, st, sa, ea, le, lcp, parent, fork, cursor, provider, lineage)
			return nil
		}},
		&cobra.Command{Use: "close ID", Args: cobra.ExactArgs(1), Short: "Close session", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := withTx(db, func(tx sqlRunner) error {
				result, err := tx.Exec(`UPDATE sessions SET state='closed', ended_at=? WHERE id=?`, store.NowUTC(), args[0])
				if err != nil {
					return err
				}
				affected, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if affected == 0 {
					return notFound("session %q not found", args[0])
				}
				return logActivityTx(tx, "session.close", "", args[0], "session close")
			}); err != nil {
				return err
			}
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
		&cobra.Command{Use: "import --repo ID --kind KIND --file F [--format across|claude|cursor|codex|gemini|opencode] [--native-id N]", Short: "Import transcript events", PreRunE: requiredFlags("repo", "file"), RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			kind, _ := cmd.Flags().GetString("kind")
			file, _ := cmd.Flags().GetString("file")
			format, _ := cmd.Flags().GetString("format")
			nativeID, _ := cmd.Flags().GetString("native-id")
			if err := requireOneOf("format", format, "across", "claude", "cursor", "codex", "gemini", "opencode"); err != nil {
				return err
			}
			return importTranscript(cmd, repoID, kind, file, format, nativeID)
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
			seen := false
			for rows.Next() {
				seen = true
				var seq int
				var et, ag, tn, body, at string
				if err := rows.Scan(&seq, &et, &ag, &tn, &body, &at); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "#%d %s agent=%s tool=%s at=%s\n  %s\n", seq, et, ag, tn, at, body)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if !seen {
				return notFound("source %q has no events or does not exist", args[0])
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
			if err := withTx(db, func(tx sqlRunner) error {
				result, err := tx.Exec(`UPDATE sources SET deleted_at=? WHERE id=? AND deleted_at=''`, now, args[0])
				if err != nil {
					return err
				}
				affected, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if affected == 0 {
					return notFound("source %q not found", args[0])
				}
				if _, err := tx.Exec(`DELETE FROM source_events WHERE source_id=?`, args[0]); err != nil {
					return err
				}
				if _, err := tx.Exec(`DELETE FROM search_index WHERE ref_id=?`, args[0]); err != nil {
					return err
				}
				_, err = tx.Exec(`INSERT INTO tombstones(id, target_kind, target_id, reason, created_at) VALUES(?,?,?,?,?)`, store.NewID("tmb"), "source", args[0], "user delete", now)
				return err
			}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "deleted")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("kind", "note", "source kind")
	c.PersistentFlags().String("file", "", "JSONL file")
	c.PersistentFlags().String("format", "across", "across|claude|cursor|codex|gemini|opencode")
	c.PersistentFlags().String("native-id", "", "native source identity")
	return c
}

const maxJSONLLine = 1 << 20 // 1MiB §97

// importTranscript parses a transcript file (native or Across format),
// collapses streaming partials (§33), and stores a redacted source projection.
// If nativeID != "", older sources with the same native_id are linked via
// superseded_by (§20) and only the new snapshot participates in retrieval.
func importTranscript(cmd *cobra.Command, repoID, kind, file, format, nativeID string) error {
	return importTranscriptWithSession(cmd, repoID, kind, file, format, nativeID, "")
}

func importJSONL(cmd *cobra.Command, repoID, kind, file string) error {
	return importTranscript(cmd, repoID, kind, file, "across", "")
}

func importTranscriptWithSession(cmd *cobra.Command, repoID, kind, file, format, nativeID, sessionID string) error {
	return importTranscriptWithSessionAt(cmd, repoID, kind, file, format, nativeID, sessionID, "")
}

func importTranscriptWithSessionAt(cmd *cobra.Command, repoID, kind, file, format, nativeID, sessionID, originPath string) error {
	if err := requireOneOf("format", format, "across", "claude", "cursor", "codex", "gemini", "opencode"); err != nil {
		return err
	}
	file, err := requireExistingFile(file)
	if err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxTranscriptSize {
		return fmt.Errorf("transcript exceeds 32MiB import bound")
	}
	sourceHash, sourceSize, err := digestFile(file)
	if err != nil {
		return err
	}
	parserVersion := "parser-v1:" + format
	db, _, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	canon, _, err := repoMustExist(db, repoID)
	if err != nil {
		return err
	}
	confinementPath := file
	if originPath != "" {
		confinementPath = originPath
	}
	if !pathWithin(canon, confinementPath) {
		return invalidArgument("transcript path must be within the repository root")
	}
	if sessionID != "" {
		if _, err := sessionMustBelong(db, repoID, sessionID, true); err != nil {
			return err
		}
	}
	if err := rejectTombstonedSource(db, repoID, kind, file, nativeID); err != nil {
		return err
	}
	deduped, combined, rawLines, err := parseTranscript(cmd, f, format)
	if err != nil {
		return err
	}
	src := store.NewID("src")
	now := store.NowUTC()
	head := git.Head(canon)
	basis := "imported_revision"
	if head == "" {
		basis = "unknown"
	}
	seq := len(deduped)
	if err := withTx(db, func(tx sqlRunner) error {
		if err := rejectTombstonedSource(tx, repoID, kind, file, nativeID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sources(id, repository_id, kind, origin, native_id, session_id, captured_at, revision, revision_basis, content_hash, size_bytes, parser_version, redaction_status, import_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			src, repoID, kind, file, nativeID, sessionID, now, head, basis, sourceHash, sourceSize, parserVersion, "best_effort_redacted", "complete"); err != nil {
			return err
		}
		for i, p := range deduped {
			eventHash := digestBytes([]byte(p.Text))
			if _, err := tx.Exec(`INSERT INTO source_events(id, source_id, seq, event_type, provider_event_type, body_text, agent, model, tool_name, turn_id, parent_session_id, input_tokens, output_tokens, cached_tokens, recorded_cost, occurred_at, native_event_id, parent_event_id, occurrence_index, content_hash, captured_at, parser_version, redaction_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				stableContractID("ev", src, fmt.Sprint(i), p.MessageID, p.Type), src, i, p.Type, p.Provider, p.Text, p.Agent, p.Model, p.Tool, p.TurnID, p.ParentSID, p.In, p.Out, p.Cached, p.Cost, now, p.MessageID, p.ParentSID, i, eventHash, now, parserVersion, "best_effort_redacted"); err != nil {
				return err
			}
		}
		if nativeID != "" {
			rows, err := tx.Query(`SELECT id FROM sources WHERE repository_id=? AND native_id=? AND superseded_by='' AND deleted_at='' AND id != ?`, repoID, nativeID, src)
			if err != nil {
				return err
			}
			var oldIDs []string
			for rows.Next() {
				var old string
				if err := rows.Scan(&old); err != nil {
					rows.Close()
					return err
				}
				oldIDs = append(oldIDs, old)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			for _, old := range oldIDs {
				result, err := tx.Exec(`UPDATE sources SET superseded_by=? WHERE id=? AND superseded_by='' AND deleted_at=''`, src, old)
				if err != nil {
					return err
				}
				affected, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if affected != 1 {
					return conflict("source %q changed during import", old)
				}
				if _, err := tx.Exec(`DELETE FROM search_index WHERE kind='source' AND ref_id=?`, old); err != nil {
					return err
				}
			}
		}
		if sessionID != "" {
			result, err := tx.Exec(`UPDATE sessions SET last_event_at=? WHERE id=? AND state='active'`, now, sessionID)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return conflict("session %q is not active", sessionID)
			}
		}
		if err := indexDocTx(tx, "source", src, repoID, kind+" import", combined); err != nil {
			return err
		}
		return logActivityTx(tx, "source.import", repoID, src, fmt.Sprintf("imported %d events (%d raw, format=%s)", seq, rawLines, format))
	}); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), src)
	return nil
}

const maxTranscriptSize int64 = 32 << 20

func parseTranscript(cmd *cobra.Command, f *os.File, format string) ([]event.Parsed, string, int, error) {
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
			continue
		}
		parsed = append(parsed, p)
		rawLines++
		if rawLines > 100000 {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, "", 0, err
	}
	deduped := event.Dedup(parsed)
	var combined strings.Builder
	for i := range deduped {
		red, _ := redact.Redact(deduped[i].Text)
		deduped[i].Text = red
		combined.WriteString(red + "\n")
	}
	return deduped, combined.String(), rawLines, nil
}

func rejectTombstonedSource(q rowsQuerier, repoID, kind, origin, nativeID string) error {
	rows, err := q.Query(`SELECT DISTINCT s.id, s.native_id, s.kind, s.origin
		FROM sources s LEFT JOIN tombstones t ON t.target_kind='source' AND t.target_id=s.id
		WHERE s.repository_id=? AND (s.deleted_at<>'' OR t.id IS NOT NULL)`, repoID)
	if err != nil {
		return operationFailed("query source tombstones: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, oldNative, oldKind, oldOrigin string
		if err := rows.Scan(&id, &oldNative, &oldKind, &oldOrigin); err != nil {
			return operationFailed("scan source tombstone: %v", err)
		}
		if nativeID != "" {
			if oldNative == nativeID {
				return conflict("source %q is tombstoned; supply a new native identity", id)
			}
			continue
		}
		if oldKind == kind && (oldNative != "" || oldOrigin == origin) {
			return conflict("source %q is tombstoned; supply a new native identity", id)
		}
	}
	if err := rows.Err(); err != nil {
		return operationFailed("read source tombstones: %v", err)
	}
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
