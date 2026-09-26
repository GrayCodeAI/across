package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
)

type migration struct {
	Version int
	SQL     string
}

// migrations are numbered and transactional where possible.
var migrations = []migration{
	{1, migration001},
	{2, migration002},
	{3, migration003},
	{4, migration004},
	{5, migration005},
}

func Migrate(db *sql.DB) error {
	if db == nil {
		return errors.New("migrate: nil database")
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return fmt.Errorf("migrate: set busy timeout: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return fmt.Errorf("migrate: enable foreign keys: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("migrate: begin immediate transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	if err := migrateLocked(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("migrate: commit: %w", err)
	}
	committed = true
	return nil
}

func migrateLocked(ctx context.Context, conn *sql.Conn) error {
	manifest, latest, err := migrationManifest()
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("migrate: create migration ledger: %w", err)
	}
	legacyChecksums, err := ensureMigrationChecksumColumn(ctx, conn)
	if err != nil {
		return err
	}
	applied, legacy, err := readAppliedMigrations(ctx, conn, manifest, latest, legacyChecksums)
	if err != nil {
		return err
	}
	for version, checksum := range legacy {
		if _, err := conn.ExecContext(ctx, `UPDATE schema_migrations SET checksum=? WHERE version=?`, checksum, version); err != nil {
			return fmt.Errorf("migrate: backfill migration %d checksum: %w", version, err)
		}
	}
	for _, m := range migrations {
		if _, ok := applied[m.Version]; ok {
			continue
		}
		if _, err := conn.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("migration %d: %w", m.Version, err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (?, ?, datetime('now'))`, m.Version, manifest[m.Version]); err != nil {
			return fmt.Errorf("migration %d: record checksum: %w", m.Version, err)
		}
	}
	return nil
}

func migrationManifest() (map[int]string, int, error) {
	if len(migrations) == 0 {
		return nil, 0, errors.New("migrate: migration manifest is empty")
	}
	manifest := make(map[int]string, len(migrations))
	for i, m := range migrations {
		expected := i + 1
		if m.Version != expected {
			return nil, 0, fmt.Errorf("migrate: migration manifest expected version %d, found %d", expected, m.Version)
		}
		if m.SQL == "" {
			return nil, 0, fmt.Errorf("migrate: migration %d has no SQL", m.Version)
		}
		sum := sha256.Sum256([]byte(m.SQL))
		manifest[m.Version] = fmt.Sprintf("%x", sum)
	}
	return manifest, migrations[len(migrations)-1].Version, nil
}

func ensureMigrationChecksumColumn(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, err := conn.QueryContext(ctx, `PRAGMA table_info(schema_migrations)`)
	if err != nil {
		return false, fmt.Errorf("migrate: inspect migration ledger: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("migrate: inspect migration ledger: %w", err)
		}
		if name == "checksum" {
			if err := rows.Err(); err != nil {
				return false, fmt.Errorf("migrate: inspect migration ledger: %w", err)
			}
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("migrate: inspect migration ledger: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN checksum TEXT NOT NULL DEFAULT ''`); err != nil {
		return false, fmt.Errorf("migrate: add migration checksum column: %w", err)
	}
	return true, nil
}

func readAppliedMigrations(ctx context.Context, conn *sql.Conn, manifest map[int]string, latest int, allowLegacyChecksums bool) (map[int]struct{}, map[int]string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: read migration ledger: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]struct{}, len(manifest))
	legacy := make(map[int]string)
	expectedVersion := 1
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, nil, fmt.Errorf("migrate: read migration ledger: %w", err)
		}
		if version <= 0 {
			return nil, nil, fmt.Errorf("migrate: invalid schema migration version %d", version)
		}
		if version > latest {
			return nil, nil, fmt.Errorf("migrate: unsupported future schema version %d; latest supported version is %d", version, latest)
		}
		expectedChecksum, ok := manifest[version]
		if !ok {
			return nil, nil, fmt.Errorf("migrate: unknown schema migration version %d", version)
		}
		if version != expectedVersion {
			return nil, nil, fmt.Errorf("migrate: schema migration gap before version %d; expected version %d", version, expectedVersion)
		}
		if checksum != expectedChecksum {
			if allowLegacyChecksums && checksum == "" {
				legacy[version] = expectedChecksum
			} else {
				return nil, nil, fmt.Errorf("migrate: migration %d checksum mismatch: expected %s, got %s", version, expectedChecksum, checksum)
			}
		}
		applied[version] = struct{}{}
		expectedVersion++
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("migrate: read migration ledger: %w", err)
	}
	return applied, legacy, nil
}

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY CHECK (version > 0),
  checksum TEXT NOT NULL CHECK (length(checksum) = 64 AND checksum NOT GLOB '*[^0-9a-f]*'),
  applied_at TEXT NOT NULL CHECK (length(applied_at) > 0)
);`

const migration001 = `
CREATE TABLE IF NOT EXISTS repositories (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  canonical_path TEXT NOT NULL,
  git_common_dir TEXT NOT NULL DEFAULT '',
  authority_mode TEXT NOT NULL DEFAULT 'external',
  default_branch TEXT NOT NULL DEFAULT 'main',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sources (
  id TEXT PRIMARY KEY,
  repository_id TEXT REFERENCES repositories(id),
  kind TEXT NOT NULL,
  origin TEXT NOT NULL DEFAULT '',
  native_id TEXT NOT NULL DEFAULT '',
  captured_at TEXT NOT NULL,
  revision TEXT NOT NULL DEFAULT '',
  revision_basis TEXT NOT NULL DEFAULT 'unknown',
  superseded_by TEXT NOT NULL DEFAULT '',
  deleted_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS source_events (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL DEFAULT 0,
  event_type TEXT NOT NULL,
  provider_event_type TEXT NOT NULL DEFAULT '',
  body_text TEXT NOT NULL DEFAULT '',
  agent TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL DEFAULT '',
  turn_id TEXT NOT NULL DEFAULT '',
  parent_session_id TEXT NOT NULL DEFAULT '',
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cached_tokens INTEGER NOT NULL DEFAULT 0,
  cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
  recorded_cost REAL NOT NULL DEFAULT 0,
  occurred_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  repository_id TEXT REFERENCES repositories(id),
  agent TEXT NOT NULL DEFAULT '',
  native_session_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'active',
  started_at TEXT NOT NULL,
  ended_at TEXT NOT NULL DEFAULT '',
  last_event_at TEXT NOT NULL DEFAULT '',
  latest_checkpoint_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS session_state (
  session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
  store TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS checkpoints (
  id TEXT PRIMARY KEY,
  repository_id TEXT REFERENCES repositories(id),
  revision TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  basis TEXT NOT NULL DEFAULT 'unknown',
  agent TEXT NOT NULL DEFAULT '',
  native_session_id TEXT NOT NULL DEFAULT '',
  task TEXT NOT NULL DEFAULT '',
  issue_id TEXT NOT NULL DEFAULT '',
  change_id TEXT NOT NULL DEFAULT '',
  version_set_id TEXT NOT NULL DEFAULT '',
  verification_id TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS memories (
  id TEXT PRIMARY KEY,
  repository_id TEXT REFERENCES repositories(id),
  kind TEXT NOT NULL DEFAULT 'note',
  state TEXT NOT NULL DEFAULT 'candidate',
  title TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT '',
  superseded_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS memory_sources (
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  source_id TEXT NOT NULL,
  PRIMARY KEY (memory_id, source_id)
);
CREATE TABLE IF NOT EXISTS verifications (
  id TEXT PRIMARY KEY,
  repository_id TEXT REFERENCES repositories(id),
  name TEXT NOT NULL,
  revision_before TEXT NOT NULL DEFAULT '',
  revision_after TEXT NOT NULL DEFAULT '',
  argv TEXT NOT NULL DEFAULT '',
  cwd TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL DEFAULT '',
  exit_code INTEGER NOT NULL DEFAULT 0,
  stdout_excerpt TEXT NOT NULL DEFAULT '',
  stderr_excerpt TEXT NOT NULL DEFAULT '',
  basis TEXT NOT NULL DEFAULT 'user_recorded'
);
CREATE TABLE IF NOT EXISTS organizations (id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, org_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS principals (id TEXT PRIMARY KEY, org_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS grants (id TEXT PRIMARY KEY, principal_id TEXT NOT NULL, scope TEXT NOT NULL DEFAULT '', permission TEXT NOT NULL DEFAULT 'read', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS principal_tokens (id TEXT PRIMARY KEY, principal_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', hash TEXT NOT NULL, created_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS issues (
  id TEXT PRIMARY KEY, repository_id TEXT REFERENCES repositories(id),
  title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'open',
  created_at TEXT NOT NULL, closed_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS issue_comments (id TEXT PRIMARY KEY, issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE, body TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS changes (
  id TEXT PRIMARY KEY, repository_id TEXT REFERENCES repositories(id),
  title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', base TEXT NOT NULL DEFAULT 'main', head TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'open', created_at TEXT NOT NULL, merged_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS change_approvals (id TEXT PRIMARY KEY, change_id TEXT NOT NULL REFERENCES changes(id) ON DELETE CASCADE, principal TEXT NOT NULL DEFAULT '', decision TEXT NOT NULL DEFAULT 'approve', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS branch_rules (id TEXT PRIMARY KEY, repository_id TEXT REFERENCES repositories(id), pattern TEXT NOT NULL, min_approvals INTEGER NOT NULL DEFAULT 0, required_verifications TEXT NOT NULL DEFAULT '', allow_direct_push INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS merge_queue (id TEXT PRIMARY KEY, change_id TEXT NOT NULL, base_sha TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'queued', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY, repository_id TEXT REFERENCES repositories(id),
  revision TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL DEFAULT '', path TEXT NOT NULL,
  created_at TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'active'
);
CREATE TABLE IF NOT EXISTS version_sets (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS version_set_entries (version_set_id TEXT NOT NULL REFERENCES version_sets(id) ON DELETE CASCADE, repository_id TEXT NOT NULL, revision TEXT NOT NULL, PRIMARY KEY (version_set_id, repository_id));
CREATE TABLE IF NOT EXISTS code_symbols (
  id TEXT PRIMARY KEY, repository_id TEXT REFERENCES repositories(id),
  file TEXT NOT NULL, language TEXT NOT NULL DEFAULT '', symbol TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT '', signature TEXT NOT NULL DEFAULT '',
  start_line INTEGER NOT NULL DEFAULT 0, end_line INTEGER NOT NULL DEFAULT 0,
  revision TEXT NOT NULL DEFAULT '', analysis_quality TEXT NOT NULL DEFAULT 'heuristic'
);
CREATE TABLE IF NOT EXISTS code_relations (
  id TEXT PRIMARY KEY, repository_id TEXT REFERENCES repositories(id),
  from_symbol TEXT NOT NULL, to_symbol TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'references',
  basis TEXT NOT NULL DEFAULT 'heuristic', confidence REAL NOT NULL DEFAULT 0.5, revision TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS plugins (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, path TEXT NOT NULL, sha256 TEXT NOT NULL DEFAULT '', installed_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS activities (id TEXT PRIMARY KEY, kind TEXT NOT NULL, repository_id TEXT NOT NULL DEFAULT '', ref_id TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', occurred_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tombstones (id TEXT PRIMARY KEY, target_kind TEXT NOT NULL, target_id TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS mirrors (id TEXT PRIMARY KEY, repository_id TEXT REFERENCES repositories(id), path TEXT NOT NULL, last_synced_at TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'unknown');
`

const migration002 = `
CREATE TABLE IF NOT EXISTS search_index (
  kind TEXT NOT NULL,
  ref_id TEXT NOT NULL,
  repository_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_search_index_ref ON search_index(kind, ref_id);
`

const migration003 = `
CREATE TABLE IF NOT EXISTS source_event_metadata (
  source_event_id TEXT NOT NULL REFERENCES source_events(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (source_event_id, key)
);
CREATE TABLE IF NOT EXISTS session_state (
  session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
  store TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);
`

const migration004 = `
CREATE INDEX IF NOT EXISTS idx_sources_repository_captured ON sources(repository_id, captured_at);
CREATE INDEX IF NOT EXISTS idx_source_events_source_seq ON source_events(source_id, seq);
CREATE INDEX IF NOT EXISTS idx_sessions_repository_started ON sessions(repository_id, started_at);
CREATE INDEX IF NOT EXISTS idx_checkpoints_repository_created ON checkpoints(repository_id, created_at);
CREATE INDEX IF NOT EXISTS idx_memories_repository_created ON memories(repository_id, created_at);
CREATE INDEX IF NOT EXISTS idx_verifications_repository_started ON verifications(repository_id, started_at);
CREATE INDEX IF NOT EXISTS idx_issues_repository_created ON issues(repository_id, created_at);
CREATE INDEX IF NOT EXISTS idx_issue_comments_issue_created ON issue_comments(issue_id, created_at);
CREATE INDEX IF NOT EXISTS idx_changes_repository_created ON changes(repository_id, created_at);
CREATE INDEX IF NOT EXISTS idx_change_approvals_change_decision ON change_approvals(change_id, decision);
CREATE INDEX IF NOT EXISTS idx_workspaces_repository_state ON workspaces(repository_id, state);
CREATE INDEX IF NOT EXISTS idx_code_symbols_repository_file_symbol ON code_symbols(repository_id, file, symbol);
CREATE INDEX IF NOT EXISTS idx_code_relations_repository_to ON code_relations(repository_id, to_symbol);
CREATE INDEX IF NOT EXISTS idx_activities_occurred ON activities(occurred_at);
`

const migration005 = `
ALTER TABLE sources ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sources ADD COLUMN parser_version TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE sources ADD COLUMN redaction_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE sources ADD COLUMN import_status TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE source_events ADD COLUMN native_event_id TEXT NOT NULL DEFAULT '';
ALTER TABLE source_events ADD COLUMN parent_event_id TEXT NOT NULL DEFAULT '';
ALTER TABLE source_events ADD COLUMN occurrence_index INTEGER NOT NULL DEFAULT 0;
ALTER TABLE source_events ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE source_events ADD COLUMN captured_at TEXT NOT NULL DEFAULT '';
ALTER TABLE source_events ADD COLUMN parser_version TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE source_events ADD COLUMN redaction_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN fork_type TEXT NOT NULL DEFAULT 'root';
ALTER TABLE sessions ADD COLUMN event_cursor INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN lineage_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE checkpoints ADD COLUMN bundle_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE checkpoints ADD COLUMN context_manifest_id TEXT NOT NULL DEFAULT '';
ALTER TABLE checkpoints ADD COLUMN event_cursor INTEGER NOT NULL DEFAULT 0;
ALTER TABLE checkpoints ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS context_manifests (
  id TEXT PRIMARY KEY,
  repository_id TEXT NOT NULL REFERENCES repositories(id),
  session_id TEXT NOT NULL DEFAULT '',
  checkpoint_id TEXT NOT NULL DEFAULT '',
  revision TEXT NOT NULL DEFAULT '',
  epoch INTEGER NOT NULL DEFAULT 0,
  schema_version INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'ready',
  content_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS context_items (
  id TEXT PRIMARY KEY,
  manifest_id TEXT NOT NULL REFERENCES context_manifests(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  kind TEXT NOT NULL,
  ref_id TEXT NOT NULL DEFAULT '',
  source_id TEXT NOT NULL DEFAULT '',
  repository_id TEXT NOT NULL DEFAULT '',
  revision TEXT NOT NULL DEFAULT '',
  basis TEXT NOT NULL DEFAULT 'unknown',
  title TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  token_cost INTEGER NOT NULL DEFAULT 0,
  included INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS handoffs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  repository_id TEXT NOT NULL REFERENCES repositories(id),
  revision TEXT NOT NULL DEFAULT '',
  schema_version INTEGER NOT NULL DEFAULT 1,
  format TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS evidence_bundles (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  subject_kind TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  repository_id TEXT NOT NULL DEFAULT '',
  revision TEXT NOT NULL DEFAULT '',
  schema_version INTEGER NOT NULL DEFAULT 1,
  payload TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS idempotency_keys (
  key TEXT PRIMARY KEY,
  entity_kind TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_context_manifests_repo_created ON context_manifests(repository_id, created_at);
CREATE INDEX IF NOT EXISTS idx_context_items_manifest_position ON context_items(manifest_id, position);
CREATE INDEX IF NOT EXISTS idx_handoffs_session_created ON handoffs(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_evidence_bundles_subject ON evidence_bundles(subject_kind, subject_id);
`
