package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/redact"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newGraphCmd() *cobra.Command {
	c := &cobra.Command{Use: "graph", Short: "Code graph"}
	c.AddCommand(
		&cobra.Command{Use: "query --repo ID [--symbol S]", Short: "Query graph", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			sym, _ := cmd.Flags().GetString("symbol")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			q := `SELECT file, symbol, kind, signature FROM code_symbols WHERE repository_id=?`
			a := []any{repoID}
			if sym != "" {
				q += ` AND symbol=?`
				a = append(a, sym)
			}
			q += ` LIMIT 100`
			rows, err := db.Query(q, a...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var f, s, k, sig string
				rows.Scan(&f, &s, &k, &sig)
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s %s\n", f, s, k, sig)
			}
			return nil
		}},
		&cobra.Command{Use: "impact --repo ID --symbol S", Short: "Impact (callers/references)", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			sym, _ := cmd.Flags().GetString("symbol")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, _ := db.Query(`SELECT from_symbol, kind, basis, confidence FROM code_relations WHERE repository_id=? AND to_symbol=? LIMIT 100`, repoID, sym)
			defer rows.Close()
			for rows.Next() {
				var f, k, b string
				var cf float64
				rows.Scan(&f, &k, &b, &cf)
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s basis=%s conf=%.2f (INFERRED unless basis=AST)\n", f, k, b, cf)
			}
			return nil
		}},
		&cobra.Command{Use: "health --repo ID", Short: "Graph health", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var canon string
			_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon)
			head := git.Head(canon)
			var indexedRev string
			_ = db.QueryRow(`SELECT revision FROM code_symbols WHERE repository_id=? LIMIT 1`, repoID).Scan(&indexedRev)
			var nsym, nrel int
			_ = db.QueryRow(`SELECT COUNT(*) FROM code_symbols WHERE repository_id=?`, repoID).Scan(&nsym)
			_ = db.QueryRow(`SELECT COUNT(*) FROM code_relations WHERE repository_id=?`, repoID).Scan(&nrel)
			stale := "current"
			if indexedRev != head {
				stale = "stale"
			}
			if indexedRev == "" {
				stale = "unindexed"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "indexed_revision: %s\nhead: %s\nstatus: %s\nsymbols: %d\nrelations: %d\n", indexedRev, head, stale, nsym, nrel)
			return nil
		}},
		&cobra.Command{Use: "snapshot --repo ID --output F", Short: "Deterministic graph snapshot", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			out, _ := cmd.Flags().GetString("output")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, _ := db.Query(`SELECT file, symbol, kind, signature, revision FROM code_symbols WHERE repository_id=? ORDER BY file, symbol`, repoID)
			defer rows.Close()
			var syms []map[string]any
			for rows.Next() {
				var f, s, k, sig, rev string
				rows.Scan(&f, &s, &k, &sig, &rev)
				syms = append(syms, map[string]any{"file": f, "symbol": s, "kind": k, "signature": sig, "revision": rev})
			}
			b, _ := json.MarshalIndent(map[string]any{"repo": repoID, "symbols": syms}, "", "  ")
			if out != "" {
				return os.WriteFile(out, b, 0o644)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		}},
		&cobra.Command{Use: "diff --repo ID [--base B] [--head H]", Short: "Graph diff: added/removed/changed signatures (renames = removed+added)", RunE: func(cmd *cobra.Command, args []string) error {
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			repoID, _ := cmd.Flags().GetString("repo")
			base, _ := cmd.Flags().GetString("base")
			head, _ := cmd.Flags().GetString("head")
			var canon string
			if err := db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon); err != nil {
				return fmt.Errorf("repository not found")
			}
			if base == "" {
				base = "HEAD~1"
			}
			if head == "" {
				head = "HEAD"
			}
			baseSyms, err := symbolsAtRevision(canon, home, base)
			if err != nil {
				return fmt.Errorf("base %s: %v", base, err)
			}
			headSyms, err := symbolsAtRevision(canon, home, head)
			if err != nil {
				return fmt.Errorf("head %s: %v", head, err)
			}
			added, removed, changed := diffSymbolSets(baseSyms, headSyms)
			for _, s := range added {
				fmt.Fprintf(cmd.OutOrStdout(), "added %s %s\n", s.Key, s.Signature)
			}
			for _, s := range removed {
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s %s\n", s.Key, s.Signature)
			}
			for _, c := range changed {
				fmt.Fprintf(cmd.OutOrStdout(), "changed %s: %s -> %s\n", c.Key, c.Old, c.New)
			}
			if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no symbol changes")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "note: renames reported as removed+added unless confidently detectable (unsupported rename detection)")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("symbol", "", "symbol")
	c.PersistentFlags().String("output", "", "output")
	c.PersistentFlags().String("base", "", "base")
	c.PersistentFlags().String("head", "", "head")
	return c
}

func newWhyCmd() *cobra.Command {
	c := &cobra.Command{Use: "why FILELINE [--repo ID]", Args: cobra.ExactArgs(1), Short: "Provenance: git blame + checkpoint/session/decision", RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var canon string
		if repoID != "" {
			_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon)
		} else {
			cwd, _ := os.Getwd()
			canon = cwd
		}
		blame, err := git.Run(canon, "blame", "-L", "1,1", "--", args[0])
		if err != nil {
			// try without line parsing: FILE:LINE
			parts := args[0]
			file := parts
			line := "1"
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] == ':' {
					file = parts[:i]
					line = parts[i+1:]
					break
				}
			}
			blame, err = git.Run(canon, "blame", "-L", line+","+line, "--", file)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "UNKNOWN: cannot blame ("+err.Error()+")")
				return nil
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), blame)
		// checkpoint linkage: latest checkpoint with same file? best-effort
		var cpid, rev, sess string
		_ = db.QueryRow(`SELECT id, revision, session_id FROM checkpoints WHERE repository_id=? ORDER BY created_at DESC LIMIT 1`, repoID).Scan(&cpid, &rev, &sess)
		if cpid != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "\nlatest checkpoint: %s rev=%s session=%s\nattribution confidence: LOW unless checkpoint revision matches blamed commit (never fabricate agent ownership)\n", cpid, rev, sess)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "\nNo checkpoint evidence. Attribution: UNKNOWN.")
		}
		_ = store.NowUTC
		return nil
	}}
	c.Flags().String("repo", "", "repository id")
	return c
}

func newInvestigateCmd() *cobra.Command {
	return &cobra.Command{Use: "investigate QUERY", Args: cobra.ExactArgs(1), Short: "Evidence-backed investigation", RunE: func(cmd *cobra.Command, args []string) error {
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		fmt.Fprintln(cmd.OutOrStdout(), "Evidence for: "+args[0])
		rows, _ := searchDocs(db, args[0], "")
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var k, r, rp, t, sn string
				rows.Scan(&k, &r, &rp, &t, &sn)
				fmt.Fprintf(cmd.OutOrStdout(), "- %s %s %s\n", k, r, t)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Unknowns: anything not cited above is UNKNOWN.")
		return nil
	}}
}

func newReviewCmd() *cobra.Command {
	c := &cobra.Command{Use: "review --repo ID", Short: "Deterministic review checks (analysis, NOT verification)", RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var canon string
		_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon)
		stat, _ := git.Run(canon, "diff", "--cached", "--stat")
		if stat == "" {
			stat, _ = git.Run(canon, "diff", "--stat")
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Deterministic checks (analysis, NOT verification):")
		fmt.Fprintln(cmd.OutOrStdout(), stat)
		full, _ := git.Run(canon, "diff", "--cached")
		if full == "" {
			full, _ = git.Run(canon, "diff")
		}
		numstat, _ := git.Run(canon, "diff", "--numstat")
		findings := []string{}
		if containsStr(full, "<<<<<<<") || containsStr(full, ">>>>>>>") {
			findings = append(findings, "FAIL: merge markers present")
		}
		if _, n := redact.Redact(full); n > 0 {
			findings = append(findings, fmt.Sprintf("FAIL: %d suspected secret(s) in diff (best-effort detection)", n))
		}
		added, removed := 0, 0
		files := []string{}
		for _, ln := range strings.Split(numstat, "\n") {
			f := strings.Fields(ln)
			if len(f) >= 3 {
				files = append(files, f[2])
				var a, r int
				fmt.Sscanf(f[0], "%d", &a)
				fmt.Sscanf(f[1], "%d", &r)
				added += a
				removed += r
			}
		}
		if added+removed > 1000 {
			findings = append(findings, fmt.Sprintf("WARN: large change (+%d/-%d lines)", added, removed))
		}
		goChanged, testChanged := false, false
		for _, f := range files {
			l := strings.ToLower(f)
			if strings.Contains(l, "migration") || strings.HasSuffix(l, ".sql") {
				findings = append(findings, "WARN: migration change: "+f)
				break
			}
		}
		for _, f := range files {
			l := strings.ToLower(f)
			if strings.HasSuffix(l, ".yaml") || strings.HasSuffix(l, ".yml") || strings.HasSuffix(l, ".toml") || strings.HasSuffix(l, ".json") || strings.Contains(l, "config") || strings.Contains(l, ".env") {
				findings = append(findings, "WARN: config change: "+f)
			}
			if strings.HasPrefix(f, ".git/") || strings.Contains(f, "hooks/") {
				findings = append(findings, "WARN: protected path touched: "+f)
			}
			if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
				goChanged = true
			}
			if strings.HasSuffix(f, "_test.go") {
				testChanged = true
			}
		}
		if containsStr(full, "TODO") || containsStr(full, "FIXME") {
			findings = append(findings, "WARN: TODO/FIXME additions")
		}
		if goChanged && !testChanged {
			findings = append(findings, "WARN: Go code changed without test change (heuristic)")
		}
		for _, c := range findings {
			fmt.Fprintln(cmd.OutOrStdout(), c)
		}
		if len(findings) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no blocking findings")
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Note: agent-provided review is analysis, not verification.")
		return nil
	}}
	c.Flags().String("repo", "", "repository id")
	return c
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
