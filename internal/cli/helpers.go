package cli

import (
	"context"
	"database/sql"
	"strings"

	"github.com/graycodeai/across/internal/store"
	"github.com/spf13/cobra"
)

var homeDir string

func openDB() (*sql.DB, string, error) {
	home := homeDir
	if home == "" {
		return nil, "", invalidArgument("--home must not be empty")
	}
	db, err := store.Open(home)
	if err != nil {
		return nil, "", err
	}
	return db, home, nil
}

type mutationExecutor interface {
	Exec(string, ...any) (sql.Result, error)
}

type rowQuerier interface {
	QueryRow(string, ...any) *sql.Row
}

type rowsQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

type sqlRunner interface {
	mutationExecutor
	rowQuerier
	rowsQuerier
}

type connectionRunner struct {
	conn *sql.Conn
}

func (r connectionRunner) Exec(query string, args ...any) (sql.Result, error) {
	return r.conn.ExecContext(context.Background(), query, args...)
}

func (r connectionRunner) Query(query string, args ...any) (*sql.Rows, error) {
	return r.conn.QueryContext(context.Background(), query, args...)
}

func (r connectionRunner) QueryRow(query string, args ...any) *sql.Row {
	return r.conn.QueryRowContext(context.Background(), query, args...)
}

func withTx(db *sql.DB, fn func(sqlRunner) error) error {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	begun := false
	committed := false
	defer func() {
		if begun && !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	begun = true
	if err := fn(connectionRunner{conn: conn}); err != nil {
		return err
	}
	if _, err := conn.ExecContext(context.Background(), `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func writeActivity(exec mutationExecutor, kind, repoID, refID, summary string) error {
	_, err := exec.Exec(`INSERT INTO activities(id, kind, repository_id, ref_id, summary, occurred_at) VALUES(?,?,?,?,?,?)`,
		store.NewID("act"), kind, repoID, refID, summary, store.NowUTC())
	return err
}

func writeIndex(exec mutationExecutor, kind, refID, repoID, title, body string) error {
	if _, err := exec.Exec(`DELETE FROM search_index WHERE kind=? AND ref_id=?`, kind, refID); err != nil {
		return err
	}
	_, err := exec.Exec(`INSERT INTO search_index(kind, ref_id, repository_id, title, body) VALUES(?,?,?,?,?)`,
		kind, refID, repoID, title, body)
	return err
}

func logActivity(db *sql.DB, kind, repoID, refID, summary string) {
	_ = writeActivity(db, kind, repoID, refID, summary)
}

func logActivityTx(tx sqlRunner, kind, repoID, refID, summary string) error {
	return writeActivity(tx, kind, repoID, refID, summary)
}

func indexDoc(db *sql.DB, kind, refID, repoID, title, body string) {
	_ = writeIndex(db, kind, refID, repoID, title, body)
}

func indexDocTx(tx sqlRunner, kind, refID, repoID, title, body string) error {
	return writeIndex(tx, kind, refID, repoID, title, body)
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

type sessionInfo struct {
	id              string
	repositoryID    string
	agent           string
	nativeSessionID string
	state           string
}

type memoryInfo struct {
	repositoryID string
	state        string
	title        string
	body         string
}

func repoMustExistQ(q rowQuerier, id string) (string, string, error) {
	var canon, def string
	err := q.QueryRow(`SELECT canonical_path, default_branch FROM repositories WHERE id=?`, id).Scan(&canon, &def)
	if err == sql.ErrNoRows {
		return "", "", notFound("repository %q not found", id)
	}
	if err != nil {
		return "", "", operationFailed("query repository %q: %v", id, err)
	}
	return canon, def, nil
}

func repoMustExist(db *sql.DB, id string) (string, string, error) {
	return repoMustExistQ(db, id)
}

func sessionMustBelong(q rowQuerier, repoID, sessionID string, active bool) (sessionInfo, error) {
	if sessionID == "" {
		return sessionInfo{}, nil
	}
	var repositoryID, agent, native, state sql.NullString
	err := q.QueryRow(`SELECT repository_id, agent, native_session_id, state FROM sessions WHERE id=?`, sessionID).Scan(&repositoryID, &agent, &native, &state)
	if err == sql.ErrNoRows {
		return sessionInfo{}, notFound("session %q not found", sessionID)
	}
	if err != nil {
		return sessionInfo{}, operationFailed("query session %q: %v", sessionID, err)
	}
	if !repositoryID.Valid || repositoryID.String == "" {
		return sessionInfo{}, conflict("session %q has no repository", sessionID)
	}
	if repositoryID.String != repoID {
		return sessionInfo{}, conflict("session %q belongs to repository %q", sessionID, repositoryID.String)
	}
	if active && (!state.Valid || state.String != "active") {
		return sessionInfo{}, conflict("session %q is not active", sessionID)
	}
	return sessionInfo{
		id:              sessionID,
		repositoryID:    repositoryID.String,
		agent:           agent.String,
		nativeSessionID: native.String,
		state:           state.String,
	}, nil
}

func sourceMustBelong(q rowQuerier, repoID, sourceID string) error {
	if sourceID == "" {
		return nil
	}
	var repositoryID, deletedAt sql.NullString
	err := q.QueryRow(`SELECT repository_id, deleted_at FROM sources WHERE id=?`, sourceID).Scan(&repositoryID, &deletedAt)
	if err == sql.ErrNoRows {
		return notFound("source %q not found", sourceID)
	}
	if err != nil {
		return operationFailed("query source %q: %v", sourceID, err)
	}
	if !repositoryID.Valid || repositoryID.String == "" {
		return conflict("source %q has no repository", sourceID)
	}
	if repositoryID.String != repoID {
		return conflict("source %q belongs to repository %q", sourceID, repositoryID.String)
	}
	if deletedAt.Valid && deletedAt.String != "" {
		return conflict("source %q is deleted", sourceID)
	}
	return nil
}

func memoryMustExist(q rowQuerier, id string) (memoryInfo, error) {
	var repositoryID, state, title, body sql.NullString
	err := q.QueryRow(`SELECT repository_id, state, title, body FROM memories WHERE id=?`, id).Scan(&repositoryID, &state, &title, &body)
	if err == sql.ErrNoRows {
		return memoryInfo{}, notFound("memory %q not found", id)
	}
	if err != nil {
		return memoryInfo{}, operationFailed("query memory %q: %v", id, err)
	}
	if !repositoryID.Valid || repositoryID.String == "" {
		return memoryInfo{}, conflict("memory %q has no repository", id)
	}
	return memoryInfo{
		repositoryID: repositoryID.String,
		state:        state.String,
		title:        title.String,
		body:         body.String,
	}, nil
}

func issueMustBelong(q rowQuerier, repoID, issueID string) error {
	if issueID == "" {
		return nil
	}
	var repositoryID sql.NullString
	err := q.QueryRow(`SELECT repository_id FROM issues WHERE id=?`, issueID).Scan(&repositoryID)
	if err == sql.ErrNoRows {
		return notFound("issue %q not found", issueID)
	}
	if err != nil {
		return operationFailed("query issue %q: %v", issueID, err)
	}
	if !repositoryID.Valid || repositoryID.String == "" {
		return conflict("issue %q has no repository", issueID)
	}
	if repositoryID.String != repoID {
		return conflict("issue %q belongs to repository %q", issueID, repositoryID.String)
	}
	return nil
}

func changeMustBelong(q rowQuerier, repoID, changeID string) error {
	if changeID == "" {
		return nil
	}
	var repositoryID sql.NullString
	err := q.QueryRow(`SELECT repository_id FROM changes WHERE id=?`, changeID).Scan(&repositoryID)
	if err == sql.ErrNoRows {
		return notFound("change %q not found", changeID)
	}
	if err != nil {
		return operationFailed("query change %q: %v", changeID, err)
	}
	if !repositoryID.Valid || repositoryID.String == "" {
		return conflict("change %q has no repository", changeID)
	}
	if repositoryID.String != repoID {
		return conflict("change %q belongs to repository %q", changeID, repositoryID.String)
	}
	return nil
}

func principalMustExist(q rowQuerier, principalID string) error {
	if principalID == "" {
		return nil
	}
	var exists int
	err := q.QueryRow(`SELECT 1 FROM principals WHERE id=?`, principalID).Scan(&exists)
	if err == sql.ErrNoRows {
		return notFound("principal %q not found", principalID)
	}
	if err != nil {
		return operationFailed("query principal %q: %v", principalID, err)
	}
	return nil
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
	configureCommandContracts(root)
}
