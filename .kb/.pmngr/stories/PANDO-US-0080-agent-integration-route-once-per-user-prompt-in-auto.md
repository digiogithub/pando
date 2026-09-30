---
id: PANDO-US-0080
type: story
title: "Agent integration: route once per user prompt in Auto sessions, apply the routed model as a session override, capability and context-window filtering"
status: done
priority: high
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, agent]
estimate: 8
created: 2026-09-30T19:34:53Z
updated: 2026-10-01T07:57:13Z
started: 2026-09-30T21:00:23Z
closed: 2026-10-01T07:57:13Z
---

## Description

As a user in an Auto session, I want every prompt I send to run on the model configured for that kind of task, so that cheap or fast models handle simple work and strong models handle hard work, with no manual switching.

**Hook point:** `processGeneration` (`internal/llm/agent/agent.go:1214`), after persona auto-select (:1286) and before `prepareProvider` (:1334). It runs only at the start of a **user** turn. It never runs on tool-iteration loops, resurrection or continuation runs, `/compact`, or summarization.

**Session Auto state:**
- Add a per-session flag `SessionLLMOverrides.AutoMode` (`session_overrides.go`), set from the selector (see the selector story).
- A session with no explicit choice uses `modelAutoMode.enabled && defaultAuto`.
- For ACP, the flag is persisted in `acp_session_state` so it survives resume.

**Per turn:**
1. Call the router engine to get a `RoutingDecision`.
2. Filter the candidates by capability: if the prompt carries attachments or images, drop candidates without `SupportsAttachments`.
3. Filter by context: if the current history exceeds a candidate's context window, skip that candidate. If it was the last candidate, fall back to auto-compact with the coder.
4. Set the session model override to the first surviving candidate for this turn only, through the same path as `SetSessionModelOverride`. Keep the remaining candidates in turn-scoped state for failover.
5. If the model differs from the previous turn's model, run the existing switch hygiene: `sanitizeHistoryForModelSwitch` (strip reasoning blocks) and refit the context.
6. The Auto flag itself is untouched, so the next prompt routes again.

## Acceptance Criteria

- [ ] Routing adds at most `timeoutMs` to time-to-first-token. A slow or down router never blocks the prompt: on timeout the turn runs on the coder model.
- [ ] Concurrent sessions route independently; the override is request/session scoped and never touches global `agents.coder.model`.
- [ ] Subagents (mesnada/delegation), title, summarizer, persona selector and the evaluator judge are not affected by auto mode.
- [ ] `messages.model` for each assistant message equals the model that actually answered.
- [ ] Router errors warn once per session through the system message; after that they go to Debug logs only.
- [ ] Tests:
  - agent-level tests with a fake router and fake providers cover: route hit, no-match to coder, router down to coder, attachment filtering, context-window skip, and two consecutive prompts routed to different models with reasoning stripped
  - `go test ./internal/llm/agent ./internal/api` passes

## Notes

- Mid-session model changes break the provider prompt cache. This is accepted, and the docs will say so.
- A future `lowConfidencePolicy: previous` (keep last turn's route on low confidence) can land here behind a flag, default off. The requested behaviour is: no match goes to the coder.
