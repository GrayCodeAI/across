package store

import (
	"database/sql"
	"fmt"
)

// migrations are numbered and transactional where possible.
var migrations = []struct {
	Version int
	SQL     string
}{
	{1, migration001},
	{2, migration002},
	{3, migration003},
}

func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var maxV int
	_ = db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&maxV)
	for _, m := range migrations {
		if m.Version <= maxV {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", m.Version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, datetime('now'))`, m.Version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	// integrity friendly defaults
	return nil
}

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
