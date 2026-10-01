---
id: PANDO-US-0099
type: story
title: "Agent integration and observability: filter wired into the turn, `Context filter: kept n/m` notice in every client only when something is dropped, `ContextFiltered` event, telemetry, doctor"
status: in_review
priority: high
parent: PANDO-EP-0018
author: mcp
labels:
  - decision-model
  - context-enrichment
  - observability
  - agent
  - acp
  - webui
  - tui
estimate: 5
created: 2026-10-01T16:19:43Z
updated: 2026-10-01T16:50:55Z
started: 2026-10-01T16:50:55Z
---

## Description

As a user, I want to see when the decision model trimmed the injected context, and as an operator I want metrics and a doctor check, so that the filter is never a silent black box.

- `internal/app/app.go`: build `modelrouter.NewRelevanceFilter(decisionModelCfg, remembrancesCfg)` and call `enricher.SetRelevanceFilter` / memory injector equivalent when the toggles are on; rebuild on `decisionModel` and `remembrances` config events.
- `internal/llm/agent/agent.go:1358-1385`: after enrichment, read the `FilterResult` (expose it through the `ContextEnricher` interface with a new optional `LastFilterResult(sessionID)` or return it alongside the string) and, **only when `Dropped > 0`**, emit a status message `Context filter: kept 4/9 (…ms)` through the same path as `emitRoutingNotice`/`announceRouting` (`model_auto.go`), so it reaches WebUI SSE system messages, TUI status, ACP session notices and the AG-UI custom event. On fail-open, one warning per session and error class (`warnAutoOnce` pattern): `Context filter unavailable (<class>): context injected unfiltered`.
- Events/telemetry: `extevents.ContextFiltered{Source counts, Kept, Dropped, Threshold, LatencyMs, RouterProvider, RouterModel, Reason}`; telemetry counters `context_filter.requests`, `.kept`, `.dropped`, `.failopen{class}` next to the model-routing ones; never prompt or snippet text.
- `pando doctor`: a "Decision model" block (provider, URL, model, health, consumers on: auto mode / persona / context filter / memory filter) replacing the router lines in the model auto mode and persona checks.

## Acceptance Criteria

- [ ] Notice appears in WebUI (SSE system message rendered like `RoutingNotice`), TUI chat status, ACP (`session/update` notice) and AG-UI custom event when `Dropped > 0`; no notice when nothing dropped or filter off (tests per surface: `internal/api`, `internal/tui`, `internal/mesnada/acp`, `internal/agui`).
- [ ] Fail-open warning emitted once per session and error class; a later successful filter in the same session clears nothing but logs at debug.
- [ ] `ContextFiltered` event published with counts only; a test asserts no prompt/snippet text in the payload.
- [ ] Telemetry counters increment; opt-in telemetry sink receives them (`internal/telemetry` tests).
- [ ] `pando doctor` output snapshot updated (`cmd/doctor_model_auto_test.go`, `doctor_persona_test.go`, new `doctor_decision_model_test.go`).
- [ ] The filter never runs for the `context-enricher` agent, subagents, resumed delegations, `/compact`, title generation (same eligibility as `autoEligible`).
- [ ] `go test ./internal/llm/agent ./internal/api ./internal/mesnada/acp ./cmd` green.

## Notes

- Anchors: `agent.go:1334-1400`, `model_auto.go` (`emitRoutingNotice`, `announceRouting`, `warnAutoOnce`), `internal/app/app.go:371-460`, `cmd/doctor.go`, `web-ui/src/components/chat/RoutingNotice.tsx`.
- WebUI rendering of the new notice belongs to the WebUI story; this story guarantees the SSE message is sent.
- Spec: "Relevance filter: observability and eligibility".
