---
id: PANDO-US-0076
type: story
title: "ContextTrimmer: give the per-session tool-filtering LLM call its own flag (default off) or remove it"
status: done
priority: low
parent: PANDO-EP-0014
author: mcp
labels: [evaluator, config, cost]
estimate: 2
created: 2026-09-29T20:33:19Z
updated: 2026-09-29T21:06:45Z
started: 2026-09-29T21:04:41Z
closed: 2026-09-29T21:06:45Z
---

## Description

`ContextTrimmer` (`internal/evaluator/context_trimmer.go`, LLM request at line 124) profiles the first user message of every new session with the judge model to filter the tool list (`internal/llm/agent/agent.go:1350-1353`). It is wired unconditionally whenever `evaluator.enabled` and a judge model exist (`internal/app/app.go:530-533`). It is not part of the evaluation loop, adds a hidden LLM call and latency to every new session, can hide tools the user expects, and has no setting of its own.

Decide and implement one of:

- Remove it (recommended if no measurable benefit): delete `context_trimmer.go`, `agentContextTrimmerAdapter`, `SetContextTrimmer`, `ContextProfile*`, and their tests.
- Keep it behind `evaluator.contextTrimmer.enabled` (default `false`), with its cost recorded in the savings ledger, a confidence threshold in config, and an entry in `pando evaluator doctor`.

## Acceptance Criteria

- [ ] With default configuration and the evaluator enabled, starting a new session performs no LLM call before the first agent request (assert with a fake provider counting calls).
- [ ] If kept: enabling the flag restores the behaviour and the call is visible in the savings ledger.
- [ ] `go test -race ./internal/evaluator ./internal/llm/agent ./internal/app` pass; KB change document written.
