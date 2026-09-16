package store

import (
	"path/filepath"
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
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	db, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate v1 state: drop v2 objects, reset version, insert legacy rows
	// with unknown revision basis.
	if _, err := db.Exec(`DROP TABLE IF EXISTS search_index`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version >= 2`); err != nil {
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
	db.Close()
	// Reopen → Migrate must apply v2 without destroying v1 rows.
	db2, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var v int
	_ = db2.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)
	if v < 2 {
		t.Fatalf("v2 not applied after reopen: %d", v)
	}
	var basis, cpb string
	_ = db2.QueryRow(`SELECT revision_basis FROM sources LIMIT 1`).Scan(&basis)
	if basis != "legacy_revision_basis_unknown" {
		t.Fatalf("provenance altered: %q", basis)
	}
	_ = db2.QueryRow(`SELECT basis FROM checkpoints WHERE id=?`, cpid).Scan(&cpb)
	if cpb != "unknown" {
		t.Fatalf("checkpoint basis altered: %q", cpb)
	}
}
