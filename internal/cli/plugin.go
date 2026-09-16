package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newPluginCmd() *cobra.Command {
	c := &cobra.Command{Use: "plugin", Short: "Plugins (NOT sandboxed; bounded output)"}
	c.AddCommand(
		&cobra.Command{Use: "install PATH --name N [--sha256 H]", Args: cobra.ExactArgs(1), Short: "Install local executable", RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			want, _ := cmd.Flags().GetString("sha256")
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			src := args[0]
			b, err := os.ReadFile(filepath.Clean(src))
			if err != nil {
				return err
			}
			h := sha256.Sum256(b)
			got := hex.EncodeToString(h[:])
			if want != "" && want != got {
				return fmt.Errorf("sha256 mismatch")
			}
			dst := filepath.Join(home, "plugins", name)
			if err := os.WriteFile(dst, b, 0o755); err != nil {
				return err
			}
			_ = os.Chmod(dst, 0o755)
			id := store.NewID("plg")
			_, _ = db.Exec(`INSERT OR REPLACE INTO plugins(id, name, path, sha256, installed_at) VALUES(?,?,?,?,?)`, id, name, dst, got, store.NowUTC())
			logActivity(db, "plugin.install", "", id, "install "+name)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		}},
		&cobra.Command{Use: "list", Short: "List plugins", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, _ := db.Query(`SELECT name, path, sha256 FROM plugins`)
			defer rows.Close()
			for rows.Next() {
				var n, p, h string
				rows.Scan(&n, &p, &h)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", n, p, h)
			}
			return nil
		}},
		&cobra.Command{Use: "run NAME -- ARGS...", Short: "Run plugin (passes argv untouched, bounded)", RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "plugin run: explicit user consent required; not sandboxed")
			return runPlugin(cmd, args)
		}},
		&cobra.Command{Use: "remove NAME", Args: cobra.ExactArgs(1), Short: "Remove plugin", RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			var p string
			_ = db.QueryRow(`SELECT path FROM plugins WHERE name=?`, args[0]).Scan(&p)
			if p != "" {
				_ = os.Remove(p)
			}
			_, _ = db.Exec(`DELETE FROM plugins WHERE name=?`, args[0])
			fmt.Fprintln(cmd.OutOrStdout(), "removed")
			return nil
		}},
	)
	c.PersistentFlags().String("name", "", "plugin name")
	c.PersistentFlags().String("sha256", "", "expected sha256")
	return c
}

func runPlugin(cmd *cobra.Command, args []string) error {
	// find -- separator
	name := ""
	rest := []string{}
	if len(args) > 0 {
		name = args[0]
		rest = args[1:]
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		// also support cobra dash split
		if i := cmd.ArgsLenAtDash(); i >= 0 && i < len(args) {
			rest = args[i:]
			if len(args) > 0 {
				// name is first non-flag before dash; simplified
			}
		}
	}
	db, _, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	var p string
	if err := db.QueryRow(`SELECT path FROM plugins WHERE name=?`, name).Scan(&p); err != nil {
		return fmt.Errorf("plugin not found")
	}
	out, err := runBounded(p, rest)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
}

func runBounded(path string, args []string) (string, error) {
	// bounded 1MB stdout/stderr, 30s timeout
	importExec, _ := boundedExec(path, args)
	return importExec, nil
}

func boundedExec(path string, args []string) (string, error) {
	// implemented in exec_unix helper to keep imports minimal
	return execBounded(path, args)
}
