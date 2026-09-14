---
id: PANDO-T-0002
type: task
title: Round-trip the HITL helpers against a live agui-serve
status: done
priority: medium
parent: PANDO-US-0008
milestone: PANDO-M-0001
author: claude
labels: [sdk, agui, hitl, tests, ci]
estimate: 3
created: 2026-09-14T00:00:00Z
updated: 2026-09-14T00:00:00Z
---

## Description

PANDO-US-0008 shipped `src/agui/hitl.ts` in the TypeScript SDK, but two of its acceptance
criteria asked for a round trip against a real `agui-serve`: raise a genuine permission request
and a genuine `AskUserQuestion`, answer them with `approve`/`deny`/`answerQuestion`, and assert
the agent resumes. That was not done, for two reasons: provoking a real prompt needs a live LLM
call with credentials, and a deterministic harness would have to add Go code, which the story's
scope forbade.

What shipped instead: the reply encoders are faithful ports of `approvalFromMessage` and
`answerFromMessage` in `internal/agui/hitl.go`, tested against the same literal cases as
`internal/agui/hitl_test.go`, plus `PandoThread`-driven round trips asserting the exact HTTP body.
That pins the wire format but not the server's acceptance of it.

Two ways to close it, pick one: a Go-side test fixture agent that raises both prompt kinds with no
model call, driven from the SDK's test suite; or a recorded-cassette integration test replayed
against a real `agui-serve` in a nightly job that has credentials.

## Acceptance Criteria

- [ ] A permission request raised by a real `agui-serve` is approved through `approve` and the run
      resumes.
- [ ] An `AskUserQuestion` raised by a real `agui-serve` is answered through `answerQuestion`,
      including the multi-select and "Other" free-text paths, and the run resumes.
- [ ] The test runs somewhere automated, or its manual run procedure is written down here.

## Notes

Follow-up of PANDO-US-0008, which stays in review until this is resolved or explicitly waived.
Related to PANDO-T-0001: both are about where SDK integration tests are allowed to run.
