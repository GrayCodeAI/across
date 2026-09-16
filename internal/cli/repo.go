package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	c := &cobra.Command{Use: "repo", Short: "Repositories"}
	c.AddCommand(
		&cobra.Command{Use: "add PATH", Args: cobra.ExactArgs(1), Short: "Register external repository", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			abs, _ := filepath.Abs(args[0])
			if !git.IsRepo(abs) {
				return fmt.Errorf("not a git repository: %s", abs)
			}
			common := git.CommonDir(abs)
			id := store.NewID("repo")
			name := filepath.Base(abs)
			def := git.CurrentBranch(abs)
			if def == "" {
				def = "main"
			}
			if _, err := db.Exec(`INSERT INTO repositories(id, display_name, canonical_path, git_common_dir, authority_mode, default_branch, created_at) VALUES(?,?,?,?,?,?,?)`,
				id, name, abs, common, "external", def, store.NowUTC()); err != nil {
				return err
			}
			logActivity(db, "repo.add", id, id, "repo add "+abs)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "list", Short: "List repositories", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, err := db.Query(`SELECT id, display_name, canonical_path, authority_mode, default_branch FROM repositories ORDER BY created_at`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, name, path, auth, def string
				rows.Scan(&id, &name, &path, &auth, &def)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", id, name, path, auth, def)
			}
			return nil
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), Short: "Show repository", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var id, name, path, common, auth, def, created string
			if err := db.QueryRow(`SELECT id, display_name, canonical_path, git_common_dir, authority_mode, default_branch, created_at FROM repositories WHERE id=?`, args[0]).Scan(&id, &name, &path, &common, &auth, &def, &created); err != nil {
				return fmt.Errorf("repository not found")
			}
			head := git.Head(path)
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nname: %s\npath: %s\ncommon_dir: %s\nauthority: %s\ndefault_branch: %s\nhead: %s\ncreated: %s\n", id, name, path, common, auth, def, head, created)
			return nil
		}},
		&cobra.Command{Use: "create NAME", Args: cobra.ExactArgs(1), Short: "Create Across-hosted local bare repository", RunE: func(cmd *cobra.Command, args []string) error {
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			name := args[0]
			bare := filepath.Join(home, "repositories", name+".git")
			if _, err := os.Stat(bare); err == nil {
				return fmt.Errorf("already exists: %s", bare)
			}
			if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
				return err
			}
			if _, err := git.Run("", "init", "--bare", "--initial-branch=main", bare); err != nil {
				return err
			}
			id := store.NewID("repo")
			if _, err := db.Exec(`INSERT INTO repositories(id, display_name, canonical_path, git_common_dir, authority_mode, default_branch, created_at) VALUES(?,?,?,?,?,?,?)`,
				id, name, bare, bare, "hosted", "main", store.NowUTC()); err != nil {
				return err
			}
			logActivity(db, "repo.create", id, id, "repo create "+name)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "clone ID PATH", Args: cobra.ExactArgs(2), Short: "Clone repository", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var src string
			if err := db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, args[0]).Scan(&src); err != nil {
				return fmt.Errorf("repository not found")
			}
			if _, err := git.Run("", "clone", src, args[1]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), args[1])
			return nil
		}},
		&cobra.Command{Use: "refs ID", Args: cobra.ExactArgs(1), Short: "List git refs", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var src string
			if err := db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, args[0]).Scan(&src); err != nil {
				return fmt.Errorf("repository not found")
			}
			out, err := git.Run(src, "show-ref")
			if err != nil {
				// possibly unborn; show HEAD anyway
				fmt.Fprintln(cmd.OutOrStdout(), "(no refs)")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		}},
	)
	return c
}

func newMirrorCmd() *cobra.Command {
	c := &cobra.Command{Use: "mirror", Short: "Local git mirrors"}
	c.AddCommand(
		&cobra.Command{Use: "create --repo ID [--path P]", Short: "Create mirror", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			path, _ := cmd.Flags().GetString("path")
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var src string
			if err := db.QueryRow(`SELECT canonical_path FROM repositories WHERE id=?`, repoID).Scan(&src); err != nil {
				return fmt.Errorf("repository not found")
			}
			if path == "" {
				h := sha256.Sum256([]byte(repoID))
				path = filepath.Join(home, "mirrors", fmt.Sprintf("%x.git", h[:6]))
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if _, err := git.Run("", "clone", "--mirror", src, path); err != nil {
				return err
			}
			id := store.NewID("mir")
			if _, err := db.Exec(`INSERT INTO mirrors(id, repository_id, path, last_synced_at, state) VALUES(?,?,?,?,?)`, id, repoID, path, store.NowUTC(), "synced"); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "sync --repo ID", Short: "Sync mirror", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var mid, mpath string
			if err := db.QueryRow(`SELECT id, path FROM mirrors WHERE repository_id=?`, repoID).Scan(&mid, &mpath); err != nil {
				return fmt.Errorf("no mirror for repo")
			}
			if _, err := git.Run(mpath, "remote", "update"); err != nil {
				_, _ = db.Exec(`UPDATE mirrors SET state='unreachable' WHERE id=?`, mid)
				return err
			}
			_, _ = db.Exec(`UPDATE mirrors SET last_synced_at=?, state='synced' WHERE id=?`, store.NowUTC(), mid)
			fmt.Fprintln(cmd.OutOrStdout(), "synced")
			return nil
		}},
		&cobra.Command{Use: "status --repo ID", Short: "Mirror status", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var mpath, synced, state string
			if err := db.QueryRow(`SELECT path, last_synced_at, state FROM mirrors WHERE repository_id=?`, repoID).Scan(&mpath, &synced, &state); err != nil {
				return fmt.Errorf("no mirror for repo")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "path: %s\nsynced: %s\nstate: %s\n", mpath, synced, state)
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("path", "", "mirror path")
	return c
}
