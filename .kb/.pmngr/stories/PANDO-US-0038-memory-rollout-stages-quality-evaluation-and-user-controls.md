---
id: PANDO-US-0038
type: story
title: Memory rollout stages, quality evaluation and user controls
status: backlog
priority: medium
parent: PANDO-EP-0008
labels: [analysis, memory, grok-build]
estimate: 3
created: 2026-09-18T08:35:09Z
updated: 2026-09-18T08:35:09Z
---

## Description

As a Pando maintainer, I want to study Grok Build's rollout, evaluation and user controls for memory, so that any future memory feature ships safely and users can inspect and correct what is remembered.

Analyze the staged rollout (Off / RecordOnly / Shadow / Active), kill switches for capture, Dream and file writes, content-free telemetry and cost counters, and the user surfaces (`/memory` browser, `/remember` with review, `/flush`, `/dream`, `grok memory clear`, ACP `x.ai/memory/*`). Map them to Pando's TUI, WebUI, ACP and telemetry.

Questions to answer:

- How would Pando evaluate extraction and consolidation quality before exposing it — evaluator / LLM-as-judge, shadow tables?
- Which user controls are worth having in TUI and WebUI?
- How should memory activity be reported in telemetry without content?

Files to study — Grok: `xai-grok-config-types/src/memory.rs`, `acp_session_impl/memory_control.rs`, `session/memory_state.rs`, `session/memory_observation.rs`, `xai-grok-pager/docs/user-guide/13-memory.md`. Pando: `internal/tui/page/settings.go` (~:2750-2790), WebUI settings sections, `internal/telemetry/`, `internal/evaluator/`.

## Acceptance Criteria

- [ ] `pando/analysis/grok-build-memory-rollout-ux.md`.
- [ ] No Pando code is changed.
