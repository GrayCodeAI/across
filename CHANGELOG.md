# Changelog

All notable changes to Across are documented in this file. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Across adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `across session fork PARENT --repo ID --agent NAME [--native-id N]` records a child session with `parent_session_id`, `fork_type=fork` and `lineage_version=2`.
- `across context pack` / `across context show` build and re-read versioned, content-hashed context manifests (checkpoint boundary, approved memories, recent verifications) within a token budget. Selection is deterministic; `--query` does not yet rank items.
- `across checkpoint bundle ID [--output F]` exports a versioned, hashed evidence bundle for a checkpoint and its linked verifications.
- `across handoff --format json` emits a versioned, content-hashed handoff envelope with evidence references; handoffs, context manifests and checkpoint bundles are recorded in the store.
- `across source import --native-id N` for snapshot supersession and tombstone identity.
- `across hook uninstall REPO_PATH` removes the Across post-commit hook and restores a chained original.
- `across backup restore --force` to replace an existing Across home (see Security).
- Typed CLI errors printed as `across: <code>: <message>` with exit codes 2 (invalid_argument), 3 (not_found), 4 (conflict), 5 (operation_failed), 1 (internal); required flags, enums and empty positional arguments are validated before the store is opened; `verify run` records a failing command and exits 5.
- Store: a checksummed migration ledger (`schema_migrations.checksum`) that refuses unknown or future versions, gaps and edited migrations; migration 4 adds indexes; migration 5 adds import provenance columns (content hash, size, parser version, redaction status), session lineage columns, checkpoint content hash and context-manifest link, and the `context_manifests`, `context_items`, `handoffs` and `evidence_bundles` tables.

### Changed

- The nine `across-agent-*` binaries are protocol-v1 shells built on a shared runtime: they implement `ping`, report every provider capability as false, and return `UNSUPPORTED_METHOD` for anything else.
- The MCP server advertises eight store-backed tools; placeholder, unknown and mutation tool names return explicit errors. `agent-help` reports the live tool list and `immutable_enforced: false` for checkpoints.
- Multi-step mutations (source import and delete, session start and fork, checkpoint create and restore bookkeeping, memory lifecycle, handoff/context/bundle records) run in one transaction, and repository, session, source and memory references are validated; checkpoint revisions are resolved with `git rev-parse --verify` before use.
- `across serve` embeds and serves the checked-in console assets, sets server timeouts and `frame-ancestors 'none'`, and requires a loopback `--addr`.
- A symlink in the `ACROSS_HOME` / `--home` path you choose is resolved instead of rejected; managed subdirectories inside the home must still be real directories.
- `backup create` writes a SQLite snapshot (`VACUUM INTO`) and the archive atomically with mode 0600, and excludes `serve.token`, `tmp/`, `backups/` and `logs/`.

### Removed

- `internal/logging` and `ACROSS_LOG=file`; errors go to stderr in the typed format above, and new homes no longer get a `logs/` directory.

### Security

- Transcript imports (`source import`, `agent import-session`) must be inside the registered repository root; the caller's path is checked and recorded as the source origin (not the private staged copy).
- Chained git hooks now run: the preserved original runs from the Across wrapper, and stdin-reading hooks such as `pre-receive` receive the same input and can reject the push. Hook ownership is marker-based, and upgrades keep the original.
- Backup restore is staged and validated (listed, unique, regular members; checksums and modes; SQLite `integrity_check`), never writes through symlinks, refuses a non-empty directory that is not an Across home, replaces an Across home only with `--force` and keeps it as `<target>.across-old-<timestamp>`, and no longer leaves `manifest.json` in the restored home.
- Deleted sources cannot be resurrected: importing the same native id, or the same kind and origin without a native id, is refused; unrelated imports are unaffected.
- The `serve` session cookie is accepted only for same-origin requests, so pages on other localhost ports cannot use it.

### Corrected

Claims in the 0.0.1 entry that the code at `1085a2a` did not meet:

- "17 tools backed by real store queries": nine of the advertised MCP tools returned a placeholder message instead of querying the store (now eight real tools).
- "9 first-party adapter binaries … capabilities introspection": the adapters advertised capture, hooks, resume and token usage and answered every method with success without doing the work (now protocol shells).
- "Transcript paths … confined to provider root": no confinement was enforced (imports are now confined to the repository root).
- "immutable sources": nothing enforced immutability of stored sources or checkpoints (`agent-help` now reports `immutable_enforced: false`).
- "structured logging": a plain-text line logger (now removed).

## [0.0.1] — 2026-09-16

_No `v0.0.1` tag or GitHub release was published; this entry describes the source at commit `1085a2a` and is kept as originally written. Claims in it that the code did not meet are listed under Unreleased → Corrected._

### Added

- **Foundation:** Go project, Cobra CLI, SQLite store (WAL, foreign keys, migrations), config (ACROSS_HOME / --home), structured logging.
- **Git & repositories:** repo add / create / clone / refs / list / show, mirror create / sync / status.
- **Evidence model:** immutable sources, canonical events (SessionStart, TurnStart, UserPrompt, AssistantMessage, ToolUse, SubagentStart, SubagentEnd, TurnEnd, Compaction, SessionEnd), redaction boundary, portable search index, snapshot supersession (§20), streaming dedup (§33).
- **Sessions & checkpoints:** session start / list / show / close; checkpoint create / list / show / explain / compare / restore; post-commit auto-checkpoint with ambiguity safety (§35); checkpoint restore via new worktree (never resets checkout).
- **Memory & continuity:** decision / fact / procedure / task / preference / outcome / note with source linkage, candidate → approved → superseded lifecycle; brief, handoff, dossier, context diff.
- **Verification:** explicit STATED vs OBSERVED distinction; `verify add` (user_recorded) vs `verify run` (executed_by_across_local_runner); local runner (NOT sandboxed, documented).
- **Code intelligence:** Go AST index (functions, methods, types, imports); heuristic structural index for other languages (flagged `analysis_quality=heuristic`); graph query / impact / health / snapshot / diff; `why` (git blame + checkpoint linkage); investigate; deterministic review (merge markers, secrets, large changes, migration changes, config changes, missing-test heuristic, protected paths).
- **Collaboration:** issues, changes, branch rules (min-approvals + required verifications, pre-receive enforcement on hosted repos), merge queue (approval / verification gates, fails if base moved).
- **Local control plane:** organizations, projects, principals, grants, tokens (hash-only, shown once, revocable).
- **MCP:** read-only stdio server, 17 tools backed by real store queries, mutation tools refused.
- **Serving:** loopback server with bearer token, Host / Origin validation, Git smart HTTP via `git http-backend`, JSON API, web console (CSP, textContent-only).
- **Plugins:** install / list / run / remove with SHA-256 verification, bounded time / output, NOT sandboxed.
- **Backup:** create / verify / restore with SHA-256 manifest, traversal rejection, plaintext (protect externally).
- **System:** doctor (with JSON), clean --dry-run, activity, recap, agent-help (machine-readable JSON), agent list / info.
- **Adapters:** 9 first-party adapter binaries (claude-code, codex, cursor, gemini, opencode, qwen, factory-droid, amp, goose), protocol version 1 (JSON stdin/stdout, capabilities introspection).
- **Native parsers:** Across JSONL, Claude/Cursor JSONL, Codex rollout JSONL, Gemini session JSON, OpenCode export — all normalized to canonical events.

### Security

- Prompt injection treated as data, never permission.
- Transcript paths canonicalized, symlink-resolved, confined to provider root.
- Hooks chained (never overwritten silently), originals preserved.
- Deletion uses tombstones; search/index caches updated.

### Known limitations

- Browser UI qualification: BLOCKED_BY_ENVIRONMENT.
- Linux / Windows: untested.
- Provider LIVE qualification: not yet performed.
- FTS5: deferred (portable substring index ships).
- Backup encryption: plaintext (documented).

[Unreleased]: https://github.com/GrayCodeAI/across/compare/1085a2aee24611360a7903cf4690df36013f1147...main
[0.0.1]: https://github.com/GrayCodeAI/across/tree/1085a2aee24611360a7903cf4690df36013f1147
