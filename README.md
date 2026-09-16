# Across by GrayCodeAI

**Version: 0.0.1**
**Status: Local Alpha**

**Code. Context. Continuity.**

> Across preserves the context, decisions, checkpoints, code history, and verification behind engineering work so developers and coding agents can reliably understand, review, restore, and continue it.

## What works (Local Alpha, evidence-backed)

- `across version` → `0.0.1`
- Repo lifecycle: `repo add/create/clone/refs/list/show`, `mirror create/sync/status`
- Sessions: `session start/list/show/close`; sources: `source import/inspect/delete` (Across/Claude/Cursor/Codex/Gemini/OpenCode formats, redaction, streaming-dedup, portable search index)
- Agent import: `agent import-session --file F` (private tmp staging, raw deleted, snapshot supersession via native_id; transcript path canonicalized + symlink-resolved + size-bounded)
- Checkpoints: `checkpoint create/list/show/explain/compare/restore` (restore creates NEW worktree, never resets checkout)
- Post-commit hook: 0 sessions → none; 1 active → checkpoint; 2+ → ambiguity event, no guessed attribution
- Memory: `memory create/list/show/approve/supersede` (candidate by default; state ≠ truth)
- Continuity: `search` (portable substring index; FTS5 deferred until guaranteed in default builds), `brief`, `handoff`, `dossier --change ID` (approvals+checkpoints+verification+decisions), `context diff` (changed-file staleness)
- Verification distinction: `verify add` (basis=user_recorded, STATED) vs `verify run` (basis=executed_by_across_local_runner, OBSERVED)
- Code intelligence: `code search`, `index` (Go AST; others heuristic with analysis_quality), `graph query/impact/health/snapshot/diff` (real added/removed/changed via isolated worktrees), `why`, `investigate`, `review` (markers, secrets, large/migration/config changes, missing-test heuristic, protected paths; analysis≠verification)
- Collaboration: `issue`, `change`, `branch-rule` (min-approvals + required-verifications, pre-receive on hosted repos), `queue add/merge` (approval/verification gates, fails if base moved)
- Control plane: `control` (org/project/principal/grant), `token` (hash-only, shown once), `serve` (loopback, bearer token, Host/Origin checks, Git smart HTTP via http-backend, JSON API + web console)
- MCP: `across mcp` stdio, 17 read-only tools backed by real store queries (mutation names refused)
- Plugins: `plugin install/list/run/remove` (SHA-256, NOT sandboxed, bounded)
- Backup: `backup create/verify/restore` (plaintext; traversal/checksum guarded)
- System: `doctor`, `clean --dry-run` (Across-owned tmp only), `activity`, `recap`, `agent-help`, `agent list/info`
- Adapters: `bin/across-agent-*` × 9, protocol version 1 JSON stdin/stdout

## Experimental

- Merge queue auto-push to hosted remote (merge commit prepared in isolated tmp clone; push manual)
- Native transcript parsers beyond Across/Claude/Codex/Gemini/OpenCode shapes
- Session export via provider CLIs (import path enforces bounds + deletes raw)

## Unverified

- Browser UI (`web/` placeholder — BLOCKED_BY_ENVIRONMENT if localhost unreachable)
- Windows qualification; all-provider LIVE qualification (see docs/agent-compatibility.md)

## Known limitations

- Local runner is NOT a sandbox (user OS permissions). Not exposed via default MCP.
- Backups are plaintext unless protected externally.
- Secret redaction is best-effort, not perfect.
- HTTP authorization is not an OS/filesystem security boundary.
- Checkpoint restore restores Git-tracked state only.
- See docs/SECURITY.md.

## Quickstart

```bash
make build
./bin/across version
export ACROSS_HOME=~/.local/share/across
./bin/across repo add /path/to/repo
./bin/across session start --repo <ID> --agent opencode
# commit → post-commit hook creates checkpoint (install: git hook calling `across hook post-commit`)
./bin/across checkpoint list --repo <ID>
./bin/across brief "continue auth migration" --repo <ID>
```

## Agent compatibility

See docs/agent-compatibility.md. Synthetic fixture success ≠ live qualification.

## Security model

See docs/SECURITY.md.
