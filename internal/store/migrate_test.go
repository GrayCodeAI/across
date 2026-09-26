package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMigrate(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "home"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v); err != nil || v < 2 {
		t.Fatalf("migrations not applied: v=%d err=%v", v, err)
	}
	var ok string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
		t.Fatalf("integrity: %v %s", err, ok)
	}
}

// TestMigrateFromV1 builds an old-schema (v1 only) fixture with history, then
// migrates and verifies history/provenance preserved (§102). Anything that
// cannot establish provenance must read as unknown, never guessed.
func TestMigrateFromV1(t *testing.T) {
	db := openMigrationTestDB(t, filepath.Join(t.TempDir(), "across.db"))
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migration001); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	now := NowUTC()
	rid, sid, cpid := NewID("repo"), NewID("sess"), NewID("cp")
	if _, err := db.Exec(`INSERT INTO repositories(id, display_name, canonical_path, git_common_dir, authority_mode, default_branch, created_at) VALUES(?,?,?,?,?,?,?)`,
		rid, "legacy", "/tmp/legacy", "", "external", "main", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sources(id, repository_id, kind, origin, captured_at, revision, revision_basis) VALUES(?,?,?,?,?,?,?)`,
		NewID("src"), rid, "session", "legacy", now, "abc123", "legacy_revision_basis_unknown"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(id, repository_id, agent, state, started_at, last_event_at) VALUES(?,?,?,?,?,?)`,
		sid, rid, "codex", "active", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO checkpoints(id, repository_id, revision, session_id, created_at, message, basis) VALUES(?,?,?,?,?,?,?)`,
		cpid, rid, "abc123", sid, now, "legacy", "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != len(migrations) {
		t.Fatalf("latest migration = %d, want %d", v, len(migrations))
	}
	var basis, cpb string
	if err := db.QueryRow(`SELECT revision_basis FROM sources LIMIT 1`).Scan(&basis); err != nil {
		t.Fatal(err)
	}
	if basis != "legacy_revision_basis_unknown" {
		t.Fatalf("provenance altered: %q", basis)
	}
	if err := db.QueryRow(`SELECT basis FROM checkpoints WHERE id=?`, cpid).Scan(&cpb); err != nil {
		t.Fatal(err)
	}
	if cpb != "unknown" {
		t.Fatalf("checkpoint basis altered: %q", cpb)
	}
}

func TestMigrateRejectsFutureVersion(t *testing.T) {
	db := openMigrationTestDB(t, filepath.Join(t.TempDir(), "across.db"))
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	futureVersion := migrations[len(migrations)-1].Version + 1
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (?, ?, datetime('now'))`, futureVersion, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	err := Migrate(db)
	if err == nil || !strings.Contains(err.Error(), "future schema version") {
		t.Fatalf("expected future schema version rejection, got %v", err)
	}
}

func TestMigrateRejectsChecksumMismatch(t *testing.T) {
	db := openMigrationTestDB(t, filepath.Join(t.TempDir(), "across.db"))
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum=? WHERE version=1`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	err := Migrate(db)
	if err == nil || !strings.Contains(err.Error(), "migration 1 checksum mismatch") {
		t.Fatalf("expected migration checksum rejection, got %v", err)
	}
}

func TestMigrateRejectsVersionGap(t *testing.T) {
	db := openMigrationTestDB(t, filepath.Join(t.TempDir(), "across.db"))
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=2`); err != nil {
		t.Fatal(err)
	}
	err := Migrate(db)
	if err == nil || !strings.Contains(err.Error(), "schema migration gap") {
		t.Fatalf("expected schema migration gap rejection, got %v", err)
	}
}

func TestConcurrentMigrateCalls(t *testing.T) {
	const callers = 64
	db := openMigrationTestDB(t, filepath.Join(t.TempDir(), "across.db"))
	db.SetMaxOpenConns(callers)
	runConcurrent(t, callers, func() error { return Migrate(db) })

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(migrations) {
		t.Fatalf("migration count = %d, want %d", count, len(migrations))
	}
}

func TestConcurrentFirstOpen(t *testing.T) {
	const callers = 100
	home := filepath.Join(t.TempDir(), "home")
	runConcurrent(t, callers, func() error {
		db, err := Open(home)
		if err != nil {
			return err
		}
		return db.Close()
	})

	db, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(migrations) {
		t.Fatalf("migration count = %d, want %d", count, len(migrations))
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	before := readMigrationLedger(t, db)
	for i := 0; i < 3; i++ {
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	after := readMigrationLedger(t, db)
	if len(after) != len(migrations) {
		t.Fatalf("migration count = %d, want %d", len(after), len(migrations))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("migration %d changed after repeated Migrate: before=%+v after=%+v", i+1, before[i], after[i])
		}
	}
}

func TestMigrateFromVersion001Database(t *testing.T) {
	db := openMigrationTestDB(t, filepath.Join(t.TempDir(), "across.db"))
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{migration001, migration002, migration003} {
		if _, err := db.Exec(body); err != nil {
			t.Fatal(err)
		}
	}
	for version := 1; version <= 3; version++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, datetime('now'))`, version); err != nil {
			t.Fatal(err)
		}
	}
	repositoryID := NewID("repo")
	if _, err := db.Exec(`INSERT INTO repositories(id, display_name, canonical_path, created_at) VALUES (?, ?, ?, ?)`, repositoryID, "legacy", "/tmp/legacy", NowUTC()); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	ledger := readMigrationLedger(t, db)
	if len(ledger) != len(migrations) {
		t.Fatalf("migration count = %d, want %d", len(ledger), len(migrations))
	}
	manifest, _, err := migrationManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range ledger {
		if row.checksum != manifest[row.version] {
			t.Fatalf("migration %d checksum = %q, want %q", row.version, row.checksum, manifest[row.version])
		}
	}
	var preserved int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE id=? AND display_name='legacy'`, repositoryID).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != 1 {
		t.Fatalf("legacy repository rows = %d, want 1", preserved)
	}
}

func TestMigrationIndexes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	indexes := []string{
		"idx_sources_repository_captured",
		"idx_source_events_source_seq",
		"idx_sessions_repository_started",
		"idx_checkpoints_repository_created",
		"idx_memories_repository_created",
		"idx_verifications_repository_started",
		"idx_issues_repository_created",
		"idx_issue_comments_issue_created",
		"idx_changes_repository_created",
		"idx_change_approvals_change_decision",
		"idx_workspaces_repository_state",
		"idx_code_symbols_repository_file_symbol",
		"idx_code_relations_repository_to",
		"idx_activities_occurred",
	}
	for _, index := range indexes {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type='index' AND name=?`, index).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("index %s count = %d, want 1", index, count)
		}
	}
}

type migrationLedgerRow struct {
	version   int
	checksum  string
	appliedAt string
}

func readMigrationLedger(t *testing.T, db *sql.DB) []migrationLedgerRow {
	t.Helper()
	rows, err := db.Query(`SELECT version, checksum, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var ledger []migrationLedgerRow
	for rows.Next() {
		var row migrationLedgerRow
		if err := rows.Scan(&row.version, &row.checksum, &row.appliedAt); err != nil {
			t.Fatal(err)
		}
		ledger = append(ledger, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ledger
}

func openMigrationTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func runConcurrent(t *testing.T, count int, fn func() error) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, count)
	var ready sync.WaitGroup
	ready.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			ready.Done()
			<-start
			errs <- fn()
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < count; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent call %d: %v", i+1, err)
		}
	}
}
