# Across implementation status — source version 0.0.1 (unreleased)

Local-first, git-native engineering continuity system. Not production-ready.

| Area | Implemented | Remaining / not validated |
|---|---|---|
| Core | SQLite WAL with checksummed migrations, repos/sessions/checkpoints, chained post-commit hook, staged and validated backup restore, CLI `repo/session/checkpoint/brief/handoff/dossier/context` | Windows qualification (compile-only in CI), restore fault-injection tests, backup encryption |
| Provenance | Commit/session linkage, verification basis `executed_by_across_local_runner` | Semantic graph import, external evidence attestation |
| Agents | 9 protocol shells; OpenCode parser synthetic-tested | 0/9 provider integrations qualified — no LIVE_TESTED |
| Interop | Checkpoints belong to Across; Rover may request/consume/restore (see README) | `rho checkpoint` <-> `across checkpoint` mapping, `rover snapshots` naming map |

See `README.md` Boundary: Across vs Rover and `docs/agent-compatibility.md`.
