---
created_at: 2026-09-29T19:12:56.390329489Z
updated_at: 2026-09-29T19:12:56.390329489Z
tags:
    - change
    - memory
    - prompt-cache
---
# Cache-stable, query-aware memory injection (PANDO-US-0036)

Date: 2026-09-29. Story PANDO-US-0036, the only implemented story of epic PANDO-EP-0008 (Grok Build memory analysis). Decision record: [[pando/analysis/grok-build-memory-pros-cons.md]]. Background: [[pando/analysis/grok-build-memory-survey.md]].

## Problem
- `buildSystemMessage` runs every turn (`processGeneration` -> `prepareProvider` -> `createAgentProvider`) and put `time.Now()` to the second in the prompt (`agent.go` WithEnvironment, `templates/base/environment.md.tpl`, legacy `prompt/coder.go` `getEnvironmentInfo`). The system prompt changed every turn, invalidating the provider prefix cache for the system block and all history after it.
- The `<memories>` block was rebuilt every turn with `BuildMemoryBlock(ctx, "")`: no search (only pinned scopes, `project/` never injected), and hits incremented on every turn.
- `formatMemoryLine` cut content at 200 bytes (could split UTF-8 runes) with no pointer to the rest.
- `MemoryAutoCapture` was dead config shown as enabled in TUI/WebUI.

## Changes
- `internal/llm/agent/memory_block.go` (new): `withMemoryQuery`/`memoryQueryFromContext`; `sessionMemoryBlock(ctx)` builds the block once per session (query = the session's first raw user prompt) and reuses it verbatim; `invalidateSessionMemoryBlock(sessionID)`; cache bounded at 512 sessions (reset when full). Session-less requests are never frozen.
- `internal/llm/agent/agent.go`: `processGeneration` captures the raw prompt (`memoryQuery`) before Lua hooks/enrichment and puts it in `promptCtx`; `buildSystemMessage` uses `sessionMemoryBlock`; date-only `2006-01-02` in `WithEnvironment`; `generateAndPersistSummary` invalidates the frozen block after saving the summary (covers `/compact`, `Summarize` and auto-compaction), so the next turn rebuilds it with that turn's prompt.
- `internal/llm/prompt/templates/base/environment.md.tpl`, `internal/llm/prompt/coder.go`: "Current date: <YYYY-MM-DD>".
- `internal/rag/memory_enricher.go`: rune-safe truncation at `memoryLineMaxRunes` (200) followed by `[truncated; full text: kb_get_document file_path="..."]` (recall has no key lookup; every memory is a KB doc).
- `MemoryAutoCapture` removed from `internal/config/config.go`, `init.go` template, `config_test.go`, TUI settings (`internal/tui/page/settings.go`) and WebUI (`web-ui/src/components/settings/RemembrancesSettings.tsx`). Existing configs with the key still load (unknown TOML keys are ignored; verified with this repo's `.pando.toml`).

Known remaining variability (not addressed): auto-activated skill instructions and session policy rulesets change the prompt when they change; the prepareProvider fast path reuses the provider built at agent creation (its memory block is session-less).

## Verification
- New tests `internal/llm/agent/memory_block_test.go` (freeze across turns, per session, invalidation + re-query, session-less not cached, no injector, and `TestSystemPromptIsByteStableAcrossTurns` which sleeps across a second boundary) and `internal/rag/memory_enricher_test.go` (short content, rune-safe truncation + pointer).
- Mutation: restoring the seconds format makes `TestSystemPromptIsByteStableAcrossTurns` fail.
- `go test -race -count=3 ./internal/llm/agent ./internal/api ./internal/rag` and `go test ./internal/llm/prompt ./internal/config ./internal/tui/page` green; `go build ./...` OK; `npx tsc --noEmit` in `web-ui` clean.

## Measurement procedure (cache effect)
With an Anthropic (or other caching) provider, run the same 5-turn session on the previous build and on this one; compare per-turn `CacheReadTokens` vs `InputTokens` in provider usage (session cost/usage in `pando_stats` or debug logs "System prompt built"). Before: cache reads cover only the tools block after turn 1; after: cache reads should cover system + prior history on turns 2-5. Not measured live in this change.
