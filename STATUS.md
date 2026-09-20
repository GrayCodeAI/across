# Across implementation status — 0.0.1

Local-first, git-native engineering continuity system. Not production-ready.

| Area | Implemented | Remaining / not validated |
|---|---|---|
| Core | SQLite WAL, repos/sessions/checkpoints, post-commit hook, CLI `repo/session/checkpoint/brief/handoff/dossier` | Linux/Windows qualification, backup/restore hardening |
| Provenance | Commit/session linkage, verification basis `executed_by_across_local_runner` | Semantic graph import, external evidence attestation |
| Agents | OpenCode synthetic-tested adapter | 8/9 providers UNIMPLEMENTED — no LIVE_TESTED |
| Interop | Checkpoints belong to Across; Rover may request/consume/restore (see README) | `rho checkpoint` <-> `across checkpoint` mapping, `rover snapshots` naming map |

See `README.md` Boundary: Across vs Rover and `docs/agent-compatibility.md`.
