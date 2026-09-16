# Agent Compatibility Matrix (v0.0.1)

| Provider | Hook Capture | Native Transcript | Token Usage | Subagents | Session Export | Native Resume | Real Version Tested | Qualification |
|---|---|---|---|---|---|---|---|---|
| Claude Code | adapter | JSONL parser (best-effort) | yes | metadata | via export cmd | planned | none | UNIMPLEMENTED |
| Codex | adapter | rollout JSONL (best-effort) | yes | metadata | via export cmd | planned | none | UNIMPLEMENTED |
| Cursor | adapter | JSONL (best-effort) | yes | no | no | planned | none | UNIMPLEMENTED |
| Gemini CLI | adapter | session JSON (best-effort) | yes | no | no | planned | none | UNIMPLEMENTED |
| OpenCode | adapter | export (best-effort) | yes | metadata | yes | planned | none | SYNTHETIC_TESTED |
| Qwen Code | adapter | transcript where stable | yes | no | no | planned | none | UNIMPLEMENTED |
| Factory Droid | adapter | JSONL (best-effort) | yes | no | no | planned | none | UNIMPLEMENTED |
| Amp | adapter | export (best-effort) | yes | no | no | planned | none | UNIMPLEMENTED |
| Goose | adapter | export (best-effort) | yes | no | no | planned | none | UNIMPLEMENTED |

Qualification statuses: UNIMPLEMENTED, SYNTHETIC_TESTED, LIVE_TESTED, LIVE_QUALIFIED, BLOCKED.
Synthetic fixture success does NOT equal live qualification.
