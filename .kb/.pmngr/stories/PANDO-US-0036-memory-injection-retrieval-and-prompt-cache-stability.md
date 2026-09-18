---
id: PANDO-US-0036
type: story
title: Memory injection, retrieval and prompt-cache stability
status: backlog
priority: medium
parent: PANDO-EP-0008
labels: [analysis, memory, grok-build]
estimate: 5
created: 2026-09-18T08:35:09Z
updated: 2026-09-18T08:35:09Z
---

## Description

As a Pando maintainer, I want to compare memory injection and retrieval in both systems, including prompt-cache stability, so that we know whether Pando's `<memories>` block helps or hurts per token.

Compare Grok's frozen bounded index (index-then-read, 8 KiB / 64 entries, reused verbatim across turns) and its legacy snippet injection (hybrid ranking, decay, MMR, staleness notes, re-injection after compaction) with Pando's `<memories>` block and the ContextEnricher.

Questions to answer:

- Confirm and quantify the empty-query injection (`internal/llm/agent/agent.go:2841` calls `BuildMemoryBlock(ctx, "")`; the search in `internal/rag/kb/memory.go:436` is skipped).
- Does rebuilding the system prompt every turn with a prepended, hit-ordered memory block invalidate provider prompt caches? Measure with the LLM cache stats / `pando_stats`.
- Index versus snippets: which gives better recall per token for Pando?
- Should memory be re-injected after compaction?
- How should memory injection and the ContextEnricher divide the work?

Files to study — Grok: `xai-grok-shell/src/session/helpers/memory_context.rs`, `acp_session_impl/{turn.rs:2130-2350,prompt_build.rs:334-368}`, `session/compaction.rs:1664`, `xai-grok-agent/templates/prompt.md:13-37`, `xai-grok-memory/src/{search,mmr,query_expansion}.rs`. Pando: `internal/rag/memory_enricher.go`, `internal/rag/kb/memory.go:426-560`, `internal/llm/agent/agent.go:1188,1203,2554-2850`, `internal/rag/enricher.go`, `internal/app/context_enricher_agent.go`.

## Acceptance Criteria

- [ ] `pando/analysis/grok-build-memory-injection.md` including a small measurement (tokens and cache-hit ratio) or a reproducible measurement procedure.
- [ ] The empty-query injection defect is verified and filed as a separate backlog item.
- [ ] No Pando code is changed.
