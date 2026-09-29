---
created_at: 2026-09-29T18:55:06.760877692Z
updated_at: 2026-09-29T18:55:06.760877692Z
tags:
    - analysis
    - memory
    - grok-build
---
# Grok Build memory ideas for Pando: advantages, disadvantages and verdict (2026-09-29)

Scope: PANDO-EP-0008 (stories US-0033..0039). Builds on [[pando/analysis/grok-build-memory-survey.md]]. Related: [[pando/plans/memory_system_implementation_plan.md]], [[pando/reference/memory_tools_analysis.md]], [[pando/features/learning_mode.md]]. Analysis only, no code changed.

## Facts re-verified in code on 2026-09-29

1. `MemoryAutoCapture` is still dead config: only `internal/config/config.go:576`, `init.go:440` (default true), TUI settings `settings.go:2698,4907`. No logic reads it.
2. Injection still never searches: `internal/llm/agent/agent.go:2868` calls `BuildMemoryBlock(ctx, "")`; `GetMemoriesForInjection` skips the search leg when query is empty (`internal/rag/kb/memory.go`), so only pinned scopes (`user/`) are injected; `project/` memories never reach the prompt automatically.
3. Pinned memories are ordered `importance DESC, hits DESC` and `BuildMemoryBlock` increments hits for every injected memory (`internal/rag/memory_enricher.go`), and `IncrementMemoryHits` extends the TTL. So `hits` counts injections, not usefulness, and a pinned memory that is injected every turn never expires.
4. `formatMemoryLine` truncates each memory to 200 chars; long memories reach the model as meaningless fragments.
5. **Bigger prompt-cache issue than memory**: `buildSystemMessage` runs on every turn (`agent.go:1231` -> `prepareProvider`) and passes `time.Now().Format("2006-01-02 15:04:05 MST")` (`agent.go:2832`) rendered by `templates/base/environment.md.tpl:10`. The system prompt therefore changes every turn regardless of memory, which invalidates the provider prefix cache for the system block and the whole history after it. Needs measurement, but it dominates any memory-block effect.
6. Scoping is wrong in practice: the DB is per project (`.pando/data/pando.db`), so `user/` memories do not cross projects, and they are mirrored to the versioned `.kb/memory/user/*.md` — both files are tracked in git in this repo. Personal memory leaks into the team repo while not being global.
7. Real usage in this repo: 5 memory files vs ~600 KB documents. The memory layer is barely used; the explicit KB (written on instruction) is what carries knowledge. Two of the `user/` memories are actually project facts (misclassified).
8. `forget` hard-deletes DB row + mirror file (`internal/llm/tools/remembrances_memory.go:277-280`); no reason, audit or tombstone.

## Per-area assessment

### Automatic capture (US-0034) — ADAPT, later
+ Fills the empty memory layer without relying on the agent choosing `remember` (fact 7).
+ Typed observations (user/feedback/project/reference) with provenance are a proven taxonomy (same one Claude Code's file memory uses).
- Extra LLM call per turn: cost and latency; Pando runs on many providers incl. local Ollama where a cheap extractor may be absent.
- Prompt-injection surface: transcripts contain fetched web pages, MCP outputs, tool results.
- Must not run before scoping is fixed (fact 6), or it will commit personal/sensitive observations to the team repo.
- Pando's multi-surface runtime (IPC primary/secondary, ACP, mesnada subagents, `-p`) needs the same crash-safety/lease machinery Grok built (~15 state tables); that is most of Grok's code.
- Cheaper variant preferred: extract at session end or before `Summarize`/auto-compact (1 call per session/compaction), no tools, strict JSON, subagents excluded, record-only first.
- Immediate action regardless: wire or remove/hide the dead `MemoryAutoCapture` toggle (it is shown as enabled and does nothing).

### Consolidation "Dream" (US-0035) — DEFER / REJECT auto-commit
+ Prevents accumulation and contradictions; KB + `[[links]]` graph is a natural topics layer.
- Nothing to consolidate today (5 memories); only meaningful after capture exists.
- In Grok memory lives outside the repo; in Pando curated docs are versioned and human-edited, so LLM rewrites would churn jj history, conflict with humans and risk losing details.
- If done: produce a reviewable proposal (shadow plan with evidence) rather than committing.

### Injection, retrieval, cache (US-0036) — ADOPT (highest value, lowest cost)
+ Fixes real defects (facts 2-5) with small, local changes.
+ Grok's lesson "freeze the prefix per session, re-inject after compaction" applies first to the timestamp (fact 5), then to the memory block.
+ Bounded index (title + one-line description) instead of 200-char truncated snippets; the model reads the full doc with `kb_get_document`/`recall` on demand (index-then-read).
- Index-then-read costs extra tool calls and depends on model discipline; weaker models may skip reading. Mitigation: keep a few top snippets + index.
- Needs a before/after measurement of cache-read tokens (`pando_stats`, provider usage).

### Scoping, storage, forgetting (US-0037) — ADOPT scoping, ADAPT forgetting
+ `user/` into a global per-user store (outside repo), `project/` stays in `.kb/`: fixes privacy leak and makes user memory actually cross-project.
+ Lightweight forget: reason + audit row; content-hash precondition cheap to add.
- Full tombstone ledger is heavy; jj history keeps deleted content anyway, so a tombstone mainly matters for the enterprise `MemorySink` and multi-instance sync.
- Migration of existing `user/` docs needed.

### Rollout and UX (US-0038) — ADAPT minimal
+ Record-only/active switch for any automatic feature; WebUI/TUI memory browser (list what is injected, delete) builds trust.
- Grok's 4 stages + 4 kill switches + shadow evaluation tables are oversized for Pando; the existing evaluator/LLM-judge can score captured observations instead.

## Recommendation on the epic itself
The survey already answers most of US-0033. Rather than 29 points of analysis, file the verified defects now (facts 1-6, 8), fix injection/cache/scoping first (small, measurable), then run capture as an experiment in record-only mode and decide on Dream only with real data.
