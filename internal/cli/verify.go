package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/graycodeai/across/internal/git"
	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	c := &cobra.Command{Use: "verify", Short: "Verification evidence"}
	c.AddCommand(
		&cobra.Command{Use: "add --repo ID --name N [--revision R] [--exit-code C]", Short: "Record claim (STATED, not executed)", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			name, _ := cmd.Flags().GetString("name")
			rev, _ := cmd.Flags().GetString("revision")
			code, _ := cmd.Flags().GetInt("exit-code")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			id := store.NewID("ver")
			now := store.NowUTC()
			if _, err := db.Exec(`INSERT INTO verifications(id, repository_id, name, revision_before, revision_after, started_at, finished_at, exit_code, basis) VALUES(?,?,?,?,?,?,?,?,?)`,
				id, repoID, name, rev, rev, now, now, code, "user_recorded"); err != nil {
				return err
			}
			indexDoc(db, "verification", id, repoID, name, "user_recorded claim")
			fmt.Fprintln(cmd.OutOrStdout(), id+" (basis=user_recorded: agent/user claim, NOT Across-executed)")
			return nil
		}},
		&cobra.Command{Use: "run --repo ID --name N -- CMD...", Short: "Execute verification locally (NOT sandboxed)", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			name, _ := cmd.Flags().GetString("name")
			argv := args
			// cobra: find -- separator
			if i := cmd.ArgsLenAtDash(); i >= 0 {
				argv = args[i:]
			}
			if len(argv) == 0 {
				return fmt.Errorf("no command after --")
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
			before := git.Head(canon)
			start := store.NowUTC()
			t0 := time.Now()
			var so, se bytes.Buffer
			ec := exec.Command(argv[0], argv[1:]...)
			ec.Dir = canon
			ec.Env = os.Environ()
			ec.Stdout = &so
			ec.Stderr = &se
			err2 := ec.Run()
			exit := 0
			if err2 != nil {
				if ee, ok := err2.(*exec.ExitError); ok {
					exit = ee.ExitCode()
				} else {
					exit = 1
				}
			}
			_ = t0
			after := git.Head(canon)
			id := store.NewID("ver")
			stdout := bound(so.String(), 4096)
			stderr := bound(se.String(), 4096)
			argvStr := strings.Join(argv, " ")
			if _, err := db.Exec(`INSERT INTO verifications(id, repository_id, name, revision_before, revision_after, argv, cwd, started_at, finished_at, exit_code, stdout_excerpt, stderr_excerpt, basis) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				id, repoID, name, before, after, argvStr, canon, start, store.NowUTC(), exit, stdout, stderr, "executed_by_across_local_runner"); err != nil {
				return err
			}
			note := ""
			if before != after {
				note = " WARNING: revision changed during verification"
			}
			indexDoc(db, "verification", id, repoID, name, stdout)
			fmt.Fprintf(cmd.OutOrStdout(), "%s exit=%d basis=executed_by_across_local_runner%s\n", id, exit, note)
			return nil
		}},
		&cobra.Command{Use: "list [--repo ID]", Short: "List verifications", RunE: func(cmd *cobra.Command, args []string) error {
			repoID, _ := cmd.Flags().GetString("repo")
			db, _, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()
			q := `SELECT id, name, exit_code, basis FROM verifications ORDER BY started_at`
			var rows, err2 = db.Query(q)
			if repoID != "" {
				rows, err2 = db.Query(`SELECT id, name, exit_code, basis FROM verifications WHERE repository_id=? ORDER BY started_at`, repoID)
			}
			if err2 != nil {
				return err2
			}
			defer rows.Close()
			for rows.Next() {
				var id, n, b string
				var e int
				rows.Scan(&id, &n, &e, &b)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\texit=%d\t%s\n", id, n, e, b)
			}
			return nil
		}},
	)
	c.PersistentFlags().String("repo", "", "repository id")
	c.PersistentFlags().String("name", "", "verification name")
	c.PersistentFlags().String("revision", "", "revision")
	c.PersistentFlags().Int("exit-code", 0, "exit code")
	return c
}

func bound(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n[TRUNCATED]"
	}
	return s
}
