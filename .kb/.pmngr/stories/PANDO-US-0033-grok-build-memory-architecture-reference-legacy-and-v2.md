---
id: PANDO-US-0033
type: story
title: Grok Build memory architecture reference (legacy and v2)
status: backlog
priority: medium
parent: PANDO-EP-0008
labels: [analysis, memory, grok-build]
estimate: 5
created: 2026-09-18T08:35:07Z
updated: 2026-09-18T08:35:07Z
---

## Description

As a Pando maintainer, I want a canonical reference of Grok Build's memory architecture (legacy and v2), so that every later comparison story argues from the same verified facts.

Document the data layout, scopes and workspace identity, both pipelines, the state-DB tables, the lifecycle (capture → inbox → Dream → topics → archive → GC), the config surface and the rollout stages.

Questions to answer:

- Why did xAI move from legacy to v2? Which legacy weaknesses does v2 address, as evidenced by comments and tests?
- How do the Dream leases and fencing guarantee crash safety?
- What is model-visible and what is host-only?

Files to study — Grok: `xai-grok-memory/src/{lib,v2,v2_capture,v2_consolidation,v2_maintenance,v2_access,v2_carryover,storage,dream,dream_lock,flush}.rs`, `xai-grok-config-types/src/memory.rs`, `xai-grok-pager/docs/user-guide/13-memory.md`.

## Acceptance Criteria

- [ ] `pando/analysis/grok-build-memory-architecture.md` exists in the KB with lifecycle diagrams (mermaid), a table of limits and budgets, and a glossary.
- [ ] The preliminary research report is stored alongside it as `pando/analysis/grok-build-memory-survey.md`.
- [ ] Every statement cites `file:line` in grok-build; no Pando code is changed.

## Notes

Foundation for the other stories of PANDO-EP-0008.
