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
| Transcript paths are confined | Canonicalized, symlink-resolved, confined to provider root; escapes rejected |
| Hooks are never silently overwritten | Original chained + preserved (`.across-orig`), ownership recorded |
| Checkpoint restore never mutates the user's checkout | New git worktree created; original left untouched |
| Runner is **not** a sandbox | Documented prominently; not exposed via default MCP |
| Plugins are **not** sandboxed | Execution time and output bounded; SHA-256 verified where supplied |
| Backups are integrity-guarded | `..` and absolute paths rejected; checksum mismatch and corrupt manifest rejected |
| HTTP auth is not an OS boundary | Same-OS-user file access is out of scope; not tenant isolation |
| Stored text is untrusted | Web UI uses `textContent`, never `innerHTML`; CSP enforced |
| Deletion is durable | Tombstones prevent resurrection; search/index caches updated |

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
- Browser XSS qualification requires an environment where localhost is reachable.
