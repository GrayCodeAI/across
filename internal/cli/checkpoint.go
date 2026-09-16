package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func queryCheckpoints(db *sql.DB, repoID string) (*sql.Rows, error) {
	if repoID != "" {
		return db.Query(`SELECT id, revision, session_id, created_at, message, basis FROM checkpoints WHERE repository_id=? ORDER BY created_at`, repoID)
	}
	return db.Query(`SELECT id, revision, session_id, created_at, message, basis FROM checkpoints ORDER BY created_at`)
}

func newCheckpointCmd() *cobra.Command {
	c := &cobra.Command{Use: "checkpoint", Short: "Checkpoints (Across-owned)"}
	c.AddCommand(
		&cobra.Command{Use: "create --repo ID [--session S] [--message M] [--revision R] [--basis B] [--agent A]", Short: "Create checkpoint", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			sess, _ := cmd.Flags().GetString("session")
			msg, _ := cmd.Flags().GetString("message")
			rev, _ := cmd.Flags().GetString("revision")
			basis, _ := cmd.Flags().GetString("basis")
			agent, _ := cmd.Flags().GetString("agent")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			canon, _, err := repoMustExist(db, repoID)
			if err != nil {
				return err
			}
			if rev == "" {
				rev = git.Head(canon)
				if basis == "" || basis == "unknown" {
					basis = "capture_time_head_not_causation"
				}
			} else if basis == "" {
				basis = "explicit_user_annotation"
			}
			if rev == "" {
				return fmt.Errorf("no revision: repository has no commits and none supplied")
			}
			id := store.NewID("cp")
			now := store.NowUTC()
			var native string
			if sess != "" {
				_ = db.QueryRow(`SELECT native_session_id FROM sessions WHERE id=?`, sess).Scan(&native)
			}
			if _, err := db.Exec(`INSERT INTO checkpoints(id, repository_id, revision, session_id, created_at, message, basis, agent, native_session_id) VALUES(?,?,?,?,?,?,?,?,?)`,
				id, repoID, rev, sess, now, msg, basis, agent, native); err != nil {
				return err
			}
			if sess != "" {
				_, _ = db.Exec(`UPDATE sessions SET latest_checkpoint_id=?, last_event_at=? WHERE id=?`, id, now, sess)
			}
			indexDoc(db, "checkpoint", id, repoID, msg, msg+" "+rev)
			logActivity(db, "checkpoint.create", repoID, id, "checkpoint "+rev)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "list [--repo ID]", Short: "List checkpoints", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var rows, err2 = queryCheckpoints(db, repoID)
			if err2 != nil {
				return err2
			}
			defer rows.Close()
			for rows.Next() {
				var id, rev, sess, at, msg, basis string
				rows.Scan(&id, &rev, &sess, &at, &msg, &basis)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\t%s\n", id, rev, sess, at, basis, msg)
			}
			return nil
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show checkpoint", RunE: func(cmd *cobra.Command, args []string) error {
			return explainCheckpoint(cmd, args[0])
		}},
		&cobra.Command{Use: "explain ID", Args: cobra.ExactArgs(1), Short: "Explain checkpoint with evidence", RunE: func(cmd *cobra.Command, args []string) error {
			return explainCheckpoint(cmd, args[0])
		}},
		&cobra.Command{Use: "compare A B", Args: cobra.ExactArgs(2), Short: "Compare checkpoints", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var ra, rb, repoA string
			if err := db.QueryRow(`SELECT revision, repository_id FROM checkpoints WHERE id=?`, args[0]).Scan(&ra, &repoA); err != nil {
				return fmt.Errorf("checkpoint A not found")
			}
			if err := db.QueryRow(`SELECT revision FROM checkpoints WHERE id=?`, args[1]).Scan(&rb); err != nil {
				return fmt.Errorf("checkpoint B not found")
			}
			var canon string
			_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoA).Scan(&canon)
			out, err := git.Run(canon, "diff", "--stat", ra, rb)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "base: %s (%s)\nhead: %s (%s)\n\n%s\n", args[0], ra, args[1], rb, out)
			return nil
		}},
		&cobra.Command{Use: "restore ID", Args: cobra.ExactArgs(1), Short: "Restore checkpoint into NEW worktree (never resets current checkout)", RunE: func(cmd *cobra.Command, args []string) error {
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var repoID, rev, sess string
			if err := db.QueryRow(`SELECT repository_id, revision, session_id FROM checkpoints WHERE id=?`, args[0]).Scan(&repoID, &rev, &sess); err != nil {
				return fmt.Errorf("checkpoint not found")
			}
			canon, _, err := repoMustExist(db, repoID)
			if err != nil {
				return err
			}
			wsID := store.NewID("ws")
			path := filepath.Join(home, "workspaces", wsID)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			// New worktree via git worktree add --detach
			if _, err := git.Run(canon, "worktree", "add", "--detach", path, rev); err != nil {
				// fallback: clone + checkout (for bare hosted repos)
				if _, err2 := git.Run("", "clone", canon, path); err2 != nil {
					return fmt.Errorf("restore failed: %v", err)
				}
				if _, err2 := git.Run(path, "checkout", "--detach", rev); err2 != nil {
					return err2
				}
			}
			_, _ = db.Exec(`INSERT INTO workspaces(id, repository_id, revision, branch, path, created_at, state) VALUES(?,?,?,?,?,?,?)`,
				wsID, repoID, rev, "", path, store.NowUTC(), "active")
			logActivity(db, "checkpoint.restore", repoID, args[0], "restored to "+path)
			fmt.Fprintf(cmd.OutOrStdout(), "workspace: %s\nrevision: %s\nbranch: (detached)\nsession: %s\nwarnings: only Git-tracked code state restored; external DBs/cloud/untracked files NOT restored\n", path, rev, sess)
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("session", "", "session id")
	c.PersistentFlags().String("message", "", "message")
	c.PersistentFlags().String("revision", "", "revision")
	c.PersistentFlags().String("basis", "", "revision basis")
	c.PersistentFlags().String("agent", "", "agent")
	return c
}

func explainCheckpoint(cmd *cobra.Command, id string) error {
	db, _, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	var repoID, rev, sess, at, msg, basis, agent, native string
	if err := db.QueryRow(`SELECT repository_id, revision, session_id, created_at, message, basis, agent, native_session_id FROM checkpoints WHERE id=?`, id).Scan(&repoID, &rev, &sess, &at, &msg, &basis, &agent, &native); err != nil {
		return fmt.Errorf("checkpoint not found")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Checkpoint %s\n\nRevision:\n%s (basis: %s)\n\nSession:\n%s\n\nAgent:\n%s (native: %s)\n\nObjective/message:\n%s\n\nCreated:\n%s\n", id, rev, basis, sess, agent, native, msg, at)
	// verification evidence bound to revision
	rows, _ := db.Query(`SELECT id, name, exit_code, basis FROM verifications WHERE repository_id=? AND (revision_before=? OR revision_after=?)`, repoID, rev, rev)
	if rows != nil {
		defer rows.Close()
		fmt.Fprintln(cmd.OutOrStdout(), "\nVerification:")
		found := false
		for rows.Next() {
			var vid, name string
			var code int
			var b string
			rows.Scan(&vid, &name, &code, &b)
			fmt.Fprintf(cmd.OutOrStdout(), "- %s exit=%d basis=%s\n", name, code, b)
			found = true
		}
		if !found {
			fmt.Fprintln(cmd.OutOrStdout(), "- Unknown: no verification evidence bound to this revision.")
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nSources:\n- checkpoint record + git revision (OBSERVED: revision exists only if git cat-file confirms)")
	return nil
}

// runPostCommitHook implements §35: 0 sessions -> none; 1 active -> checkpoint; 2+ -> ambiguity event.
func runPostCommitHook(cmd *cobra.Command, cwd string) error {
	db, _, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	// Robust resolution: use git itself to find the repo root / common dir from
	// cwd, then match against stored canonical_path / git_common_dir. This
	// handles symlinks, relative paths, and worktrees that naive string
	// comparison misses (§35).
	top := git.ShowToplevel(cwd)
	common := git.CommonDir(cwd)
	var repoID, canon string
	rows, err := db.Query(`SELECT id, canonical_path, git_common_dir FROM repositories`)
	if err != nil {
		return err
	}
	defer rows.Close()
	best := ""
	for rows.Next() {
		var id, cp, gd string
		rows.Scan(&id, &cp, &gd)
		// Exact canonical match (covers non-bare repo added by path).
		if top != "" && (cp == top || cp == cwd) {
			repoID, canon = id, cp
			break
		}
		// Git common dir match (covers worktrees and exact git dir).
		if common != "" && (gd == common || cp == common) {
			repoID, canon = id, cp
			break
		}
		// Prefix match as fallback (cwd inside repo working tree).
		if top != "" && len(top) > len(cp) && top[:len(cp)] == cp && cp != "" {
			repoID, canon = id, cp
			best = id
		}
	}
	if repoID == "" && best != "" {
		repoID = best
		_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon)
	}
	if repoID == "" {
		return nil // not an Across repo; silent no-op
	}
	head := git.Head(canon)
	if head == "" {
		// Maybe a worktree head; resolve from cwd directly
		head = git.Head(cwd)
	}
	if head == "" {
		return nil
	}
	srows, _ := db.Query(`SELECT id, agent, native_session_id FROM sessions WHERE repository_id=? AND state='active'`, repoID)
	type s struct{ id, agent, native string }
	var act []s
	if srows != nil {
		defer srows.Close()
		for srows.Next() {
			var x s
			srows.Scan(&x.id, &x.agent, &x.native)
			act = append(act, x)
		}
	}
	switch len(act) {
	case 0:
		return nil
	case 1:
		id := store.NewID("cp")
		_, _ = db.Exec(`INSERT INTO checkpoints(id, repository_id, revision, session_id, created_at, message, basis, agent, native_session_id) VALUES(?,?,?,?,?,?,?,?,?)`,
			id, repoID, head, act[0].id, store.NowUTC(), "post-commit checkpoint", "checkpoint_revision", act[0].agent, act[0].native)
		_, _ = db.Exec(`UPDATE sessions SET latest_checkpoint_id=?, last_event_at=? WHERE id=?`, id, store.NowUTC(), act[0].id)
		logActivity(db, "checkpoint.auto", repoID, id, "post-commit checkpoint "+head)
		fmt.Fprintln(cmd.OutOrStdout(), id)
		return nil
	default:
		logActivity(db, "checkpoint.ambiguous", repoID, head, "multiple active sessions; no guessed attribution")
		fmt.Fprintln(cmd.OutOrStdout(), "ambiguous: multiple active sessions, no checkpoint created")
		return nil
	}
}
