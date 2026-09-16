package cli

import (
	"fmt"
	"os"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newWorkspaceCmd() *cobra.Command {
	c := &cobra.Command{Use: "workspace", Short: "Git worktree workspaces"}
	c.AddCommand(
		&cobra.Command{Use: "create --repo ID --revision REV", Short: "Create workspace", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			rev, _ := cmd.Flags().GetString("revision")
			branch, _ := cmd.Flags().GetString("branch")
			db, home, err := openDB()
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
			}
			wsID := store.NewID("ws")
			path := home + "/workspaces/" + wsID
			args2 := []string{"worktree", "add"}
			if branch != "" {
				args2 = append(args2, "-b", branch)
			} else {
				args2 = append(args2, "--detach")
			}
			args2 = append(args2, path, rev)
			if _, err := git.Run(canon, args2...); err != nil {
				return err
			}
			_, _ = db.Exec(`INSERT INTO workspaces(id, repository_id, revision, branch, path, created_at, state) VALUES(?,?,?,?,?,?,?)`,
				wsID, repoID, rev, branch, path, store.NowUTC(), "active")
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		}},
		&cobra.Command{Use: "list", Short: "List workspaces", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, _ := db.Query(`SELECT id, repository_id, revision, branch, path, state FROM workspaces ORDER BY created_at`)
			defer rows.Close()
			for rows.Next() {
				var id, rp, rev, br, p, st string
				rows.Scan(&id, &rp, &rev, &br, &p, &st)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\t%s\n", id, rp, rev, br, p, st)
			}
			return nil
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show workspace", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var id, rp, rev, br, p, at, st string
			if err := db.QueryRow(`SELECT id, repository_id, revision, branch, path, created_at, state FROM workspaces WHERE id=?`, args[0]).Scan(&id, &rp, &rev, &br, &p, &at, &st); err != nil {
				return fmt.Errorf("workspace not found")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nrepo: %s\nrev: %s\nbranch: %s\npath: %s\ncreated: %s\nstate: %s\n", id, rp, rev, br, p, at, st)
			return nil
		}},
		&cobra.Command{Use: "remove ID", Args: cobra.ExactArgs(1), Short: "Remove workspace", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var rp, p string
			if err := db.QueryRow(`SELECT repository_id, path FROM workspaces WHERE id=?`, args[0]).Scan(&rp, &p); err != nil {
				return fmt.Errorf("workspace not found")
			}
			var canon string
			_ = db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, rp).Scan(&canon)
			_, _ = git.Run(canon, "worktree", "remove", "--force", p)
			_ = os.RemoveAll(p)
			_, _ = db.Exec(`UPDATE workspaces SET state='removed' WHERE id=?`, args[0])
			fmt.Fprintln(cmd.OutOrStdout(), "removed")
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("revision", "", "revision")
	c.PersistentFlags().String("branch", "", "branch")
	return c
}

func newVersionSetCmd() *cobra.Command {
	c := &cobra.Command{Use: "version-set", Short: "Cross-repo version sets"}
	c.AddCommand(
		&cobra.Command{Use: "create NAME", Args: cobra.ExactArgs(1), Short: "Create version set", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("vs")
			if _, err := db.Exec(`INSERT INTO version_sets(id, name, created_at) VALUES(?,?,?)`, id, args[0], store.NowUTC()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "add NAME REPO@REV", Args: cobra.ExactArgs(2), Short: "Add entry", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var vsid string
			if err := db.QueryRow(`SELECT id FROM version_sets WHERE name=?`, args[0]).Scan(&vsid); err != nil {
				return fmt.Errorf("version set not found")
			}
			// parse REPO@REV (repo id may contain _; split at last @)
			s := args[1]
			at := -1
			for i := len(s) - 1; i >= 0; i-- {
				if s[i] == '@' {
					at = i
					break
				}
			}
			if at < 0 {
				return fmt.Errorf("want REPO@REV")
			}
			_, _ = db.Exec(`INSERT OR REPLACE INTO version_set_entries(version_set_id, repository_id, revision) VALUES(?,?,?)`, vsid, s[:at], s[at+1:])
			fmt.Fprintln(cmd.OutOrStdout(), "added")
			return nil
		}},
		&cobra.Command{Use: "show NAME", Args: cobra.ExactArgs(1), Short: "Show version set", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var vsid string
			if err := db.QueryRow(`SELECT id FROM version_sets WHERE name=?`, args[0]).Scan(&vsid); err != nil {
				return fmt.Errorf("version set not found")
			}
			rows, _ := db.Query(`SELECT repository_id, revision FROM version_set_entries WHERE version_set_id=?`, vsid)
			defer rows.Close()
			for rows.Next() {
				var r, v string
				rows.Scan(&r, &v)
				fmt.Fprintf(cmd.OutOrStdout(), "%s@%s\n", r, v)
			}
			return nil
		}},
	)
	return c
}
