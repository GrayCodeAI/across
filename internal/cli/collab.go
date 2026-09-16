package cli

import (
	"fmt"
	"strings"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newIssueCmd() *cobra.Command {
	c := &cobra.Command{Use: "issue", Short: "Local issues"}
	c.AddCommand(
		&cobra.Command{Use: "create --repo ID --title T [--body B]", Short: "Create issue", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			title, _ := cmd.Flags().GetString("title")
			body, _ := cmd.Flags().GetString("body")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("iss")
			if _, err := db.Exec(`INSERT INTO issues(id, repository_id, title, body, state, created_at) VALUES(?,?,?,?,?,?)`, id, repoID, title, body, "open", store.NowUTC()); err != nil {
				return err
			}
			indexDoc(db, "issue", id, repoID, title, body)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "list [--repo ID]", Short: "List issues", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			q := `SELECT id, title, state FROM issues ORDER BY created_at`
			var rows, err2 = db.Query(q)
			if repoID != "" {
				rows, err2 = db.Query(`SELECT id, title, state FROM issues WHERE repository_id=? ORDER BY created_at`, repoID)
			}
			if err2 != nil {
				return err2
			}
			defer rows.Close()
			for rows.Next() {
				var id, t, s string
				rows.Scan(&id, &t, &s)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", id, s, t)
			}
			return nil
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show issue", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var id, rp, t, b, s string
			if err := db.QueryRow(`SELECT id, repository_id, title, body, state FROM issues WHERE id=?`, args[0]).Scan(&id, &rp, &t, &b, &s); err != nil {
				return fmt.Errorf("issue not found")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nrepo: %s\ntitle: %s\nstate: %s\n\n%s\n", id, rp, t, s, b)
			rows, _ := db.Query(`SELECT body, created_at FROM issue_comments WHERE issue_id=? ORDER BY created_at`, id)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var cb, ca string
					rows.Scan(&cb, &ca)
					fmt.Fprintf(cmd.OutOrStdout(), "\ncomment [%s]: %s\n", ca, cb)
				}
			}
			return nil
		}},
		&cobra.Command{Use: "comment ID --body B", Args: cobra.ExactArgs(1), Short: "Comment", RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := cmd.Flags().GetString("body")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			_, _ = db.Exec(`INSERT INTO issue_comments(id, issue_id, body, created_at) VALUES(?,?,?,?)`, store.NewID("cmt"), args[0], body, store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), "commented")
			return nil
		}},
		&cobra.Command{Use: "close ID", Args: cobra.ExactArgs(1), Short: "Close issue", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			_, _ = db.Exec(`UPDATE issues SET state='closed', closed_at=? WHERE id=?`, store.NowUTC(), args[0])
			fmt.Fprintln(cmd.OutOrStdout(), "closed")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repo")
	c.PersistentFlags().String("title", "", "title")
	c.PersistentFlags().String("body", "", "body")
	return c
}

func newChangeCmd() *cobra.Command {
	c := &cobra.Command{Use: "change", Short: "Local PR-like changes"}
	c.AddCommand(
		&cobra.Command{Use: "create --repo ID --title T [--base B] [--head H]", Short: "Create change", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			title, _ := cmd.Flags().GetString("title")
			base, _ := cmd.Flags().GetString("base")
			head, _ := cmd.Flags().GetString("head")
			desc, _ := cmd.Flags().GetString("desc")
			if base == "" {
				base = "main"
			}
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("chg")
			if _, err := db.Exec(`INSERT INTO changes(id, repository_id, title, description, base, head, state, created_at) VALUES(?,?,?,?,?,?,?,?)`, id, repoID, title, desc, base, head, "open", store.NowUTC()); err != nil {
				return err
			}
			indexDoc(db, "change", id, repoID, title, desc)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "list [--repo ID]", Short: "List changes", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var rows, err2 = db.Query(`SELECT id, title, state FROM changes ORDER BY created_at`)
			if repoID != "" {
				rows, err2 = db.Query(`SELECT id, title, state FROM changes WHERE repository_id=? ORDER BY created_at`, repoID)
			}
			if err2 != nil {
				return err2
			}
			defer rows.Close()
			for rows.Next() {
				var id, t, s string
				rows.Scan(&id, &t, &s)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", id, s, t)
			}
			return nil
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show change", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var id, rp, t, d, b, h, s string
			if err := db.QueryRow(`SELECT id, repository_id, title, description, base, head, state FROM changes WHERE id=?`, args[0]).Scan(&id, &rp, &t, &d, &b, &h, &s); err != nil {
				return fmt.Errorf("change not found")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nrepo: %s\ntitle: %s\nbase: %s\nhead: %s\nstate: %s\n\n%s\n", id, rp, t, b, h, s, d)
			return nil
		}},
		&cobra.Command{Use: "approve ID [--by NAME]", Args: cobra.ExactArgs(1), Short: "Approve", RunE: func(cmd *cobra.Command, args []string) error {
			by, _ := cmd.Flags().GetString("by")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			_, _ = db.Exec(`INSERT INTO change_approvals(id, change_id, principal, decision, created_at) VALUES(?,?,?,?,?)`, store.NewID("appr"), args[0], by, "approve", store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), "approved")
			return nil
		}},
		&cobra.Command{Use: "request-changes ID [--by NAME]", Args: cobra.ExactArgs(1), Short: "Request changes", RunE: func(cmd *cobra.Command, args []string) error {
			by, _ := cmd.Flags().GetString("by")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			_, _ = db.Exec(`INSERT INTO change_approvals(id, change_id, principal, decision, created_at) VALUES(?,?,?,?,?)`, store.NewID("appr"), args[0], by, "request-changes", store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), "changes requested")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repo")
	c.PersistentFlags().String("title", "", "title")
	c.PersistentFlags().String("base", "main", "base")
	c.PersistentFlags().String("head", "", "head")
	c.PersistentFlags().String("desc", "", "desc")
	c.PersistentFlags().String("by", "local", "principal")
	return c
}

func newBranchRuleCmd() *cobra.Command {
	c := &cobra.Command{Use: "branch-rule", Short: "Branch protection rules"}
	c.AddCommand(&cobra.Command{Use: "add --repo ID --pattern P [--min-approvals N] [--required-verifications a,b]", Short: "Add rule", RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		pat, _ := cmd.Flags().GetString("pattern")
		min, _ := cmd.Flags().GetInt("min-approvals")
		reqVer, _ := cmd.Flags().GetString("required-verifications")
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		id := store.NewID("br")
		if _, err := db.Exec(`INSERT INTO branch_rules(id, repository_id, pattern, min_approvals, required_verifications, created_at) VALUES(?,?,?,?,?,?)`, id, repoID, pat, min, reqVer, store.NowUTC()); err != nil {
			return err
		}
		// install pre-receive hook for hosted repos
		var canon, auth string
		_ = db.QueryRow(`SELECT canonical_path, authority_mode FROM repositories WHERE id=?`, repoID).Scan(&canon, &auth)
		if auth == "hosted" {
			if err := installPreReceive(canon); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "rule %s (hook install warning: %v)\n", id, err)
				return nil
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), id)
		return nil
	}})
	c.PersistentFlags().String("repo", "", "repo")
	c.PersistentFlags().String("pattern", "main", "pattern")
	c.PersistentFlags().Int("min-approvals", 0, "min approvals")
	c.PersistentFlags().String("required-verifications", "", "comma-separated required verification names")
	return c
}

func installPreReceive(bare string) error {
	// never overwrite silently: chain if exists
	content := "#!/bin/sh\n# Across pre-receive: block direct pushes to protected branches without change approval\n# (minimal enforcement: reject pushes to refs/heads/main)\nwhile read old new ref; do\n  case \"$ref\" in\n    refs/heads/main) echo \"Across: direct push to main blocked; use change queue\" >&2; exit 1;;\n  esac\ndone\nexit 0\n"
	return git.InstallHook(bare+"/hooks", "pre-receive", content)
}

func newQueueCmd() *cobra.Command {
	c := &cobra.Command{Use: "queue", Short: "Merge queue"}
	c.AddCommand(
		&cobra.Command{Use: "add CHANGE_ID", Args: cobra.ExactArgs(1), Short: "Queue change", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("mq")
			_, _ = db.Exec(`INSERT INTO merge_queue(id, change_id, state, created_at) VALUES(?,?,?,?)`, id, args[0], "queued", store.NowUTC())
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "merge CHANGE_ID", Args: cobra.ExactArgs(1), Short: "Merge via isolated check (fail if base moved)", RunE: func(cmd *cobra.Command, args []string) error {
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var repoID, base, head string
			if err := db.QueryRow(`SELECT repository_id, base, head FROM changes WHERE id=?`, args[0]).Scan(&repoID, &base, &head); err != nil {
				return fmt.Errorf("change not found")
			}
			var canon, authority string
			_ = db.QueryRow(`SELECT canonical_path, authority_mode FROM repositories WHERE id=?`, repoID).Scan(&canon, &authority)
			baseSHA, err := git.Run(canon, "rev-parse", base)
			if err != nil {
				return err
			}
			// Gate 1: branch-rule approvals + required verifications (§66).
			var minAppr int
			var reqVer string
			_ = db.QueryRow(`SELECT min_approvals, required_verifications FROM branch_rules WHERE repository_id=? AND (pattern=? OR ?=pattern) LIMIT 1`, repoID, base, base).Scan(&minAppr, &reqVer)
			var approvals int
			_ = db.QueryRow(`SELECT COUNT(*) FROM change_approvals WHERE change_id=? AND decision='approve'`, args[0]).Scan(&approvals)
			if approvals < minAppr {
				return fmt.Errorf("blocked: %d approvals, need %d (APPROVED != correct, but gate enforced)", approvals, minAppr)
			}
			if reqVer != "" {
				for _, vname := range strings.Split(reqVer, ",") {
					vname = strings.TrimSpace(vname)
					if vname == "" {
						continue
					}
					var exitCode int
					var basis string
					err := db.QueryRow(`SELECT exit_code, basis FROM verifications WHERE repository_id=? AND name=? ORDER BY started_at DESC LIMIT 1`, repoID, vname).Scan(&exitCode, &basis)
					if err != nil {
						return fmt.Errorf("blocked: required verification %q has no evidence", vname)
					}
					if exitCode != 0 || basis != "executed_by_across_local_runner" {
						return fmt.Errorf("blocked: required verification %q not passing with Across-executed evidence (exit=%d basis=%s)", vname, exitCode, basis)
					}
				}
			}
			// isolated workspace: clone to tmp and merge
			tmp := home + "/tmp/merge-" + store.NewID("m")
			if _, err := git.Run("", "clone", canon, tmp); err != nil {
				return err
			}
			// ensure commit identity in the isolated clone
			if _, cerr := git.Run(tmp, "config", "user.email", "across@local"); cerr != nil {
				// ignore — global config may exist
			}
			if _, cerr := git.Run(tmp, "config", "user.name", "across"); cerr != nil {
				// ignore
			}
			if _, err := git.Run(tmp, "checkout", base); err != nil {
				return err
			}
			if _, err := git.Run(tmp, "merge", "--no-ff", "--no-commit", head); err != nil {
				return fmt.Errorf("merge conflict or bad head: %v", err)
			}
			// verify base hasn't moved
			nowSHA, _ := git.Run(canon, "rev-parse", base)
			if nowSHA != baseSHA {
				return fmt.Errorf("base moved during merge; failing (no silent rebase)")
			}
			// Check if the merge introduced anything (head may already be in base).
			diffOut, _ := git.Run(tmp, "diff", "--cached", "--stat")
			if diffOut == "" {
				// Fast-forward / already merged — nothing to commit. Report cleanly.
				fmt.Fprintln(cmd.OutOrStdout(), "no-op merge: head already in base (fast-forward); base="+baseSHA)
				_, _ = db.Exec(`UPDATE changes SET state='merged', merged_at=? WHERE id=?`, store.NowUTC(), args[0])
				return nil
			}
			if _, err := git.Run(tmp, "commit", "-m", "merge change "+args[0]); err != nil {
				return fmt.Errorf("merge commit failed: %v", err)
			}
			// push back if hosted; for external just report
			fmt.Fprintln(cmd.OutOrStdout(), "merge prepared at "+tmp+" base="+baseSHA+" (push manually or host-merge)")
			_, _ = db.Exec(`UPDATE changes SET state='merged', merged_at=? WHERE id=?`, store.NowUTC(), args[0])
			return nil
		}},
	)
	return c
}
