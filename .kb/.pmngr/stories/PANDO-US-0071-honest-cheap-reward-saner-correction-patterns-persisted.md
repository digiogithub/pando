---
id: PANDO-US-0071
type: story
title: "Honest, cheap reward: saner correction patterns, persisted-data signals and explicit user feedback"
status: backlog
priority: high
parent: PANDO-EP-0014
author: mcp
labels: [evaluator, reward]
estimate: 5
created: 2026-09-29T20:33:19Z
updated: 2026-09-29T20:33:19Z
---

## Description

`calculateReward` (`internal/evaluator/reward.go`) derives `S_success` solely from regex correction patterns over user messages and `S_tokens` from a baseline computed over `session_scores`, which is empty, so efficiency is always the neutral 0.5. The developer config carries `(?i)\bno[,.]?\b`, so every "no" is a correction. Signals Pando already persists for free are ignored.

Rework the reward without adding any LLM call:

- **Correction detection**: drop bare negations from defaults, match only user turns after the first, weight by position (a correction right after an assistant turn counts more), cap the penalty; make the pattern list visible and editable in settings with a "test against my last sessions" preview.
- **Persisted signals** (from `messages`, `events`, savings ledger): tool calls that returned errors, cancelled runs (`AgentEventTypeError`/cancel events), repeated identical tool calls, sessions ended right after an error, number of turns to completion, wall time. Each becomes a bounded component with a configurable weight.
- **Explicit feedback**: `/feedback good|bad [note]` slash command in TUI/WebUI/ACP and a thumbs control in WebUI, stored as an event; when present it dominates the score.
- **Persist the decomposition**: extend `session_scores` (migration) with a JSON `components` column and the pattern hits, so the UI can explain a score.
- **Baseline**: compute `S_tokens` against the session table (`sessions.prompt_tokens + completion_tokens` of the last N sessions) so it works before any score exists, and per task type.

## Acceptance Criteria

- [ ] Default `correctionsPatterns` no longer match "no", "no problem", "no, that's fine"; a fixture test with ≥10 real anonymised user turns (Spanish and English) documents expected hits.
- [ ] A session with an explicit `/feedback bad` scores below 0.3 regardless of other signals; `/feedback good` above 0.8.
- [ ] `session_scores.components` is populated and returned by `/api/v1/evaluator/sessions`.
- [ ] `S_tokens` differs from 0.5 on the first evaluated session of a DB that already has sessions.
- [ ] `go test -race ./internal/evaluator` passes; KB change document written.

## Notes

Keep α/β weights but add the new components under a single `evaluator.weights` table in config; migrate existing `alphaWeight`/`betaWeight` transparently.
