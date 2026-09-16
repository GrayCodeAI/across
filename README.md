# Across

<div align="center">

**Git-native engineering context, provenance, checkpoint, and continuity system.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/GrayCodeAI/across/workflows/CI/badge.svg)](https://github.com/GrayCodeAI/across/actions)
[![Version](https://img.shields.io/badge/version-0.0.1--alpha-orange)]()
[![Status](https://img.shields.io/badge/status-local%20-alpha-red)]()

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
| **Version** | `0.0.1` |
| **Status** | Local Alpha |
| **Runtime** | Local-first, single binary |
| **Database** | SQLite (WAL, foreign keys, migrations) |
| **Platforms** | macOS (qualified) · Linux, Windows (untested) |
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

A durable, immutable record linking engineering activity to a git revision:

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
across session list / show ID / close ID
across source list [--repo ID]
across source import --repo ID --kind KIND --file F
                             # Formats: across, claude, cursor, codex, gemini, opencode
across source inspect SOURCE_ID
across source delete SOURCE_ID   # Tombstoned, not resurrected
```

### Checkpoints

```bash
across checkpoint create --repo ID [--session S] [--message M]
across checkpoint list / show / explain / compare A B
across checkpoint restore ID  # → new worktree
across hook install REPO_PATH
across hook post-commit       # Git hook entrypoint
```

### Memory and continuity

```bash
across memory create --repo ID --kind KIND --title T --body B
across memory list / show / approve / supersede OLD NEW
across search QUERY
across brief QUERY [--repo ID]
across handoff --session ID [--output F]
across dossier --change CHANGE_ID
across context diff --base R --head H --repo ID
```

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
across mcp                            # Read-only MCP stdio server (17 tools)
```

### System

```bash
across doctor [--json]
across clean [--dry-run]
across activity / recap
across agent-help                    # Machine-readable JSON for coding agents
across agent list / info NAME
across plugin install / list / run / remove
across backup create / verify / restore
across version
```

---

## Architecture

```
across/
├── cmd/
│   ├── across/                  # Main CLI
│   └── across-agent-*/           # 9 adapter binaries (protocol v1, JSON stdio)
├── internal/
│   ├── cli/                     # Command implementations
│   ├── config/                  # ACROSS_HOME, --home
│   ├── store/                   # SQLite, migrations, ID generation
│   ├── git/                     # Git wrapper, safe hook installer
│   ├── event/                   # Canonical events, native parsers, dedup
│   ├── redact/                  # Deterministic secret redaction
│   └── logging/                 # Structured stderr logging
├── web/                         # Thin read-only console (CSP, textContent-only)
├── docs/
│   ├── SECURITY.md
│   ├── agent-compatibility.md
│   └── research/clean-room-decisions.md
├── e2e/                         # End-to-end tests
├── scripts/                     # Build and utility scripts
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
├── tmp/                         # Temporary data (cleaned by `across clean`)
└── logs/                        # Structured logs (when ACROSS_LOG=file)
```

---

## Security Model

See [docs/SECURITY.md](docs/SECURITY.md) for the full threat model and enforcement rules.

Key points:

- Retrieved context is **data**, never permission.
- Transcript paths are canonicalized, symlink-resolved, and confined to provider root.
- Hooks are chained (never overwritten silently); originals preserved.
- Checkpoint restore creates a new worktree; never mutates your checkout.
- Backups reject path traversal and checksum mismatches.
- Deletion uses tombstones to prevent resurrection.
- The local runner is **not** a sandbox (user OS permissions).
- Plugins are **not** sandboxed.
- Secret redaction is best-effort.
- HTTP authorization is not an OS/filesystem security boundary.

---

## Agent Compatibility

See [docs/agent-compatibility.md](docs/agent-compatibility.md) for the full matrix and import workflow.

| Provider | Format | Qualification |
|---|---|---|
| Claude Code | JSONL | UNIMPLEMENTED |
| Codex | rollout JSONL | UNIMPLEMENTED |
| Cursor | JSONL | UNIMPLEMENTED |
| Gemini CLI | session JSON | UNIMPLEMENTED |
| OpenCode | export | SYNTHETIC_TESTED |
| Qwen Code | transcript | UNIMPLEMENTED |
| Factory Droid | JSONL | UNIMPLEMENTED |
| Amp | export | UNIMPLEMENTED |
| Goose | export | UNIMPLEMENTED |

Qualification: `UNIMPLEMENTED` · `SYNTHETIC_TESTED` · `LIVE_TESTED` · `LIVE_QUALIFIED` · `BLOCKED`

---

## Development

```bash
make build          # Build all binaries to bin/
make test           # Unit tests
make test-race      # Race detector
make e2e            # End-to-end tests
make vet            # go vet
make check          # vet + test-race
make install-local  # Copy binaries to ~/.local/bin/
make uninstall-local
```

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). One concern per commit. Evidence over confidence. `make check` before opening a PR.

---

## Limitations

### Unverified

- Browser UI — environment-dependent (BLOCKED if localhost unreachable)
- Linux, Windows — untested on this platform
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
