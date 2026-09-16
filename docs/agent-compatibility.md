# Agent Compatibility Matrix

| Provider | Format | Parser | Qualification |
|---|---|---|---|
| Claude Code | JSONL | best-effort | UNIMPLEMENTED |
| Codex | rollout JSONL | best-effort | UNIMPLEMENTED |
| Cursor | JSONL | best-effort | UNIMPLEMENTED |
| Gemini CLI | session JSON | best-effort | UNIMPLEMENTED |
| OpenCode | export | best-effort | SYNTHETIC_TESTED |
| Qwen Code | transcript | best-effort | UNIMPLEMENTED |
| Factory Droid | JSONL | best-effort | UNIMPLEMENTED |
| Amp | export | best-effort | UNIMPLEMENTED |
| Goose | export | best-effort | UNIMPLEMENTED |

## Qualification statuses

| Status | Meaning |
|---|---|
| `UNIMPLEMENTED` | Adapter exists; parser not yet live-tested |
| `SYNTHETIC_TESTED` | Passed fixture-based tests |
| `LIVE_TESTED` | Verified against real provider output |
| `LIVE_QUALIFIED` | Validated end-to-end in a real workflow |
| `BLOCKED` | Cannot be tested due to external constraints |

**Synthetic fixture success does not equal live qualification.**

## Adapter protocol

All adapters implement **protocol version 1**:

- Communication: JSON on stdin/stdout
- Capabilities: introspectable via `capabilities` argv
- Common capabilities: `capture_events`, `install_hooks`, `native_resume`, `session_export`, `token_usage`, `subagents`, `review`

## Importing sessions

```bash
# Export from provider to a file, then import (bounded, raw deleted after parsing)
across agent import-session --agent NAME --repo REPO_ID --session SID --file EXPORT.jsonl
```

The import path enforces:
- Transcript path canonicalization + symlink resolution
- 32 MiB size bound
- Parse in a private temporary directory
- Raw file deleted; only safe projection retained
- Streaming partials deduplicated (§33, §108)
