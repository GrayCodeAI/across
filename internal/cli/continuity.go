package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newMemoryCmd() *cobra.Command {
	c := &cobra.Command{Use: "memory", Short: "Engineering memory"}
	c.AddCommand(
		&cobra.Command{Use: "create --repo ID --kind KIND --title T --body B [--source S]", Short: "Create memory (candidate by default)", PreRunE: requiredFlags("repo", "title", "body"), RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			kind, _ := cmd.Flags().GetString("kind")
			title, _ := cmd.Flags().GetString("title")
			body, _ := cmd.Flags().GetString("body")
			src, _ := cmd.Flags().GetString("source")
			if err := requireOneOf("kind", kind, "decision", "fact", "procedure", "task", "preference", "outcome", "note"); err != nil {
				return err
			}
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			if _, _, err := repoMustExist(db, repoID); err != nil {
				return err
			}
			id := store.NewID("mem")
			now := store.NowUTC()
			if err := withTx(db, func(tx sqlRunner) error {
				if src != "" {
					if err := sourceMustBelong(tx, repoID, src); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(`INSERT INTO memories(id, repository_id, kind, state, title, body, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
					id, repoID, kind, "candidate", title, body, now, now); err != nil {
					return err
				}
				if src != "" {
					if _, err := tx.Exec(`INSERT INTO memory_sources(memory_id, source_id) VALUES(?,?)`, id, src); err != nil {
						return err
					}
				}
				if err := indexDocTx(tx, "memory", id, repoID, title, body); err != nil {
					return err
				}
				return logActivityTx(tx, "memory.create", repoID, id, kind+": "+title)
			}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "list [--repo ID] [--state S]", Short: "List memories", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			state, _ := cmd.Flags().GetString("state")
			if state != "" {
				if err := requireOneOf("state", state, "candidate", "approved", "superseded"); err != nil {
					return err
				}
			}
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			q := `SELECT id, kind, state, title FROM memories WHERE 1=1`
			var a []any
			if repoID != "" {
				q += ` AND repository_id=?`
				a = append(a, repoID)
			}
			if state != "" {
				q += ` AND state=?`
				a = append(a, state)
			}
			q += ` ORDER BY created_at`
			rows, err := db.Query(q, a...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, k, s, t string
				rows.Scan(&id, &k, &s, &t)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", id, k, s, t)
			}
			return nil
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show memory", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var id, rp, k, s, t, b, ca string
			if err := db.QueryRow(`SELECT id, repository_id, kind, state, title, body, created_at FROM memories WHERE id=?`, args[0]).Scan(&id, &rp, &k, &s, &t, &b, &ca); err != nil {
				return notFound("memory %q not found", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nrepo: %s\nkind: %s\nstate: %s\ntitle: %s\ncreated: %s\n\n%s\n\nNote: memory state %s means approved engineering context, not objective truth.\n", id, rp, k, s, t, ca, b, s)
			return nil
		}},
		&cobra.Command{Use: "approve ID", Args: cobra.ExactArgs(1), Short: "Approve candidate memory", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := withTx(db, func(tx sqlRunner) error {
				memory, err := memoryMustExist(tx, args[0])
				if err != nil {
					return err
				}
				if memory.state != "candidate" {
					return conflict("memory %q is not an existing candidate", args[0])
				}
				now := store.NowUTC()
				result, err := tx.Exec(`UPDATE memories SET state='approved', updated_at=? WHERE id=? AND state='candidate'`, now, args[0])
				if err != nil {
					return err
				}
				affected, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if affected != 1 {
					return conflict("memory %q is not an existing candidate", args[0])
				}
				if err := indexDocTx(tx, "memory", args[0], memory.repositoryID, memory.title, memory.body); err != nil {
					return err
				}
				return logActivityTx(tx, "memory.approve", memory.repositoryID, args[0], "memory approved")
			}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "approved")
			return nil
		}},
		&cobra.Command{Use: "supersede OLD NEW", Args: cobra.ExactArgs(2), Short: "Supersede memory", RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == args[1] {
				return invalidArgument("a memory cannot supersede itself")
			}
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := withTx(db, func(tx sqlRunner) error {
				oldMemory, err := memoryMustExist(tx, args[0])
				if err != nil {
					return err
				}
				newMemory, err := memoryMustExist(tx, args[1])
				if err != nil {
					return err
				}
				if oldMemory.repositoryID != newMemory.repositoryID {
					return conflict("memories %q and %q belong to different repositories", args[0], args[1])
				}
				if oldMemory.state == "superseded" {
					return conflict("memory %q is already superseded", args[0])
				}
				now := store.NowUTC()
				result, err := tx.Exec(`UPDATE memories SET state='superseded', superseded_by=?, updated_at=? WHERE id=? AND state!='superseded'`, args[1], now, args[0])
				if err != nil {
					return err
				}
				affected, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if affected != 1 {
					return conflict("memory %q is missing or already superseded", args[0])
				}
				if _, err := tx.Exec(`DELETE FROM search_index WHERE kind='memory' AND ref_id=?`, args[0]); err != nil {
					return err
				}
				return logActivityTx(tx, "memory.supersede", oldMemory.repositoryID, args[0], "memory superseded by "+args[1])
			}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "superseded")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("kind", "note", "decision|fact|procedure|task|preference|outcome|note")
	c.PersistentFlags().String("title", "", "title")
	c.PersistentFlags().String("body", "", "body")
	c.PersistentFlags().String("source", "", "source id")
	c.PersistentFlags().String("state", "", "filter")
	return c
}

func newSearchCmd() *cobra.Command {
	c := &cobra.Command{Use: "search QUERY", Args: cobra.ExactArgs(1), Short: "Search sessions/memory/verifications (FTS deferred to when FTS5 ships in default builds)", RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" {
			return invalidArgument("query must not be empty")
		}
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		repo, _ := cmd.Flags().GetString("repo")
		rows, err := searchDocs(db, args[0], repo)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k, r, rp, t, sn string
			rows.Scan(&k, &r, &rp, &t, &sn)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s repo=%s title=%s\n  %s\n", k, r, rp, t, sn)
		}
		return nil
	}}
	c.Flags().String("repo", "", "filter repo")
	return c
}

func newBriefCmd() *cobra.Command {
	c := &cobra.Command{Use: "brief QUERY", Args: cobra.ExactArgs(1), Short: "Continuity brief", RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" {
			return invalidArgument("query must not be empty")
		}
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		repo, _ := cmd.Flags().GetString("repo")
		fmt.Fprintln(cmd.OutOrStdout(), "Objective")
		fmt.Fprintln(cmd.OutOrStdout(), args[0])
		if repo != "" {
			var canon string
			_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repo).Scan(&canon)
			fmt.Fprintln(cmd.OutOrStdout(), "\nCurrent repository revision")
			fmt.Fprintln(cmd.OutOrStdout(), git.Head(canon)+" (OBSERVED)")
		}
		// relevant decisions
		fmt.Fprintln(cmd.OutOrStdout(), "\nRelevant decisions")
		rows, _ := db.Query(`SELECT title, state FROM memories WHERE (repository_id=? OR ?='') AND kind='decision' ORDER BY created_at DESC LIMIT 10`, repo, repo)
		if rows != nil {
			defer rows.Close()
			n := 0
			for rows.Next() {
				var t, s string
				rows.Scan(&t, &s)
				fmt.Fprintf(cmd.OutOrStdout(), "- %s [%s]\n", t, s)
				n++
			}
			if n == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "- Unknown: no decisions recorded.")
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nRelevant checkpoints")
		rows2, _ := db.Query(`SELECT id, revision, message FROM checkpoints WHERE (repository_id=? OR ?='') ORDER BY created_at DESC LIMIT 10`, repo, repo)
		if rows2 != nil {
			defer rows2.Close()
			n := 0
			for rows2.Next() {
				var id, rev, m string
				rows2.Scan(&id, &rev, &m)
				fmt.Fprintf(cmd.OutOrStdout(), "- %s rev=%s %s\n", id, rev, m)
				n++
			}
			if n == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "- Unknown: no checkpoints.")
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nVerification evidence")
		rows3, _ := db.Query(`SELECT name, exit_code, basis FROM verifications WHERE (repository_id=? OR ?='') ORDER BY started_at DESC LIMIT 10`, repo, repo)
		if rows3 != nil {
			defer rows3.Close()
			n := 0
			for rows3.Next() {
				var nm, b string
				var code int
				rows3.Scan(&nm, &code, &b)
				fmt.Fprintf(cmd.OutOrStdout(), "- %s exit=%d basis=%s\n", nm, code, b)
				n++
			}
			if n == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "- Unknown: no verification evidence.")
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nUnknowns\n- Anything not listed above is UNKNOWN. Do not invent confidence.")
		return nil
	}}
	c.Flags().String("repo", "", "repository id")
	return c
}

func newHandoffCmd() *cobra.Command {
	c := &cobra.Command{Use: "handoff --session ID [--output F]", Short: "Write handoff", PreRunE: requiredFlags("session"), RunE: func(cmd *cobra.Command, args []string) error {
		sess, _ := cmd.Flags().GetString("session")
		out, _ := cmd.Flags().GetString("output")
		format, _ := cmd.Flags().GetString("format")
		if err := requireOneOf("format", format, "markdown", "json"); err != nil {
			return err
		}
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		var info sessionInfo
		if err := db.QueryRow(`SELECT id, repository_id, agent, native_session_id, state FROM sessions WHERE id=?`, sess).Scan(&info.id, &info.repositoryID, &info.agent, &info.nativeSessionID, &info.state); err != nil {
			if err == sql.ErrNoRows {
				return notFound("session %q not found", sess)
			}
			return err
		}
		var latest, revision, basis, message string
		if info.state != "" {
			if err := db.QueryRow(`SELECT latest_checkpoint_id FROM sessions WHERE id=?`, sess).Scan(&latest); err != nil {
				return err
			}
		}
		if latest != "" {
			if err := db.QueryRow(`SELECT revision, basis, message FROM checkpoints WHERE id=? AND repository_id=?`, latest, info.repositoryID).Scan(&revision, &basis, &message); err != nil {
				return notFound("latest checkpoint %q not found", latest)
			}
		}
		envelope := HandoffEnvelope{
			SchemaVersion: contractSchemaVersion,
			ID:            newContractID("hnd"),
			Type:          "handoff",
			Session:       sess,
			Repository:    info.repositoryID,
			Revision:      revision,
			Latest:        latest,
			Evidence:      []EvidenceReference{},
			Unknowns:      []string{"remaining work beyond recorded checkpoints is UNKNOWN"},
			GeneratedAt:   store.NowUTC(),
		}
		if latest != "" {
			envelope.Evidence = append(envelope.Evidence, EvidenceReference{Kind: "checkpoint", ID: latest, Revision: revision, Basis: basis, Reason: message})
		}
		decisionRows, err := db.Query(`SELECT id, title, state FROM memories WHERE repository_id=? AND state='approved' ORDER BY created_at DESC LIMIT 10`, info.repositoryID)
		if err != nil {
			return err
		}
		for decisionRows.Next() {
			var id, title, state string
			if err := decisionRows.Scan(&id, &title, &state); err != nil {
				decisionRows.Close()
				return err
			}
			envelope.Evidence = append(envelope.Evidence, EvidenceReference{Kind: "memory", ID: id, Basis: state, Reason: title})
		}
		if err := decisionRows.Err(); err != nil {
			decisionRows.Close()
			return err
		}
		if err := decisionRows.Close(); err != nil {
			return err
		}
		verificationRows, err := db.Query(`SELECT id, name, revision_after, basis, exit_code FROM verifications WHERE repository_id=? ORDER BY started_at DESC LIMIT 10`, info.repositoryID)
		if err != nil {
			return err
		}
		for verificationRows.Next() {
			var id, name, rev, basis string
			var exitCode int
			if err := verificationRows.Scan(&id, &name, &rev, &basis, &exitCode); err != nil {
				verificationRows.Close()
				return err
			}
			envelope.Evidence = append(envelope.Evidence, EvidenceReference{Kind: "verification", ID: id, Revision: rev, Basis: basis, Reason: fmt.Sprintf("%s exit=%d", name, exitCode)})
		}
		if err := verificationRows.Err(); err != nil {
			verificationRows.Close()
			return err
		}
		if err := verificationRows.Close(); err != nil {
			return err
		}
		if err := sealHandoff(&envelope); err != nil {
			return err
		}
		var content string
		if format == "json" {
			encoded, err := json.MarshalIndent(envelope, "", "  ")
			if err != nil {
				return err
			}
			content = string(encoded)
		} else {
			content = handoffMarkdown(envelope)
		}
		if err := withTx(db, func(tx sqlRunner) error {
			_, err := tx.Exec(`INSERT INTO handoffs(id, session_id, repository_id, revision, schema_version, format, content, content_hash, created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
				envelope.ID, sess, info.repositoryID, revision, contractSchemaVersion, format, content, envelope.ContentHash, envelope.GeneratedAt)
			return err
		}); err != nil {
			return err
		}
		if out != "" {
			resolved, err := requireOutputFile(out)
			if err != nil {
				return err
			}
			return os.WriteFile(resolved, []byte(content), 0o644)
		}
		fmt.Fprintln(cmd.OutOrStdout(), content)
		return nil
	}}
	c.Flags().String("session", "", "session id")
	c.Flags().String("output", "", "output file")
	c.Flags().String("format", "markdown", "markdown|json")
	return c
}

func handoffMarkdown(envelope HandoffEnvelope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Handoff\n\nid: %s\nschema: %d\nsession: %s\nrepo: %s\nrevision: %s\nlatest_checkpoint: %s\n\n", envelope.ID, envelope.SchemaVersion, envelope.Session, envelope.Repository, envelope.Revision, envelope.Latest)
	b.WriteString("## Evidence\n")
	for _, evidence := range envelope.Evidence {
		fmt.Fprintf(&b, "- %s %s [%s] %s\n", evidence.Kind, evidence.ID, evidence.Basis, evidence.Reason)
	}
	b.WriteString("\n## Unknowns\n")
	for _, unknown := range envelope.Unknowns {
		fmt.Fprintf(&b, "- %s\n", unknown)
	}
	return b.String()
}

func newDossierCmd() *cobra.Command {
	c := &cobra.Command{Use: "dossier [--change ID]", Short: "Change dossier: requirement+issue+decision+session+checkpoint+verification+review+outcome", RunE: func(cmd *cobra.Command, args []string) error {
		changeID, _ := cmd.Flags().GetString("change")
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		out := cmd.OutOrStdout()
		if changeID == "" {
			fmt.Fprintln(out, "Usage: across dossier --change CHANGE_ID")
			fmt.Fprintln(out, "Combines linked issue/decision/session/checkpoint/verification/review/outcome. UNKNOWN where evidence missing.")
			return nil
		}
		var repoID, title, desc, base, head, state string
		if err := db.QueryRow(`SELECT repository_id, title, description, base, head, state FROM changes WHERE id=?`, changeID).Scan(&repoID, &title, &desc, &base, &head, &state); err != nil {
			return fmt.Errorf("change not found")
		}
		fmt.Fprintf(out, "# Dossier %s\n\nTitle: %s\nState: %s\nBase: %s Head: %s\n\n%s\n", changeID, title, state, base, head, desc)
		fmt.Fprintln(out, "\n## Approvals")
		arows, _ := db.Query(`SELECT principal, decision, created_at FROM change_approvals WHERE change_id=? ORDER BY created_at`, changeID)
		n := 0
		if arows != nil {
			defer arows.Close()
			for arows.Next() {
				var p, d, c string
				arows.Scan(&p, &d, &c)
				fmt.Fprintf(out, "- %s %s at %s (STATED, not correctness)\n", p, d, c)
				n++
			}
		}
		if n == 0 {
			fmt.Fprintln(out, "- Unknown: no approvals recorded.")
		}
		fmt.Fprintln(out, "\n## Linked checkpoints")
		crows, _ := db.Query(`SELECT id, revision, message, basis FROM checkpoints WHERE change_id=? OR repository_id=? ORDER BY created_at DESC LIMIT 10`, changeID, repoID)
		n = 0
		if crows != nil {
			defer crows.Close()
			for crows.Next() {
				var id, rev, m, b string
				crows.Scan(&id, &rev, &m, &b)
				fmt.Fprintf(out, "- %s rev=%s basis=%s %s\n", id, rev, b, m)
				n++
			}
		}
		if n == 0 {
			fmt.Fprintln(out, "- Unknown: no checkpoints linked.")
		}
		fmt.Fprintln(out, "\n## Verification evidence")
		vrows, _ := db.Query(`SELECT name, exit_code, basis, revision_after FROM verifications WHERE repository_id=? ORDER BY started_at DESC LIMIT 10`, repoID)
		n = 0
		if vrows != nil {
			defer vrows.Close()
			for vrows.Next() {
				var nm, b, rev string
				var code int
				vrows.Scan(&nm, &code, &b, &rev)
				fmt.Fprintf(out, "- %s exit=%d basis=%s rev=%s\n", nm, code, b, rev)
				n++
			}
		}
		if n == 0 {
			fmt.Fprintln(out, "- Unknown: no verification evidence.")
		}
		fmt.Fprintln(out, "\n## Decisions (memory)")
		mrows, _ := db.Query(`SELECT title, state FROM memories WHERE repository_id=? AND kind='decision' ORDER BY created_at DESC LIMIT 10`, repoID)
		n = 0
		if mrows != nil {
			defer mrows.Close()
			for mrows.Next() {
				var t, s string
				mrows.Scan(&t, &s)
				fmt.Fprintf(out, "- %s [%s]\n", t, s)
				n++
			}
		}
		if n == 0 {
			fmt.Fprintln(out, "- Unknown: no decisions recorded.")
		}
		fmt.Fprintln(out, "\n## Unknowns\n- Anything not cited above is UNKNOWN.")
		return nil
	}}
	c.Flags().String("change", "", "change id")
	return c
}

func newContextCmd() *cobra.Command {
	c := &cobra.Command{Use: "context", Short: "Context utilities"}
	c.AddCommand(newContextPackCmd(), newContextShowCmd(), &cobra.Command{Use: "diff --base R --head H --repo ID", Short: "Staleness analysis over changed files", PreRunE: requiredFlags("base", "head", "repo"), RunE: func(cmd *cobra.Command, args []string) error {
		base, _ := cmd.Flags().GetString("base")
		head, _ := cmd.Flags().GetString("head")
		repo, _ := cmd.Flags().GetString("repo")
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		canon, _, err := repoMustExist(db, repo)
		if err != nil {
			return err
		}
		resolvedBase, err := git.ResolveRevision(canon, base)
		if err != nil {
			return invalidArgument("revision %q does not resolve in repository %q", base, repo)
		}
		resolvedHead, err := git.ResolveRevision(canon, head)
		if err != nil {
			return invalidArgument("revision %q does not resolve in repository %q", head, repo)
		}
		changed := map[string]bool{}
		out, err := git.Run(canon, "diff", "--name-only", resolvedBase, resolvedHead)
		if err != nil {
			return err
		}
		if out != "" {
			for _, f := range strings.Split(out, "\n") {
				if f != "" {
					changed[f] = true
				}
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "base: %s\nhead: %s\nchanged files: %d\n", base, head, len(changed))
		for f := range changed {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f)
		}
		// Memories whose body mentions a changed file are possibly stale.
		rows, _ := db.Query(`SELECT id, title, body FROM memories WHERE repository_id=?`, repo)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var id, t, b string
				rows.Scan(&id, &t, &b)
				stale := false
				for f := range changed {
					if strings.Contains(b, f) || strings.Contains(t, f) {
						stale = true
						break
					}
				}
				if stale {
					fmt.Fprintf(cmd.OutOrStdout(), "- memory %s %q: POSSIBLY STALE (references changed file; verify before relying, INFERRED)\n", id, t)
				}
			}
		}
		// Verifications bound to base or older are stale.
		vrows, _ := db.Query(`SELECT id, name, revision_after FROM verifications WHERE repository_id=?`, repo)
		if vrows != nil {
			defer vrows.Close()
			for vrows.Next() {
				var id, nm, rev string
				vrows.Scan(&id, &nm, &rev)
				if rev != resolvedHead {
					fmt.Fprintf(cmd.OutOrStdout(), "- verification %s %q bound to %s (not %s): STALE, re-run (OBSERVED binding)\n", id, nm, rev, head)
				}
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Rule: no claim of correctness without fresh evidence at head revision.")
		return nil
	}})
	c.PersistentFlags().String("base", "", "base rev")
	c.PersistentFlags().String("head", "", "head rev")
	c.PersistentFlags().String("repo", "", "repo")
	return c
}
