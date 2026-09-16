# Changelog

All notable changes to Across are documented in this file. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Across adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.0.1] — 2026-09-16

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

[Unreleased]: https://github.com/GrayCodeAI/across/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/GrayCodeAI/across/releases/tag/v0.0.1
