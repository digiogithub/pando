---
id: PANDO-US-0036
type: story
title: Cache-stable, query-aware memory injection in the system prompt
status: done
priority: medium
parent: PANDO-EP-0008
labels: [memory, prompt-cache, grok-build]
estimate: 5
created: 2026-09-18T08:35:09Z
updated: 2026-09-29T19:13:09Z
started: 2026-09-29T19:09:18Z
closed: 2026-09-29T19:13:09Z
---

## Description

As a Pando user, I want the system prompt to stay byte-stable across the turns of a session and the injected memories to be relevant to what I asked, so that provider prompt caches are reused and memory tokens are not wasted.

Re-scoped on 2026-09-29 from analysis to implementation: it is the only story of PANDO-EP-0008 the maintainer decided to implement (the rest were cancelled; see `pando/analysis/grok-build-memory-pros-cons.md`). Grok Build's lesson adopted here: freeze the injected block per session and re-inject after compaction (`xai-grok-shell/src/session/helpers/memory_context.rs:13-21`, `session/compaction.rs:1664`).

Verified defects (2026-09-29):

1. The system prompt is rebuilt every turn (`agent.go:1231` -> `prepareProvider` -> `buildSystemMessage`) with `time.Now()` to the second (`agent.go:2832`, `templates/base/environment.md.tpl:10`; legacy `prompt/coder.go:212`), so the prefix changes every turn and the provider cache for system + history is invalidated.
2. Injection never searches: `agent.go:2868` calls `BuildMemoryBlock(ctx, "")`, so only pinned scopes are injected and `project/` memories never are.
3. The memory block is recomputed every turn and hits are incremented on every injection.
4. `formatMemoryLine` truncates to 200 **bytes**, which can split a UTF-8 rune, and gives the model no way to reach the rest.
5. `MemoryAutoCapture` is dead config shown as enabled in settings (automatic capture was rejected in PANDO-US-0034).

## Acceptance Criteria

- [ ] The system prompt carries the date only (no time), in both the template path and the legacy path.
- [ ] The memory block is built once per session, using the session's first user prompt as search query (pinned scopes still included), and reused verbatim on later turns.
- [ ] The frozen block is dropped after a summary/compaction so the next turn rebuilds it.
- [ ] Truncated memories are cut on a rune boundary and point the model at `recall` (by key) or `kb_get_document` (by path) for the full text.
- [ ] `MemoryAutoCapture` is no longer offered as a working setting.
- [ ] Unit tests cover freezing, invalidation, query propagation and truncation; `go test ./internal/llm/agent ./internal/rag/... ./internal/llm/prompt ./internal/config` green.
- [ ] Measurement procedure documented (cache-read tokens before/after via `pando_stats` / provider usage).

## Notes

Remaining per-turn variability not addressed here: auto-activated skill instructions and session policies change the prompt only when they change.
