# Across

<div align="center">

**Git-native engineering context, provenance, checkpoint, and continuity system.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/GrayCodeAI/across/workflows/CI/badge.svg)](https://github.com/GrayCodeAI/across/actions)
[![Version](https://img.shields.io/badge/version-0.0.1-blue.svg)](https://github.com/GrayCodeAI/across)
[![Status](https://img.shields.io/badge/status-local--alpha-blue)]()

</div>

> Across preserves the context, decisions, checkpoints, code history, and verification behind engineering work so developers and coding agents can reliably understand, review, restore, and continue it.

---

## Table of Contents

- [Status](#status)
- [What Across Is](#what-across-is)
- [Quickstart](#quickstart)
- [Core Concepts](#core-concepts)
- [Commands](#commands)
- [Architecture](#architecture)
- [Security Model](#security-model)
- [Agent Compatibility](#agent-compatibility)
- [Development](#development)
- [Limitations](#limitations)
- [License](#license)

---

## Status

| | |
|---|---|
| **Version** | `0.0.1` source snapshot (unreleased) |
| **Status** | Local Alpha |
| **Runtime** | Local-first; one core CLI binary plus optional protocol-shell adapter binaries |
| **Database** | SQLite (WAL, foreign keys, migrations) |
| **Platforms** | macOS (qualified) · Linux (CI-tested) · Windows (compile-checked only, untested) |
| **License** | [MIT](LICENSE) |

This is **not production-ready**. See [Limitations](#limitations) for what is unverified.

---

## What Across Is

Across connects **developers**, **coding agents**, and **git repositories** into one evidence-backed local system. Its primary purpose is **continuity**: stop work today, return weeks later, and answer:

- What was I trying to do?
- What did the agent do? What failed? What succeeded?
- Why was this decision made? Which commit belongs to which session?
- Which tests actually ran? Can I restore the exact code state safely?

### What Across Is Not

Across is **not** a coding agent, IDE, task orchestration system, replacement for Git/GitHub, vector database, chat-history viewer, LLM wrapper, or note-taking app.

### Boundary: Across vs Rover

GrayCodeAI also builds **Rover**. The boundary is explicit:

| Rover owns | Across owns |
|---|---|
| task execution | engineering history |
| agent orchestration | session capture and checkpoints |
| verification execution | commit/session linkage and provenance |
| delivery | context retrieval and handoffs |

Checkpoints belong to **Across**. Rover may request, consume, and restore them — but does not own them.

---

## Quickstart

```bash
# Build
make build

# Configure
export ACROSS_HOME=~/.local/share/across

# Register a repository
./bin/across repo add /path/to/repo
# → repo_abc123

# Start a session
./bin/across session start --repo repo_abc123 --agent codex
# → sess_xyz789

# Install the post-commit hook (auto-checkpoint)
./bin/across hook install /path/to/repo

# Work, commit — a checkpoint is created automatically
# ... git commit ...

# Later: reconstruct context
./bin/across brief "continue auth migration" --repo repo_abc123

# Restore code state safely (new worktree — never resets your checkout)
./bin/across checkpoint restore cp_def456
```

---

## Core Concepts

### Epistemic Model

Across distinguishes kinds of knowledge. Never merge these:

| Kind | Meaning | Example |
|---|---|---|
| `OBSERVED` | Directly verified | Commit `abc123` exists |
| `STATED` | Agent or user claimed it | "Tests passed" |
| `APPROVED` | Human signed off | Tech lead approved design |
| `INFERRED` | Across deduced it | Service B is affected |
| `DISPUTED` | Contradicted | Two sessions disagree |
| `SUPERSEDED` | Replaced | Decision replaced by later decision |
| `UNKNOWN` | No evidence | Anything uncited |

**Critical rule:** Never convert "the agent said tests passed" into "tests passed" without execution evidence (`basis: executed_by_across_local_runner`).

### Checkpoint

A durable checkpoint record linking engineering activity to a git revision. The revision is resolved with `git rev-parse --verify` when the checkpoint is recorded; the record itself is not cryptographically sealed or enforced immutable (`agent-help` reports `immutable_enforced: false`).

```
checkpoint_id, repository_id, revision, session_id,
created_at, message, basis, agent, native_session_id
```

Restore creates a **new git worktree** — never mutates your existing checkout.

### Revision Provenance

Every revision relationship carries a basis:

| Basis | Meaning |
|---|---|
| `checkpoint_revision` | Caused by the checkpointed work |
| `capture_time_head_not_causation` | HEAD at capture time — not proven causal |
| `executed_by_across_local_runner` | Across executed and observed exit code |
| `user_recorded` | Human or agent claim, not independently verified |
| `unknown` | Provenance cannot be established |

---

## Commands

### Repositories and mirrors

```bash
across repo add PATH          # Register external repo
across repo create NAME       # Create hosted bare repo
across repo clone ID PATH     # Clone
across repo list / show ID
across mirror create --repo ID
across mirror sync / status --repo ID
```

### Sessions and evidence

```bash
across session start --repo ID --agent NAME [--native-id N]
across session fork PARENT --repo ID --agent NAME [--native-id N]
                             # New session with parent_session_id + fork_type=fork
across session list / show ID / close ID
across source list [--repo ID]
across source import --repo ID --kind KIND --file F [--format FMT] [--native-id N]
                             # Formats: across, claude, cursor, codex, gemini, opencode
                             # F must be inside the repository root (see Security Model)
across agent import-session --agent NAME --repo ID --session SID --file F
across source inspect SOURCE_ID
across source delete SOURCE_ID   # Tombstoned; the same native id or origin cannot be re-imported
```

### Checkpoints

```bash
across checkpoint create --repo ID [--session S] [--message M]
across checkpoint list / show / explain / compare A B
across checkpoint restore ID  # → new worktree
across checkpoint bundle ID [--output F]
                             # Versioned, hashed evidence bundle (checkpoint + linked verifications)
across hook install REPO_PATH   # Chains an existing post-commit hook
across hook uninstall REPO_PATH # Removes the Across hook, restores the chained original
across hook post-commit       # Git hook entrypoint
```

### Memory and continuity

```bash
across memory create --repo ID --kind KIND --title T --body B
across memory list / show / approve / supersede OLD NEW
across search QUERY
across brief QUERY [--repo ID]
across handoff --session ID [--output F] [--format markdown|json]
across dossier --change CHANGE_ID
across context pack --repo ID --query Q [--session S] [--checkpoint CP] [--budget N] [--output F]
across context show MANIFEST_ID
across context diff --base R --head H --repo ID
```

`context pack` builds a versioned, content-hashed manifest from the checkpoint boundary, approved memories and recent verifications, marking items that exceed the token budget as not included. Selection is deterministic; `--query` is required but does not yet rank or filter items. Handoffs, context manifests and checkpoint bundles are also recorded in the local store; there is no command to list the recorded copies yet.

### Verification

```bash
across verify add --repo ID --name N    # basis=user_recorded (STATED)
across verify run --repo ID --name N -- CMD...
                                       # basis=executed_by_across_local_runner (OBSERVED)
```

### Code intelligence

```bash
across code search QUERY [--repo ID]
across index --repo ID                # Go AST; others heuristic
across graph query / impact / health / snapshot / diff
across why FILELINE [--repo ID]       # git blame + checkpoint linkage
across investigate QUERY              # Evidence-backed investigation
across review --repo ID               # Deterministic checks (analysis ≠ verification)
```

### Collaboration

```bash
across issue create / list / show / comment / close
across change create / list / show / approve / request-changes
across branch-rule add --repo ID --pattern P [--min-approvals N]
across queue add / merge
```

### Control plane and serving

```bash
across control org-create / project-create / principal-create / grant
across token create / list / revoke
across serve [--addr 127.0.0.1:7681]  # Loopback, bearer token, Git HTTP, web console
across mcp                            # Read-only MCP stdio server (8 store-backed tools)
```

### System

```bash
across doctor [--json]
across clean [--dry-run]
across activity / recap
across agent-help                    # Machine-readable JSON for coding agents
across agent list / info NAME
across plugin install / list / run / remove
across backup create --output F / verify FILE
across backup restore FILE --target-home DIR [--force]
                                     # DIR must be new or empty; --force replaces an existing
                                     # Across home and keeps it as DIR.across-old-TIMESTAMP
across version
```

Errors print as `across: <code>: <message>` on stderr. Exit codes: `2` invalid_argument, `3` not_found, `4` conflict, `5` operation_failed (including a failed `verify run` command), `1` internal.

---

## Architecture

```
across/
├── cmd/
│   ├── across/                  # Main CLI
│   └── across-agent-*/           # 9 protocol-shell binaries (protocol v1, JSON stdio)
├── internal/
│   ├── cli/                     # Command implementations
│   ├── config/                  # ACROSS_HOME, --home
│   ├── store/                   # SQLite, migrations, ID generation
│   ├── git/                     # Git wrapper, safe hook installer
│   ├── event/                   # Canonical events, native parsers, dedup
│   ├── adapter/                 # Shared protocol-v1 shell for cmd/across-agent-*
│   └── redact/                  # Deterministic secret redaction
├── web/                         # Optional thin read-only console; browser qualification pending
├── docs/
│   ├── SECURITY.md
│   ├── agent-compatibility.md
│   └── research/clean-room-decisions.md
├── e2e/                         # End-to-end tests
├── .github/
│   ├── workflows/ci.yml
│   ├── ISSUE_TEMPLATE/
│   └── PULL_REQUEST_TEMPLATE.md
├── CONTRIBUTING.md
├── CHANGELOG.md
├── CODE_OF_CONDUCT.md
├── LICENSE
├── Makefile
├── README.md
├── go.mod
└── go.sum
```

### Data layout

The default data directory is `~/.local/share/across/` (override with `ACROSS_HOME` or `--home`):

```
~/.local/share/across/
├── across.db                    # Main SQLite database
├── across.db-wal                # WAL file
├── repositories/                # Hosted bare repos
├── mirrors/                     # Local git mirrors
├── workspaces/                  # Checkpoint restore worktrees
├── plugins/                     # Installed plugins
├── backups/                     # Backup archives
└── tmp/                         # Temporary data (cleaned by `across clean`)
```

---

## Security Model

See [docs/SECURITY.md](docs/SECURITY.md) for the full threat model and enforcement rules.

Key points:

- Retrieved context is **data**, never permission.
- Transcript imports (`source import`, `agent import-session`) are canonicalized, symlink-resolved, size-bounded, and must be inside the registered repository root; a path outside it is rejected with `transcript path must be within the repository root`. Copy a provider export into the working tree (for example an untracked or git-ignored directory) before importing it.
- Hooks are chained, never silently overwritten: the original is preserved as `<hook>.across-orig` and runs from the Across wrapper (stdin-reading hooks such as `pre-receive` receive the same input); ownership is marker-based and `across hook uninstall` restores the original.
- Checkpoint restore creates a new worktree; never mutates your checkout.
- Backup restore is staged and validated (member list, checksums, modes, SQLite integrity) before it is committed by rename; it refuses a non-empty directory that is not an Across home and replaces an Across home only with `--force`, keeping the previous one beside it.
- Deleted sources are tombstoned; re-importing the same native id, or the same origin without a native id, is refused.
- The local runner is **not** a sandbox (user OS permissions).
- Plugins are **not** sandboxed.
- Secret redaction is best-effort.
- `across serve` binds to loopback only and requires the bearer token for everything except `/health`; the browser console's cookie (set by opening `/?token=…`) is accepted only for same-origin requests, so pages on other localhost ports cannot use it.
- HTTP authorization is not an OS/filesystem security boundary.

---

## Agent Compatibility

See [docs/agent-compatibility.md](docs/agent-compatibility.md) for the full matrix and import workflow.

| Provider | Manual parser | Protocol shell | Provider integration |
|---|---|---|---|
| Claude Code | JSONL | protocol v1 | UNIMPLEMENTED |
| Codex | rollout JSONL | protocol v1 | UNIMPLEMENTED |
| Cursor | JSONL | protocol v1 | UNIMPLEMENTED |
| Gemini CLI | session JSON | protocol v1 | UNIMPLEMENTED |
| OpenCode | export; synthetic parser test | protocol v1 | UNIMPLEMENTED (parser evidence only) |
| Qwen Code | not implemented | protocol v1 | UNIMPLEMENTED |
| Factory Droid | not implemented | protocol v1 | UNIMPLEMENTED |
| Amp | not implemented | protocol v1 | UNIMPLEMENTED |
| Goose | not implemented | protocol v1 | UNIMPLEMENTED |

Provider integration qualification: `UNIMPLEMENTED` · `SYNTHETIC_TESTED` · `LIVE_TESTED` · `LIVE_QUALIFIED` · `BLOCKED`. Protocol-shell tests and manual parser tests do not qualify a provider integration.

---

## Development

```bash
make build          # Build the core CLI and optional protocol-shell binaries
make test           # All tests, including E2E
make test-e2e       # End-to-end tests only (alias: make e2e)
make test-race      # Race detector
make vet            # go vet
make check          # gofmt check + vet + test-race
make cross-check    # Windows compile-only check (CGO disabled)
make vulncheck      # govulncheck at the pinned version
make install-local  # Copy binaries to ~/.local/bin/
make uninstall-local
```

Requires Go 1.26.6 or newer (see `go.mod`), `git`, and a C compiler for cgo (the SQLite driver).

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). One concern per commit. Evidence over confidence. `make check` before opening a PR.

---

## Limitations

### Unverified

- Browser UI — not qualified in a browser; the served assets are the checked-in files embedded in the binary
- Linux — CI runs the test suite on Linux; not otherwise qualified
- Windows — compile-checked in CI only; not tested
- Provider LIVE qualification — requires real provider sessions

### Deferred

- FTS5 search (portable substring index ships instead)
- Backup encryption (plaintext; protect externally)
- Session export via provider CLIs (import path enforces bounds)
- Merge queue auto-push (merge commit prepared in isolated clone; push manual)

### Known

- Memory state (`approved`) means approved engineering context — not objective truth.
- `capture_time_head_not_causation` does not prove the agent caused the commit.
- Anything not cited in a brief or dossier is **UNKNOWN** — Across does not invent confidence.
- Backups are plaintext unless protected externally.
- The local runner executes with user OS permissions (not a sandbox).

---

## License

[MIT](LICENSE) © 2026 GrayCodeAI
