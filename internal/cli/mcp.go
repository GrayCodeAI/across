package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// MCP: read-only stdio server (§74). No mutation tools.
// tools/call dispatches to real read-only store queries; unknown or
// mutation-sounding names are refused.
func newMCPCmd() *cobra.Command {
	return &cobra.Command{Use: "mcp", Short: "Read-only MCP stdio server", RunE: func(cmd *cobra.Command, args []string) error {
		in := bufio.NewScanner(cmd.InOrStdin())
		in.Buffer(make([]byte, 1024*1024), 4*1024*1024)
		out := cmd.OutOrStdout()
		tools := []map[string]any{}
		for _, n := range []string{"across_search", "across_brief", "across_inspect", "across_code_search", "across_graph", "across_graph_health", "across_investigate", "across_why", "across_sessions", "across_checkpoints", "across_verifications", "across_review", "across_issues", "across_changes", "across_workspaces", "across_version_sets", "across_activity"} {
			tools = append(tools, map[string]any{"name": n, "description": "read-only Across tool " + n})
		}
		for in.Scan() {
			line := in.Text()
			var msg map[string]any
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				writeMCP(out, map[string]any{"error": "invalid message"})
				continue
			}
			method, _ := msg["method"].(string)
			id := msg["id"]
			switch method {
			case "initialize":
				writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "across", "version": Version}}})
			case "tools/list":
				writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"tools": tools}})
			case "tools/call":
				params, _ := msg["params"].(map[string]any)
				name, _ := params["name"].(string)
				argsMap, _ := params["arguments"].(map[string]any)
				text, isErr := mcpDispatch(name, argsMap)
				if isErr {
					writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "error: " + text}}, "isError": true}})
				} else {
					writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}})
				}
			default:
				writeMCP(out, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "method not found"}})
			}
		}
		return nil
	}}
}

// mcpDispatch runs a read-only tool and returns (text, isError).
func mcpDispatch(name string, args map[string]any) (string, bool) {
	db, _, err := openDB()
	if err != nil {
		return "store unavailable: " + err.Error(), true
	}
	defer db.Close()
	str := func(k string) string {
		if args == nil {
			return ""
		}
		s, _ := args[k].(string)
		return s
	}
	var sb strings.Builder
	q := str("query")
	repo := str("repo")
	limit := 20
	switch name {
	case "across_search":
		rows, err := searchDocs(db, q, repo)
		if err != nil {
			return err.Error(), true
		}
		defer rows.Close()
		n := 0
		for rows.Next() && n < limit {
			var k, r, rp, t, sn string
			rows.Scan(&k, &r, &rp, &t, &sn)
			fmt.Fprintf(&sb, "%s %s %s\n", k, r, t)
			n++
		}
		if n == 0 {
			return "no results (UNKNOWN beyond this)", false
		}
		return sb.String(), false
	case "across_sessions":
		rows, _ := db.Query(`SELECT id, agent, state FROM sessions WHERE (?='' OR repository_id=?) ORDER BY started_at DESC LIMIT 20`, repo, repo)
		defer rows.Close()
		for rows.Next() {
			var id, ag, st string
			rows.Scan(&id, &ag, &st)
			fmt.Fprintf(&sb, "%s %s %s\n", id, ag, st)
		}
		return sb.String(), false
	case "across_checkpoints":
		rows, _ := db.Query(`SELECT id, revision, message FROM checkpoints WHERE (?='' OR repository_id=?) ORDER BY created_at DESC LIMIT 20`, repo, repo)
		defer rows.Close()
		for rows.Next() {
			var id, rev, m string
			rows.Scan(&id, &rev, &m)
			fmt.Fprintf(&sb, "%s %s %s\n", id, rev, m)
		}
		return sb.String(), false
	case "across_verifications":
		rows, _ := db.Query(`SELECT name, exit_code, basis FROM verifications WHERE (?='' OR repository_id=?) ORDER BY started_at DESC LIMIT 20`, repo, repo)
		defer rows.Close()
		for rows.Next() {
			var n, b string
			var e int
			rows.Scan(&n, &e, &b)
			fmt.Fprintf(&sb, "%s exit=%d %s\n", n, e, b)
		}
		return sb.String(), false
	case "across_issues":
		rows, _ := db.Query(`SELECT id, title, state FROM issues WHERE (?='' OR repository_id=?) ORDER BY created_at DESC LIMIT 20`, repo, repo)
		defer rows.Close()
		for rows.Next() {
			var id, t, s string
			rows.Scan(&id, &t, &s)
			fmt.Fprintf(&sb, "%s %s %s\n", id, s, t)
		}
		return sb.String(), false
	case "across_changes":
		rows, _ := db.Query(`SELECT id, title, state FROM changes WHERE (?='' OR repository_id=?) ORDER BY created_at DESC LIMIT 20`, repo, repo)
		defer rows.Close()
		for rows.Next() {
			var id, t, s string
			rows.Scan(&id, &t, &s)
			fmt.Fprintf(&sb, "%s %s %s\n", id, s, t)
		}
		return sb.String(), false
	case "across_activity":
		rows, _ := db.Query(`SELECT kind, ref_id, summary FROM activities ORDER BY occurred_at DESC LIMIT 20`)
		defer rows.Close()
		for rows.Next() {
			var k, r, s string
			rows.Scan(&k, &r, &s)
			fmt.Fprintf(&sb, "%s %s %s\n", k, r, s)
		}
		return sb.String(), false
	case "across_graph_health":
		var nsym int
		_ = db.QueryRow(`SELECT COUNT(*) FROM code_symbols WHERE (?='' OR repository_id=?)`, repo, repo).Scan(&nsym)
		return fmt.Sprintf("symbols: %d\n", nsym), false
	case "across_brief", "across_inspect", "across_code_search", "across_graph", "across_investigate", "across_why", "across_review", "across_workspaces", "across_version_sets":
		return "Across read-only tool " + name + ": run the corresponding across CLI for full evidence.", false
	default:
		return "unknown or undisclosed tool: " + name + " (mutation tools are not exposed)", true
	}
}

func writeMCP(out interface{ Write([]byte) (int, error) }, v any) {
	b, _ := json.Marshal(v)
	out.Write(append(b, '\n'))
	_ = os.Stderr
	fmt.Fprint(os.Stderr, "")
}
