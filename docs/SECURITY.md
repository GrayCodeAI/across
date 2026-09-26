# Security Model

## Threat model

Across considers the following adversaries:

| Category | Examples |
|---|---|
| Malicious input | malicious repository, malicious git hooks, malicious transcript, malicious adapter, malicious plugin |
| Injection | prompt injection, command injection, XSS |
| Leakage | secret leakage, stale authorization, deleted-data resurrection |
| Integrity | Git races, stale verification, poisoned memory, backup traversal |

## Rules enforced

| Rule | Mechanism |
|---|---|
| Retrieved context is **data**, never permission | Prompt-injection text is stored and displayed, never executed |
| Repository-controlled data never auto-enables executables | Explicit local consent required for hooks, plugins, adapters |
| Transcript imports are confined to the repository root | `source import` and `agent import-session` canonicalize and symlink-resolve the supplied path, require a regular file within the 32 MiB bound, and reject any path outside the registered repository root (`invalid_argument`, exit 2, `transcript path must be within the repository root`). `import-session` parses a private staged copy but records and confines the caller's path. Provider export directories (for example `~/.claude/projects`) are outside every repository, so exports must be copied into the working tree first |
| Hooks are chained, never silently overwritten | An existing hook is moved to `<hook>.across-orig` and run by the Across wrapper; hooks that read stdin (`pre-receive`, `post-receive`, `pre-push`, …) run the original first with the same input and reject if it rejects. Ownership is a marker line; upgrades are staged and renamed with rollback and keep the original; `across hook uninstall` restores it. Symlinked hook files and hook directories are refused |
| Checkpoint restore never mutates the user's checkout | New git worktree created; original left untouched |
| Runner is **not** a sandbox | Documented prominently; not exposed via default MCP |
| Plugins are **not** sandboxed | Execution time and output bounded; SHA-256 verified where supplied |
| Backups are validated before they are used | Archive members must be regular, listed in the manifest, unique, and match size, SHA-256 and (when recorded) mode; traversal, absolute, drive-letter, control-character and linked names are rejected; the SQLite snapshot must pass `PRAGMA integrity_check` |
| Restore never destroys an unrelated directory | Restore is staged beside the target and committed by rename. A missing or empty target is used directly; a non-empty directory without `across.db` is refused (`conflict`, exit 4) even with `--force`; an existing Across home is replaced only with `--force` and kept as `<target>.across-old-<UTC timestamp>` |
| Across-managed directories are real directories | Symbolic links in the `ACROSS_HOME` / `--home` / `--target-home` path you choose are resolved once; the directories Across manages inside a home (`repositories/`, `mirrors/`, `workspaces/`, `plugins/`, `backups/`, `tmp/`) must not be symlinks |
| HTTP auth is not an OS boundary | Same-OS-user file access is out of scope; not tenant isolation |
| Served console requests are same-origin | `serve` binds to loopback, validates `Host` and `Origin`, and requires the bearer token except for `/health`. The `across_token` cookie (HttpOnly, SameSite=Strict) set from `/?token=` is honoured only when `Sec-Fetch-Site` is absent, `same-origin` or `none` and any `Origin` equals the served host and port, so pages on other localhost ports cannot use it |
| Stored text is untrusted | The console assets are embedded in the binary from `web/`, render records with `textContent` only, and are served with a CSP that includes `frame-ancestors 'none'` |
| Deletion is durable | `source delete` removes events and index entries in one transaction and writes a tombstone; importing the same native id again, or the same kind and origin without a native id, is refused (`conflict`, exit 4) |

## Resource limits

| Resource | Limit |
|---|---|
| Transcript / JSONL line | 1 MiB |
| Indexed file | 1 MiB |
| Search results | 50 |
| Adapter / plugin / runner output | Bounded (1 MiB stdout/stderr, 30 s timeout) |

## Honest limitations

- Secret redaction is best-effort — not a guarantee of perfect detection.
- The local runner executes with user OS permissions. It is not a sandbox.
- Plugins run unsandboxed. Only install plugins you trust.
- There is no multi-tenant isolation. Across HTTP authorization is a control-plane convenience, not a filesystem security boundary.
- Browser XSS qualification has not been performed in a real browser; the `textContent`-only rendering is covered by a unit test of the embedded assets.
- Tombstones match identity (native id, or kind + origin path), not content: the same bytes imported from a different path without a native id are accepted.
- Backups are plaintext unless protected externally.
