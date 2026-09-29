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

func insertCheckpointMutation(tx sqlRunner, id, repoID, revision, sessionID, createdAt, message, basis, agent, nativeSessionID, activityKind, activitySummary string) error {
	contentHash := digestBytes([]byte(fmt.Sprintf("%s|%s|%s|%s|%s", repoID, revision, sessionID, message, basis)))
	if _, err := tx.Exec(`INSERT INTO checkpoints(id, repository_id, revision, session_id, created_at, message, basis, agent, native_session_id, bundle_version, context_manifest_id, content_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, repoID, revision, sessionID, createdAt, message, basis, agent, nativeSessionID, 1, "", contentHash); err != nil {
		return err
	}
	if sessionID != "" {
		result, err := tx.Exec(`UPDATE sessions SET latest_checkpoint_id=?, last_event_at=? WHERE id=? AND state='active'`, id, createdAt, sessionID)
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
	if err := indexDocTx(tx, "checkpoint", id, repoID, message, message+" "+revision); err != nil {
		return err
	}
	return logActivityTx(tx, activityKind, repoID, id, activitySummary)
}

func newCheckpointCmd() *cobra.Command {
	c := &cobra.Command{Use: "checkpoint", Short: "Checkpoints (Across-owned)"}
	c.AddCommand(
		newCheckpointBundleCmd(),
		&cobra.Command{Use: "create --repo ID [--session S] [--message M] [--revision R] [--basis B] [--agent A]", Short: "Create checkpoint", PreRunE: requiredFlags("repo"), RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			sess, _ := cmd.Flags().GetString("session")
			msg, _ := cmd.Flags().GetString("message")
			rev, _ := cmd.Flags().GetString("revision")
			basis, _ := cmd.Flags().GetString("basis")
			agent, _ := cmd.Flags().GetString("agent")
			if rev == "" && cmd.Flags().Changed("revision") {
				return invalidArgument("revision must not be empty")
			}
			if basis != "" {
				if err := requireOneOf("basis", basis, "checkpoint_revision", "verification_revision", "explicit_user_annotation", "capture_time_head_not_causation", "imported_revision", "unknown", "legacy_revision_basis_unknown"); err != nil {
					return err
				}
			}
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
				resolved, resolveErr := git.ResolveRevision(canon, "HEAD")
				if resolveErr != nil {
					return fmt.Errorf("no revision: repository has no commits and none supplied")
				}
				rev = resolved
				if basis == "" || basis == "unknown" {
					basis = "capture_time_head_not_causation"
				}
			} else {
				if _, resolveErr := git.ResolveRevision(canon, rev); resolveErr != nil {
					return invalidArgument("revision %q does not resolve in repository %q", rev, repoID)
				}
				if basis == "" {
					basis = "explicit_user_annotation"
				}
			}
			if rev == "" {
				return fmt.Errorf("no revision: repository has no commits and none supplied")
			}
			id := store.NewID("cp")
			now := store.NowUTC()
			if err := withTx(db, func(tx sqlRunner) error {
				native := ""
				if sess != "" {
					info, err := sessionMustBelong(tx, repoID, sess, true)
					if err != nil {
						return err
					}
					native = info.nativeSessionID
				}
				return insertCheckpointMutation(tx, id, repoID, rev, sess, now, msg, basis, agent, native, "checkpoint.create", "checkpoint "+rev)
			}); err != nil {
				return err
			}
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
			var ra, rb, repoA, repoB string
			if err := db.QueryRow(`SELECT revision, repository_id FROM checkpoints WHERE id=?`, args[0]).Scan(&ra, &repoA); err != nil {
				return fmt.Errorf("checkpoint A not found")
			}
			if err := db.QueryRow(`SELECT revision, repository_id FROM checkpoints WHERE id=?`, args[1]).Scan(&rb, &repoB); err != nil {
				return fmt.Errorf("checkpoint B not found")
			}
			if repoA != repoB {
				return conflict("checkpoints belong to different repositories")
			}
			canon, _, err := repoMustExist(db, repoA)
			if err != nil {
				return err
			}
			resolvedA, err := git.ResolveRevision(canon, ra)
			if err != nil {
				return invalidArgument("revision %q does not resolve in repository %q", ra, repoA)
			}
			resolvedB, err := git.ResolveRevision(canon, rb)
			if err != nil {
				return invalidArgument("revision %q does not resolve in repository %q", rb, repoA)
			}
			out, err := git.Run(canon, "diff", "--stat", resolvedA, resolvedB)
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
			if sess != "" {
				if _, err := sessionMustBelong(db, repoID, sess, false); err != nil {
					return err
				}
			}
			resolvedRevision, err := git.ResolveRevision(canon, rev)
			if err != nil {
				return invalidArgument("revision %q does not resolve in repository %q", rev, repoID)
			}
			wsID := store.NewID("ws")
			path := filepath.Join(home, "workspaces", wsID)
			if _, err := os.Lstat(path); err == nil {
				return conflict("workspace path already exists")
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if _, err := git.Run(canon, "worktree", "add", "--detach", path, resolvedRevision); err != nil {
				_ = os.RemoveAll(path)
				if _, err2 := git.Run("", "clone", canon, path); err2 != nil {
					cleanupRestoreWorkspace(canon, path)
					return fmt.Errorf("restore failed: %v", err)
				}
				if _, err2 := git.Run(path, "checkout", "--detach", resolvedRevision); err2 != nil {
					cleanupRestoreWorkspace(canon, path)
					return err2
				}
			}
			if err := withTx(db, func(tx sqlRunner) error {
				if _, err := tx.Exec(`INSERT INTO workspaces(id, repository_id, revision, branch, path, created_at, state) VALUES(?,?,?,?,?,?,?)`,
					wsID, repoID, rev, "", path, store.NowUTC(), "active"); err != nil {
					return err
				}
				return logActivityTx(tx, "checkpoint.restore", repoID, args[0], "restored to "+path)
			}); err != nil {
				cleanupRestoreWorkspace(canon, path)
				return err
			}
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

func cleanupRestoreWorkspace(canon, path string) {
	_, _ = git.Run(canon, "worktree", "remove", "--force", path)
	_ = os.RemoveAll(path)
	_, _ = git.Run(canon, "worktree", "prune")
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
const autoCheckpointMessage = "post-commit checkpoint"

type autoSession struct {
	id     string
	agent  string
	native string
}

func runPostCommitHook(cmd *cobra.Command, cwd string) error {
	db, _, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	top := git.ShowToplevel(cwd)
	common := git.CommonDir(cwd)
	var repoID, canon string
	rows, err := db.Query(`SELECT id, canonical_path, git_common_dir FROM repositories`)
	if err != nil {
		return err
	}
	best := ""
	for rows.Next() {
		var id, cp, gd string
		if err := rows.Scan(&id, &cp, &gd); err != nil {
			rows.Close()
			return operationFailed("read repository for post-commit hook: %v", err)
		}
		if top != "" && (cp == top || cp == cwd) {
			repoID, canon = id, cp
			break
		}
		if common != "" && (gd == common || cp == common) {
			repoID, canon = id, cp
			break
		}
		if top != "" && len(top) > len(cp) && top[:len(cp)] == cp && cp != "" {
			best = id
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return operationFailed("read repositories for post-commit hook: %v", err)
	}
	if err := rows.Close(); err != nil {
		return operationFailed("close repository scan: %v", err)
	}
	if repoID == "" && best != "" {
		repoID = best
		if err := db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&canon); err != nil {
			return operationFailed("query repository %q: %v", repoID, err)
		}
	}
	if repoID == "" {
		return nil
	}
	head := git.Head(canon)
	if head == "" {
		head = git.Head(cwd)
	}
	if head == "" {
		return nil
	}
	resolvedHead, err := git.ResolveRevision(canon, head)
	if err != nil {
		return invalidArgument("repository HEAD does not resolve for repository %q", repoID)
	}
	srows, err := db.Query(`SELECT id, agent, native_session_id FROM sessions WHERE repository_id=? AND state='active'`, repoID)
	if err != nil {
		return operationFailed("query active sessions: %v", err)
	}
	var act []autoSession
	for srows.Next() {
		var session autoSession
		if err := srows.Scan(&session.id, &session.agent, &session.native); err != nil {
			srows.Close()
			return operationFailed("read active session: %v", err)
		}
		act = append(act, session)
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return operationFailed("read active sessions: %v", err)
	}
	if err := srows.Close(); err != nil {
		return operationFailed("close active session scan: %v", err)
	}
	switch len(act) {
	case 0:
		return nil
	case 1:
		id, err := createAutoCheckpoint(db, repoID, resolvedHead, act[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), id)
		return nil
	default:
		if err := withTx(db, func(tx sqlRunner) error {
			return logActivityTx(tx, "checkpoint.ambiguous", repoID, resolvedHead, "multiple active sessions; no guessed attribution")
		}); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "ambiguous: multiple active sessions, no checkpoint created")
		return nil
	}
}

func createAutoCheckpoint(db *sql.DB, repoID, revision string, session autoSession) (string, error) {
	var id string
	err := withTx(db, func(tx sqlRunner) error {
		if _, err := sessionMustBelong(tx, repoID, session.id, true); err != nil {
			return err
		}
		var existing string
		err := tx.QueryRow(`SELECT id FROM checkpoints WHERE repository_id=? AND revision=? AND session_id=? AND message=? ORDER BY created_at LIMIT 1`, repoID, revision, session.id, autoCheckpointMessage).Scan(&existing)
		if err == nil {
			id = existing
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		id = store.NewID("cp")
		return insertCheckpointMutation(tx, id, repoID, revision, session.id, store.NowUTC(), autoCheckpointMessage, "checkpoint_revision", session.agent, session.native, "checkpoint.auto", autoCheckpointMessage+" "+revision)
	})
	return id, err
}
