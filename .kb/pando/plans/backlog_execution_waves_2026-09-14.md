---
created_at: 2026-09-14T15:40:05.746579656Z
updated_at: 2026-09-14T15:40:05.746579656Z
tags:
    - plan
    - backlog
    - coordination
    - pmngr
---

# PANDO backlog execution plan (gintrack/pmngr `.kb/.pmngr`)

Backlog source: `.kb/.pmngr/` (project key `PANDO`) — 2 milestones, 7 epics, 29 stories, all
created 2026-09-13 from the git-in-track gap analysis. Every story carries file:line references,
an implementation sketch, explicit "do NOT" constraints and acceptance criteria, so each one is
directly delegable to a subagent.

Note: the gintrack MCP server has only the `GIT` and `ALCH` projects cloned, so `PANDO` items are
read and updated directly as Markdown in `.kb/.pmngr/`.

## Milestones

- **PANDO-M-0001** Embeddable AG-UI backend for web hosts — EP-0001 (TS SDK), EP-0002 (tool
  allow-list + profiles), EP-0003 (thread lifecycle / run durability), EP-0004 (operability).
- **PANDO-M-0002** Search fidelity and transport hardening — EP-0005 (KB metadata + REST search),
  EP-0006 (MCP HTTP auth), EP-0007 (search scale/correctness).

## Wave ordering rationale

Waves are cut so that concurrently running agents never edit the same file.

- **Wave 1** (in flight): US-0002 `internal/rag/kb`, US-0011 `internal/agui` + `AGUIConfig`,
  US-0006 `sdk/typescript`. All three are `critical` and are the prerequisites of their epics.
- **Wave 2**: US-0003 + US-0004 (KB watcher, must land after US-0002), US-0012/US-0013/US-0014
  (AGUI profiles, absorb the US-0011 config key), US-0007/US-0008 (SDK PandoThread + HITL).
- **Wave 3**: US-0001 + US-0005 (REST KB/code search routes), US-0025 + US-0026 (MCP bearer token
  and CORS allow-list — both touch config, kept away from the AGUI config work).
- **Wave 4**: US-0015..US-0019 (thread API, snapshot, disconnect parking, reattach, cancel).
- **Wave 5**: US-0020..US-0024 (healthz, MaxConcurrentRuns, drain, token provisioning, docs +
  Vite/React example), US-0009/US-0010 (typings drift check, agui tests),
  US-0027/US-0028/US-0029 (search SQL body selection, path-prefix pushdown, embedding model
  staleness).

## Working rules for delegated agents

- Spec file is authoritative; the "do NOT" list is a hard constraint.
- English for all code, comments and docs.
- Tests per acceptance criterion; `go build ./...` plus the package's own `go test`.
- Agents never commit: the coordinator makes one `jj` commit per completed story.
- Story front matter moves `backlog` -> `in_progress` at dispatch and -> `in_review`/`done` on
  landing.

Related: [[pando/analysis/copilotkit_agui_integration_analysis.md]],
[[pando/features/agui_adapter_p0_p1.md]], [[analysis_pando_enterprise_extension_system]]