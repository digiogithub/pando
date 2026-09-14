---
id: PANDO-T-0004
type: task
title: Regenerate the non-HITL .sse fixtures from a real server
status: backlog
priority: low
parent: PANDO-US-0010
author: claude
labels: [sdk, agui, tests]
estimate: 3
created: 2026-09-14T00:00:00Z
updated: 2026-09-14T00:00:00Z
---

## Description

Three fixtures under `sdk/typescript/tests/fixtures/agui/` are still hand-authored to the Go wire
format rather than recorded from a running server: `interrupt-frontend-tool.sse`,
`resume-frontend-tool.sse` and `state-delta-todos-tokenusage-files.sse`. A hand-typed fixture
asserts what someone believed the server emits, not what it emits.

PANDO-T-0002 removed the reason they could not be recorded — there is now a deterministic fixture
agent behind the `agui_fixture_agent` build tag plus `PANDO_AGUI_FIXTURE_AGENT=1` — but it only
raises the two HITL prompt kinds it was scoped to. These three fixtures cover frontend tool calls
and todo/state-delta traffic, which it does not produce.

Extend the fixture agent with a frontend-tool-call scenario and a state-delta scenario, then
regenerate the three files through the existing `sdk/typescript/scripts/record-agui-fixtures.mjs`.

If a regenerated fixture differs from the hand-authored one, that difference is the finding: it
means a test has been asserting against bytes the server never sends. Record what changed here
before replacing the file.

## Acceptance Criteria

- [ ] The fixture agent can raise a frontend tool call and emit state deltas, still with no model
      call and still behind the same two gates.
- [ ] All three fixtures are regenerated through `record-agui-fixtures.mjs` from a real
      `agui-serve`, and `tests/fixtures/agui/README.md` stops describing them as hand-authored.
- [ ] Any byte difference between the hand-authored and recorded fixtures is written down here,
      with what it implies about the tests that were relying on the old bytes.

## Notes

Follow-up of PANDO-US-0010, unblocked by PANDO-T-0002. Low priority: the scenarios are covered by
tests today, this is about the provenance of what they assert against.
