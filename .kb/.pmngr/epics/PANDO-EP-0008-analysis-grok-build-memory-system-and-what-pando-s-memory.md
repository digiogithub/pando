---
id: PANDO-EP-0008
type: epic
title: "Analysis: Grok Build memory system and what Pando's memory can learn from it"
status: backlog
priority: medium
labels: [analysis, memory, grok-build, research]
created: 2026-09-18T08:34:30Z
updated: 2026-09-18T08:34:30Z
---

## Description

Analysis-only epic: understand how Grok Build (xAI, Rust, `/www/MCP/Pando/grok-build`, code index `grok-build`) implements cross-session memory, and decide whether and how Pando's memory can improve from it. **No code, config or schema changes in this epic.**

Grok Build ships two memory pipelines in crate `xai-grok-memory`, wired from `xai-grok-shell` (`xai-grok-memory/src/lib.rs:1-25`), experimental and off by default:

- **Legacy** (`~/.grok/memory/`): `MEMORY.md` per scope plus session logs, SQLite FTS + optional vectors; hybrid ranking 0.3 keyword / 0.7 vector with 30-day decay on session logs, access boost, MMR diversity and staleness notes (`search.rs:1-14, 316-321`); first-turn and post-compaction snippet injection (≤ 6 hits, score ≥ 0.9, `turn.rs:2266-2350`); pre-compaction LLM flush with 0.92 cosine dedup (`flush.rs`); LLM-free session-end summary (`hooks.rs`); "Dream" consolidation gated on 24 h + 5 sessions (`dream.rs`).
- **v2** (`~/.grok/memory-v2/{global,<slug>-<hash8>}`): curated `topics/*.md`, immutable observation inbox, `archive/`, generated read-only `MEMORY.md` index (`v2.rs`). Per-turn automatic capture with no tools, strict JSON schema, typed observations (user / feedback / project / reference) with provenance, a noop outcome and prompt-injection guard (`memory_capture.rs:304-343, 491`). v2 Dream on 20 pending or 24 h: typed operations (create / update / delete / rename / merge / split) each citing evidence, contradiction pruning, < 10 topics (`v2_consolidation.rs:85`, `v2_memory_dream.rs:1090`). Bounded index (8 KiB, 64 entries) injected once and reused verbatim to keep provider prompt caches valid; model reads topics with normal file tools under a read-before-write path policy (`memory_context.rs:13-104`, `v2_access.rs`). Hash-preconditioned tombstone forgetting with reason (`v2_maintenance.rs`). Workspace identity = hash of git origin, so clones and worktrees share memory (`storage.rs:713-760`). Staged rollout Off / RecordOnly / Shadow / Active plus kill switches (`xai-grok-config-types/src/memory.rs`). User surfaces: `/memory` browser, `/remember` with review, `/flush`, `/dream`, `grok memory clear`, ACP `x.ai/memory/*`.

Pando's memory is a key/value layer on the KB: `memory_key` / `memory_scope` / `hits` / `importance` / `expires_at` / `outdated` columns (`internal/db/migrations/20260611000001_add_kb_memory.sql`, `internal/rag/kb/memory.go`), hourly outdated-marking GC (`memory_gc.go`), `remember` / `recall` / `forget` (`internal/llm/tools/remembrances_memory.go`), `<memories>` block injection (`internal/rag/memory_enricher.go`, `internal/llm/agent/agent.go:2840`), the per-message ContextEnricher, versioned `.kb/` mirror and wiki-link graph.

Defects observed during the preliminary survey (to verify and file separately, not fix here):

1. `MemoryAutoCapture` is dead config: defined in `internal/config/config.go:563`, defaulted true in `init.go:441`, shown in TUI settings, never read. Pando has no automatic capture.
2. Injection never searches: `agent.go:2841` calls `BuildMemoryBlock(ctx, "")`, so the search step in `memory.go:436` is skipped and only pinned-scope (`user/`) memories are injected.
3. The system prompt is rebuilt every turn (`agent.go:1203 → 2554 → 2706`) with a hit-ordered memory block at the front; possible prompt-cache invalidation — to be measured.
4. `forget` hard-deletes with no tombstone, reason or content-hash precondition.

Preliminary research report: session scratchpad `memory_report.md` (to be stored in the KB by story 1).

## Acceptance Criteria

- [ ] Every child story delivers a KB document under `pando/analysis/grok-build-memory-*.md` (via `kb_add_document`) with `[[wiki links]]` to `pando/plans/memory_system_implementation_plan.md`, `pando/reference/memory_tools_analysis.md`, `analysis/memory-tool-analysis.md`, `pando/features/learning_mode.md` and each other.
- [ ] Every claim about either codebase cites `file:line`.
- [ ] A final recommendation ranks candidate improvements by value, cost and risk (adopt / adapt / reject) and lists the follow-up implementation epics to create — without implementing them.
- [ ] The verified Pando defects are filed as separate backlog items, not fixed in this epic.
- [ ] No source, config or migration file is modified by this epic.

## Notes

Reference repository indexed as code project `grok-build`; use `code_hybrid_search` / `code_find_symbol` against it. The user's external DeepSeek comparison of Grok Build features motivated the epic. Total estimate of child stories: 29 points.
