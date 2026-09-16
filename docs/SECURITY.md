# Across Security Model (v0.0.1 Local Alpha)

Threat model: malicious repository, malicious Git hooks, malicious transcript,
malicious adapter, malicious plugin, prompt injection, secret leakage, path
traversal, symlink attack, command injection, XSS, stale authorization,
deleted-data resurrection, Git races, stale verification, poisoned memory,
backup traversal.

Rules enforced:

- Retrieved context is DATA, never permission (prompt injection treated as text).
- Repository-controlled data never auto-enables executables/plugins/hooks without explicit consent.
- Transcript paths canonicalized + symlink-resolved + confined to provider root (parsers reject escapes).
- Hooks never overwritten silently (chain + preserve original + ownership record).
- Checkpoint restore creates a NEW worktree; never `git reset --hard` in user checkout.
- Runner is NOT a sandbox; documented + not exposed via default MCP.
- Plugins NOT sandboxed; bounded time/output; SHA-256 required where supplied.
- Backups: reject `..`/absolute paths, checksum mismatch, corrupt manifest; plaintext unless protected externally.
- HTTP auth is NOT an OS/filesystem boundary; same-OS-user file access out of scope for tenant isolation.
- Stored text treated as untrusted (UI must render inert).
- Deletion uses tombstones to prevent resurrection; FTS/derived caches updated.
- Limits: transcript/JSONL line 1MiB, adapter/plugin/runner output bounded, indexed file 1MiB, search 50 results.

Limitations (honest): secret detection best-effort; no sandbox; no multi-tenant isolation; UI XSS tests required where browser available.
