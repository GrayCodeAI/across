# Across Architecture

- **CLI** `bin/across` → `internal/` (Go)
- **Storage** SQLite WAL (`ACROSS_HOME` default `~/.local/share/across`)
- **Git** post-commit hook → auto-checkpoint, commit/session linkage
- **Concepts** Repo → Session → Checkpoint → Brief/Handoff/Dossier → Verify

Boundary: Rover owns task execution/orchestration/verification; Across owns engineering history/sessions/checkpoints/provenance (README 62-73).
