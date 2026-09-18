---
created_at: 2026-09-18T12:49:09.261349315Z
updated_at: 2026-09-18T12:49:09.261349315Z
tags:
    - fix
    - copilot
    - provider
    - tool-calls
---
# Fix: Copilot session permanently 400 after a broken `write` tool call

Date: 2026-09-18. Related: [[fix_copilot_byok_strict_null_400]], [[fix_copilot_endpoint_metadata_routing]].

## Symptom
With the last release, a `write` call from Sonnet 5 via Copilot failed; afterwards EVERY request (even after switching model) failed with a Copilot API parameter error.

## Root cause (escape)
- `copilot.go` replayed stored tool call `Input` verbatim as `arguments` (chat-completions path and Responses API path). `openai.go`/`anthropic.go` normalized, Copilot did not.
- A tool call whose args are not a JSON object (stream cut by `max_tokens` mid large `write`, lost deltas in the Anthropic "monkeypatch" stream adapter, bare string) is persisted in session history. Copilot's Claude backend must parse `arguments` into a `tool_use.input` object, so every later request 400s. Switching model does not help: the poisoned message stays in the session.
- Copilot forced `FinishReasonToolUse` whenever tool calls existed, even on `length`, so a truncated write was executed (after jsonrepair could write a truncated file).
- Anthropic monkeypatch dropped argument deltas when a continuation chunk repeated the same tool call ID.
- `anthropic.go`/`anthropic_beta.go` *skipped* unparseable tool_use blocks, leaving an orphaned `tool_result` (another 400).

## Changes
- New `internal/llm/provider/tool_args.go`: `sanitizeToolCallArguments` (repair via `tools.NormalizeJSONInput`, require JSON object, else `{}` + warn), `sanitizeToolCallArgumentsMap`, `dropTruncatedToolCalls`.
- `copilot.go`: sanitize args in `convertMessages` and `convertMessagesToResponsesInput`; drop invalid-JSON tool calls when finish reason is `length` (stream + send); monkeypatch appends args when ID repeats.
- `openai.go`, `anthropic.go`, `anthropic_beta.go`: use shared sanitizer (no more send-as-is / skip).
- Tests: `tool_args_test.go`.

## Verification
`go test ./internal/llm/provider ./internal/llm/agent ./internal/api` OK. Not reproduced live against Copilot. Existing broken sessions heal automatically on next request (history sanitized at send time).
