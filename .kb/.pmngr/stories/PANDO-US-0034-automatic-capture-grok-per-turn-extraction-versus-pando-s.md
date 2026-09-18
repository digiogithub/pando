---
id: PANDO-US-0034
type: story
title: "Automatic capture: Grok per-turn extraction versus Pando's unused MemoryAutoCapture"
status: backlog
priority: medium
parent: PANDO-EP-0008
labels: [analysis, memory, grok-build]
estimate: 5
created: 2026-09-18T08:35:08Z
updated: 2026-09-18T08:35:08Z
---

## Description

As a Pando maintainer, I want to understand Grok Build's automatic memory capture and map it onto Pando's hook points, so that we can decide whether to implement or remove the dead `MemoryAutoCapture` setting.

Analyze Grok's extraction trigger, prompt, JSON schema, observation taxonomy (user / feedback / project / reference) with provenance, the no-tools isolation, retry and terminal-failure classification, subagent exclusion, cost controls, and the legacy session-end summary and pre-compaction flush.

Questions to answer:

- Which trigger fits Pando: end of turn, session end, before `Summarize` / auto-compact, or idle?
- Which model tier, and what token cost per session?
- How must capture behave with IPC primary / secondary, ACP, mesnada subagents and non-interactive `-p`?
- How are transcript prompt-injections prevented?
- Implement or remove `MemoryAutoCapture` (`internal/config/config.go:556-564`, `init.go:436-441`)?
- Could learning mode or the evaluator consume the observations?

Files to study — Grok: `xai-grok-shell/src/session/acp_session_impl/memory_capture.rs`, `session/memory/{v2_capture,capture_transcript,hooks}.rs`, `xai-grok-memory/src/flush.rs`. Pando: `internal/config/{config.go,init.go}`, `internal/llm/agent/agent.go` (turn end, `Summarize` ~:2100-2200), `internal/learning/`, `internal/evaluator/`, `internal/ipc/`.

## Acceptance Criteria

- [ ] `pando/analysis/grok-build-memory-capture.md` with a trigger decision matrix, a per-session cost estimate and a risk list.
- [ ] The dead `MemoryAutoCapture` flag is verified and filed as a separate backlog item.
- [ ] No Pando code is changed.
