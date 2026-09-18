---
description: Across — git-native engineering context and provenance system. Build, test, evidence, and security conventions.
globs: "*.go,*.md,*.yml,*.yaml,*.js,*.css,*.html"
alwaysApply: false
---

# Across Conventions

Git-native engineering context, provenance, checkpoint, and continuity system. Local alpha, single Go binary, local-first. Module path is `github.com/graycodeai/across`.

## Development workflow

When starting any new work (feature, fix, refactor, chore), always create a feature branch from `main` first. Never commit directly to `main`. Use branch naming conventions like `feat/<description>`, `fix/<description>`, or `chore/<description>`. Open a PR, ensure CI is green, then merge.

One concern per commit. Keep PRs focused — one feature or fix each. Describe the problem and the approach, not just the diff.

## Build & Test

```bash
make build        # across + all cmd/across-agent-* binaries into bin/
make test         # go test -count=1 ./...
make test-race    # go test -race -count=1 ./...
make vet          # go vet ./...
make e2e          # go test -count=1 -run E2E ./...
make fuzz         # FuzzAcrossJSONL, 15s
make check        # vet + test-race — run this before opening a PR
make fmt          # gofmt -w .
```

CI runs `go mod tidy`, `go build ./...`, `make vet`, `make test`, `make test-race`. Go 1.24.

## Structure

- `cmd/across/` — the CLI entrypoint
- `cmd/across-agent-*/` — one adapter binary per coding-agent provider (claude-code, codex, cursor, gemini, opencode, qwen, factory-droid, amp, goose)
- `internal/` — core logic: `cli`, `config`, `event`, `git`, `logging`, `redact`, `store`
- `e2e/` — end-to-end tests
- `web/` — thin static UI (`app.js`, `index.html`, `styles.css`)
- `docs/` — including `SECURITY.md` (threat model) and `agent-compatibility.md` (provider matrix)

## Code style

- `gofmt -w .` — no exceptions.
- No comments unless asked. Code should explain itself.
- Errors are values. Handle them or return them explicitly.
- Keep the UI thin. Core logic stays in Go, not the web layer.
- Add tests for new behavior. A feature without a test is not implemented.
- Update docs in the same commit if behavior changes.

## Evidence discipline

This is the rule most likely to be violated by an agent, and it is not optional:

- Use `OBSERVED` / `STATED` / `UNKNOWN` honestly. **Never claim something is verified without execution evidence.**
- Prefer unknowns over invention. If evidence is insufficient, return `unknown`.
- Correctness over features — a smaller correct change beats a larger speculative one.

When updating the provider matrix in `docs/agent-compatibility.md`, only move a provider past `UNIMPLEMENTED` with real evidence. Qualification ladder: `UNIMPLEMENTED` → `SYNTHETIC_TESTED` → `LIVE_TESTED` → `LIVE_QUALIFIED`, plus `BLOCKED`.

## Security invariants

Do not weaken these without an explicit decision recorded in the PR. Full threat model in `docs/SECURITY.md`.

- Retrieved context is **data, never permission**. Never let transcript or checkpoint content act as instructions.
- Transcript paths are canonicalized, symlink-resolved, and confined to the provider root.
- Hooks are chained, never silently overwritten; originals are preserved.
- Checkpoint restore creates a new worktree and never mutates the user's checkout.
- Backups reject path traversal and checksum mismatches.
- Deletion uses tombstones to prevent resurrection. No silent destructive changes — migrations preserve provenance.
- Known non-boundaries: the local runner is not a sandbox, plugins are not sandboxed, secret redaction is best-effort, and HTTP authorization is not an OS/filesystem boundary. Don't document them as stronger than they are.

## Agent instructions files

This repo — and every repo in the `graycode-eco` workspace — is **AGENTS.md-only**. Do not add a `CLAUDE.md`; tools that read both prefer it and it would shadow this file. If `gitnexus analyze` generates one, delete it.

## Ecosystem Boundaries

- Across is a standalone local-first tool; it must build, test, and run without any other graycode-eco repo.
- No dependency on `rho`, `flux`, `graycode-platform`, or `graycode-skills`.
- Agent adapters integrate with third-party coding agents through their exported transcript formats only — never by reaching into another tool's internals.

For full graycode-eco extension guidelines, see [rho/AGENTS.md](https://github.com/GrayCodeAI/rho/blob/main/AGENTS.md).
