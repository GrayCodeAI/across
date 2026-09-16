package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/graycodeai/across/internal/git"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	c := &cobra.Command{Use: "doctor", Short: "Diagnostics", RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		db, home, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		checks := map[string]string{}
		var ok string
		if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
			checks["sqlite_integrity"] = "FAIL: " + ok
		} else {
			checks["sqlite_integrity"] = "ok"
		}
		var v int
		_ = db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)
		checks["schema_version"] = fmt.Sprint(v)
		if _, err := db.Exec(`SELECT count(*) FROM search_index`); err != nil {
			checks["search_index"] = "FAIL"
		} else {
			checks["search_index"] = "ok"
		}
		rows, _ := db.Query(`SELECT id, canonical_path FROM repositories`)
		bad := 0
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var id, p string
				rows.Scan(&id, &p)
				if _, err := os.Stat(p); err != nil {
					bad++
				}
			}
		}
		checks["repo_paths_missing"] = fmt.Sprint(bad)
		if _, err := git.Run("", "--version"); err != nil {
			checks["git"] = "FAIL"
		} else {
			checks["git"] = "ok"
		}
		checks["home"] = home
		if asJSON {
			fmt.Fprintf(cmd.OutOrStdout(), "{\"sqlite_integrity\":%q,\"schema_version\":%q,\"search_index\":%q,\"repo_paths_missing\":%q,\"git\":%q}\n",
				checks["sqlite_integrity"], checks["schema_version"], checks["search_index"], checks["repo_paths_missing"], checks["git"])
			return nil
		}
		for k, val := range checks {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", k, val)
		}
		return nil
	}}
	c.Flags().Bool("json", false, "json output")
	return c
}

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "clean", Short: "Clean Across-owned temp/cache only", RunE: func(cmd *cobra.Command, args []string) error {
		dry, _ := cmd.Flags().GetBool("dry-run")
		_, home, err := openDB()
		if err != nil {
			return err
		}
		tmp := filepath.Join(home, "tmp")
		entries, _ := os.ReadDir(tmp)
		for _, e := range entries {
			p := filepath.Join(tmp, e.Name())
			label := "remove"
			if dry {
				label = "would-remove"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", label, p)
			if !dry {
				_ = os.RemoveAll(p)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "never deletes user repository state")
		return nil
	}}
	cmd.Flags().Bool("dry-run", false, "dry run")
	return cmd
}

func newActivityCmd() *cobra.Command {
	return &cobra.Command{Use: "activity", Short: "Activity feed", RunE: func(cmd *cobra.Command, args []string) error {
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		rows, _ := db.Query(`SELECT kind, ref_id, summary, occurred_at FROM activities ORDER BY occurred_at DESC LIMIT 100`)
		defer rows.Close()
		for rows.Next() {
			var k, r, s, a string
			rows.Scan(&k, &r, &s, &a)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s %s\n", a, k, r, s)
		}
		return nil
	}}
}

func newRecapCmd() *cobra.Command {
	return &cobra.Command{Use: "recap", Short: "Recent recap", RunE: func(cmd *cobra.Command, args []string) error {
		db, _, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		rows, _ := db.Query(`SELECT kind, ref_id, summary, occurred_at FROM activities ORDER BY occurred_at DESC LIMIT 20`)
		defer rows.Close()
		for rows.Next() {
			var k, r, s, a string
			rows.Scan(&k, &r, &s, &a)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", a, k, s)
		}
		return nil
	}}
}
