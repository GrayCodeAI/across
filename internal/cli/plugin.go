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
		&cobra.Command{Use: "install PATH --name N [--sha256 H]", Args: cobra.ExactArgs(1), Short: "Install local executable", PreRunE: requiredFlags("name"), RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if err := requireSafeName(name); err != nil {
				return err
			}
			want, _ := cmd.Flags().GetString("sha256")
			db, home, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			resolvedHome, err := requireExistingDirectory(home)
			if err != nil {
				return err
			}
			home = resolvedHome
			src, err := requireExistingFile(args[0])
			if err != nil {
				return err
			}
			b, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			h := sha256.Sum256(b)
			got := hex.EncodeToString(h[:])
			if want != "" && want != got {
				return fmt.Errorf("sha256 mismatch")
			}
			pluginsRoot := filepath.Join(home, "plugins")
			if err := os.MkdirAll(pluginsRoot, 0o755); err != nil {
				return err
			}
			if err := requireDirectoryChain(pluginsRoot); err != nil {
				return err
			}
			dst, err := requireOutputFile(filepath.Join(pluginsRoot, name))
			if err != nil {
				return err
			}
			if err := os.WriteFile(dst, b, 0o755); err != nil {
				return err
			}
			_ = os.Chmod(dst, 0o755)
			id := store.NewID("plg")
			if _, err := db.Exec(`INSERT OR REPLACE INTO plugins(id, name, path, sha256, installed_at) VALUES(?,?,?,?,?)`, id, name, dst, got, store.NowUTC()); err != nil {
				return err
			}
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
		&cobra.Command{Use: "run NAME -- ARGS...", Args: cobra.MinimumNArgs(1), Short: "Run plugin (passes argv untouched, bounded)", RunE: func(cmd *cobra.Command, args []string) error {
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
			if err := db.QueryRow(`SELECT path FROM plugins WHERE name=?`, args[0]).Scan(&p); err != nil {
				return notFound("plugin %q not found", args[0])
			}
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
			if _, err := db.Exec(`DELETE FROM plugins WHERE name=?`, args[0]); err != nil {
				return err
			}
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
		if err := requireSafeName(name); err != nil {
			return err
		}
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
	db, home, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	resolvedHome, err := requireExistingDirectory(home)
	if err != nil {
		return err
	}
	var p string
	if err := db.QueryRow(`SELECT path FROM plugins WHERE name=?`, name).Scan(&p); err != nil {
		return notFound("plugin %q not found", name)
	}
	resolved, err := requireExistingFile(p)
	if err != nil {
		return err
	}
	if !pathWithin(filepath.Join(resolvedHome, "plugins"), resolved) {
		return invalidArgument("plugin path is outside the Across plugin directory")
	}
	out, err := runBounded(resolved, rest)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), out)
	return nil
}

func runBounded(path string, args []string) (string, error) {
	return boundedExec(path, args)
}

func boundedExec(path string, args []string) (string, error) {
	// implemented in exec_unix helper to keep imports minimal
	return execBounded(path, args)
}
