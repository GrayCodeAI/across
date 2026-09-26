# Agent Compatibility Matrix

This matrix separates manual transcript parsers, protocol shells, and provider integrations. They are different evidence surfaces and are not interchangeable.

| Provider | Manual parser | Protocol shell | Provider integration |
|---|---|---|---|
| Claude Code | JSONL | protocol v1 | UNIMPLEMENTED |
| Codex | rollout JSONL | protocol v1 | UNIMPLEMENTED |
| Cursor | JSONL | protocol v1 | UNIMPLEMENTED |
| Gemini CLI | session JSON | protocol v1 | UNIMPLEMENTED |
| OpenCode | export; synthetic parser test | protocol v1 | UNIMPLEMENTED (parser evidence only) |
| Qwen Code | not implemented | protocol v1 | UNIMPLEMENTED |
| Factory Droid | not implemented | protocol v1 | UNIMPLEMENTED |
| Amp | not implemented | protocol v1 | UNIMPLEMENTED |
| Goose | not implemented | protocol v1 | UNIMPLEMENTED |

## Qualification statuses

| Status | Meaning |
|---|---|
| `UNIMPLEMENTED` | No provider integration or qualification; a protocol shell or parser prototype may exist |
| `SYNTHETIC_TESTED` | Fixture or parser evidence only; not a live provider integration |
| `LIVE_TESTED` | Verified against recorded real provider output |
| `LIVE_QUALIFIED` | Validated end-to-end in a real user workflow |
| `BLOCKED` | Cannot be tested due to external constraints |

Synthetic parser success does not equal provider integration or live qualification.

## Protocol shells

All nine binaries expose a protocol-v1 JSON shell over stdin/stdout and capabilities metadata. The only implemented request method is `ping`; unsupported methods return a typed `UNSUPPORTED_METHOD` response. `capture_events`, `install_hooks`, `native_resume`, `session_export`, `token_usage`, `subagents`, and `review` are not implemented or qualified.

## Importing sessions

```bash
across agent import-session --agent NAME --repo REPO_ID --session SID --file EXPORT.jsonl
```

Accepted `--agent` values are `across`, `claude-code`, `claude`, `cursor`, `codex`, `gemini`, and `opencode`. Qwen Code, Factory Droid, Amp, Goose, and unknown values are rejected explicitly.

The export file must be inside the registered repository root (`across repo show REPO_ID` prints it). Provider export locations such as `~/.claude/projects` or `~/.codex/sessions` are outside every repository, so copy the export into the working tree first — for example into an untracked or git-ignored directory so it is never committed. A path outside the root fails with exit code 2 and `transcript path must be within the repository root`. `across source import --file` applies the same rule.

The import path:

- canonicalizes and resolves the supplied transcript path and checks it against the repository root;
- requires a regular file within the 32 MiB import bound;
- parses a private temporary staged copy and deletes it after parsing;
- records the supplied path (not the staged copy) as the source origin, and preserves the original export;
- uses `--session` as the native identity, so a later import for the same session supersedes the earlier snapshot, and a deleted (tombstoned) session snapshot cannot be re-imported under the same identity;
- deduplicates streaming partials before projection.
