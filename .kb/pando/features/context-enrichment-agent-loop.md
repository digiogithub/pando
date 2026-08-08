---
created_at: 2026-08-08T15:00:46.67124347Z
updated_at: 2026-08-08T15:20:17.606746354Z
tags:
    - feature
    - remembrances
    - context-enrichment
    - agent-loop
---
# Feature: Context Enrichment as a separate agent loop (2026-08-08)

## What changed

Context enrichment (Remembrances section) can now run as a **dedicated agent loop** on the
`context-enricher` model instead of the single-shot search pipeline. The loop calls the
memory, knowledge-base, events and code-index tools iteratively and returns one
`<enriched_context>` block appended to the user prompt exactly like before — the main agent
only ever sees that block, never the loop's tool calls. The loop model is independent of the
model selected by the user for the main agent (`[Agents.context-enricher]`).

Second iteration (same day) added:

- **Warm start** — the enrichment agent + provider are built in a background goroutine at
  app boot (`loopEnricher.Warmup()`), so the first prompt does not pay provider construction.
  A failed build is retried lazily on next use (mutex-guarded `ensureAgent`, not `sync.Once`).
- **Session-start only** — by default the loop runs only for the first message of a session
  (`len(msgs) == 0` at the agent call site). `ContextEnrichmentAgentLoopEveryMessage = true`
  restores per-turn enrichment.
- **Chat notices** — start/end system messages, same mechanism as context compaction
  (`AgentEventTypeSystemMessage` + `addRunStatusMessage`, via the new `agent.emitStatus`
  helper): `🧠 Context enrichment agent gathering project context...` then
  `✓ Context enrichment done — N chars of context added.` (or "no additional context found").

The run is recorded as a **child session of the active chat session** (visible/inspectable in
the UI) unless hidden by config; hidden runs use a throwaway session deleted after the run.
The loop's cost is added to the parent chat session cost.

## Config (`[Remembrances]`)

- `ContextEnrichmentAgentLoopEnabled` (bool, default false) — enable the loop.
- `ContextEnrichmentAgentLoopTimeoutSeconds` (int, default 60) — bounds one loop run.
- `ContextEnrichmentAgentLoopMaxChars` (int, default 6000) — caps the emitted block.
- `ContextEnrichmentAgentLoopEveryMessage` (bool, default false) — run every turn instead of
  only at session start.
- `ContextEnrichmentAgentLoopSilent` (inverted, notices ON by default).
- `ContextEnrichmentAgentLoopFallbackDisabled` (inverted, fallback ON) — fall back to the
  classic search enrichment when the loop fails/times out/returns nothing.
- `ContextEnrichmentAgentLoopHiddenInChat` (inverted, child session visible by default).

Configurable from TOML, TUI settings (Remembrances → Context Enrichment) and WebUI
(`RemembrancesSettings.tsx`). API needs no change: `PUT /api/v1/config/services` marshals the
whole `RemembrancesConfig`.

## Files / symbols touched

- `internal/config/config.go` — 7 new `RemembrancesConfig` fields; `internal/config/init.go` —
  TOML template defaults.
- `internal/llm/prompt/context_enricher.go` — new `ContextEnricherAgentPrompt` (loop system
  prompt; old `ContextEnricherPrompt` remains the JSON query planner).
- `internal/llm/prompt/prompt.go` — `GetAgentPrompt` case for `AgentContextEnricher`.
- `internal/llm/prompt/templates/agents/context-enricher.md.tpl` — canonical template used by
  `BuildPrompt`.
- `internal/llm/prompt/builder.go` — skips `base/workflow` + `base/conventions` for the
  retrieval-only `context-enricher` agent (imports `internal/config`).
- `internal/llm/agent/tools.go` — `ContextEnricherAgentTools(remembrances, lspProvider)`:
  read-only set (kb_search/kb_get/kb_related, search_events, hybrid_search_remembrances,
  code_* search tools, recall, glob/grep/ls/view). No mutating tool.
- `internal/llm/agent/agent.go` — `SessionContextEnricher` interface
  (`EnrichContextForSession`, `SessionStartOnly`, `Announce`); new `emitStatus` helper; call
  site gates on session start, announces, and **skips enrichment when
  `a.agentName == config.AgentContextEnricher`** (recursion guard).
- `internal/app/context_enricher_agent.go` — `agentLoopEnricher` (`Warmup`, `ensureAgent`,
  timeout, session creation/cleanup, parent cost charge, `normalizeEnrichedBlock`).
- `internal/app/app.go` — wiring + `go loopEnricher.Warmup()`; log field `mode`.
- `internal/tui/page/settings.go` — 7 fields + setters with validation.
- `web-ui/src/types/index.ts`, `web-ui/src/components/settings/RemembrancesSettings.tsx` — UI.

## Why

The fixed single-shot retrieval (heuristic or LLM query planner) cannot drill down from a
first hit to the actual symbols/files involved. An agent loop on a cheap dedicated model
produces far more relevant context for the same prompt slot; running it once per session with
warm start keeps the added latency to the first message only.

## Verification

- `go build ./...` clean; `npx tsc --noEmit` clean.
- `go test ./internal/app ./internal/llm/agent ./internal/api ./internal/config ./internal/llm/prompt` pass.
- `internal/app/context_enricher_agent_test.go` covers `normalizeEnrichedBlock` (tag
  extraction, untagged wrap, `NO_RELEVANT_CONTEXT`, truncation).

Related: [[plan_leanctx_context_intelligence]], [[memory_system_implementation_plan]],
[[context-enrichment-toggle]].
