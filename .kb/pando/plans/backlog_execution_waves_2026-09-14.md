---
created_at: 2026-09-14T15:40:06.039847585Z
updated_at: 2026-09-14T18:14:06.65852345Z
tags:
    - plan
    - backlog
    - coordination
    - pmngr
    - complete
---

# PANDO backlog execution plan (gintrack/pmngr `.kb/.pmngr`)

Backlog source: `.kb/.pmngr/` (project key `PANDO`) — 2 milestones, 7 epics, 29 stories, created
2026-09-13 from the git-in-track gap analysis. Every story carried file:line references, an
implementation sketch, explicit "do NOT" constraints and acceptance criteria, which is what made
them directly delegable to subagents with no further design work.

Note: the gintrack MCP server has only the `GIT` and `ALCH` projects cloned, so `PANDO` items are
read and updated directly as Markdown in `.kb/.pmngr/`.

## Outcome, 2026-09-14

All 29 stories delivered. **PANDO-M-0002 done.** **PANDO-M-0001 in review**, held open only by
US-0008 and US-0010, both for want of a round trip against a live `agui-serve`.

| Epic | Status | Stories |
|---|---|---|
| EP-0001 TypeScript SDK | in_review | US-0006, 0007, 0009 done; 0008, 0010 in review |
| EP-0002 tool allow-list and profiles | done | US-0011..0014 |
| EP-0003 thread lifecycle and run durability | done | US-0015..0019 |
| EP-0004 operability | done | US-0020..0024 |
| EP-0005 KB metadata and REST search | done | US-0001..0005 |
| EP-0006 MCP HTTP auth | done | US-0025, US-0026 |
| EP-0007 search scale | done | US-0027..0029 |

Follow-ups filed: [[PANDO-T-0001]] SDK CI, [[PANDO-T-0002]] HITL round trip, PANDO-T-0003
(config-singleton test leak, done the same day).

## What the waves were, and why they held

Waves were cut so two concurrently running agents never edited the same file. That constraint, not
story priority, set the order. The tracks were `internal/rag/kb`, `internal/agui`, `sdk/typescript`
(a separate git repository), `internal/api` and the MCP transport. Where a track's stories rewrote
the same functions — the KB watcher pair, the AG-UI profile trio, the run-durability trio — one
agent did them sequentially rather than two in parallel.

One commit per story or per inseparable group, made by the coordinator, never by the agent: a
subagent committing would snapshot a tree that other agents were mid-edit in.

## What this shape produced that a single-agent pass would not have

Several findings came out of writing the tests, not from the specs:

- Suppressing the mirror-to-watcher echo cannot consume its suppression entry on first match: one
  `os.WriteFile` on a new file fires Create and Write with the same mtime, and both must match.
- `ToolDiscovery` is visibility, not authorization — a denied tool stays executable through
  `tool_search`'s remote executor, so the allow-list filter has to run after `ApplyToolDiscovery`
  and drop `tool_search` itself.
- Filtering a path prefix in Go after fusion under-returns, because the limit is applied first.
- Widening `AguiEventType` with a `(string & {})` catch-all breaks discriminated-union narrowing
  across the whole switch, not only the fallback arm.
- HITL waits select on the adapter's base context, not the run's, so context cancellation never
  reached them; cancel and drain both need `cancelAll`.
- `run.done` was closed before the run left the store, so a waiter could see teardown finish early.

## Working rules for delegated agents

- Spec file is authoritative; the "do NOT" list is a hard constraint.
- English for all code, comments and docs.
- Tests per acceptance criterion; `go build ./...` plus the package's own `go test`, with `-race`
  for concurrency work.
- Agents never commit. Story front matter moves `backlog` -> `in_progress` at dispatch and ->
  `in_review`/`done` on landing; a story whose acceptance criteria are not all met stays in review
  with a follow-up task, rather than being called done.

Related: [[pando/analysis/copilotkit_agui_integration_analysis.md]],
[[pando/features/agui_adapter_p0_p1.md]], [[pando/features/agui-profiles-per-profile-overrides.md]],
[[pando/features/agui-run-parking-reattach-cancel.md]],
[[pando/features/agui-operability-healthz-cap-drain-token.md]],
[[pando/fixes/agent_pkg_config_load_leak.md]]