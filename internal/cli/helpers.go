package cli

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

var homeDir string

func openDB() (*sql.DB, string, error) {
	home := homeDir
	if home == "" {
		return nil, "", fmt.Errorf("home not set")
	}
	db, err := store.Open(home)
	if err != nil {
		return nil, "", err
	}
	return db, home, nil
}

func logActivity(db *sql.DB, kind, repoID, refID, summary string) {
	_, _ = db.Exec(`INSERT INTO activities(id, kind, repository_id, ref_id, summary, occurred_at) VALUES(?,?,?,?,?,?)`,
		store.NewID("act"), kind, repoID, refID, summary, store.NowUTC())
}

func indexDoc(db *sql.DB, kind, refID, repoID, title, body string) {
	_, _ = db.Exec(`DELETE FROM search_index WHERE kind=? AND ref_id=?`, kind, refID)
	_, _ = db.Exec(`INSERT INTO search_index(kind, ref_id, repository_id, title, body) VALUES(?,?,?,?,?)`,
		kind, refID, repoID, title, body)
}

// searchDocs performs portable substring search over the search_index table.
// (v0.0.1: LIKE-based; FTS5 deferred until it can be guaranteed in default builds.)
func searchDocs(db *sql.DB, query, repoID string) (*sql.Rows, error) {
	pat := "%" + strings.ReplaceAll(query, "%", "\\%") + "%"
	if repoID != "" {
		return db.Query(`SELECT kind, ref_id, repository_id, title, substr(body,1,200) FROM search_index
			WHERE (title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\') AND repository_id=? LIMIT 50`, pat, pat, repoID)
	}
	return db.Query(`SELECT kind, ref_id, repository_id, title, substr(body,1,200) FROM search_index
		WHERE title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\' LIMIT 50`, pat, pat)
}

func repoMustExist(db *sql.DB, id string) (string, string, error) {
	var canon, def string
	err := db.QueryRow(`SELECT canonical_path, default_branch FROM repositories WHERE id=?`, id).Scan(&canon, &def)
	if err != nil {
		return "", "", fmt.Errorf("repository %q not found", id)
	}
	return canon, def, nil
}

func AddCommands(root *cobra.Command) {
	root.AddCommand(
		newVersionCmd(),
		newRepoCmd(),
		newMirrorCmd(),
		newSessionCmd(),
		newSourceCmd(),
		newCheckpointCmd(),
		newWorkspaceCmd(),
		newVersionSetCmd(),
		newMemoryCmd(),
		newSearchCmd(),
		newBriefCmd(),
		newHandoffCmd(),
		newDossierCmd(),
		newContextCmd(),
		newVerifyCmd(),
		newCodeCmd(),
		newIndexCmd(),
		newGraphCmd(),
		newWhyCmd(),
		newInvestigateCmd(),
		newReviewCmd(),
		newIssueCmd(),
		newChangeCmd(),
		newBranchRuleCmd(),
		newQueueCmd(),
		newControlCmd(),
		newTokenCmd(),
		newServeCmd(),
		newMCPCmd(),
		newPluginCmd(),
		newBackupCmd(),
		newDoctorCmd(),
		newCleanCmd(),
		newActivityCmd(),
		newRecapCmd(),
		newAgentHelpCmd(),
		newHookCmd(),
		newAgentCmd(),
	)
}
