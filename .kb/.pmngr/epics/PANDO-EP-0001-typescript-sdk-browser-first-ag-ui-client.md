---
id: PANDO-EP-0001
type: epic
title: "TypeScript SDK: browser-first AG-UI client"
status: backlog
priority: critical
milestone: PANDO-M-0001
labels: [sdk, typescript, agui]
created: 2026-09-13T21:11:26Z
updated: 2026-09-13T21:11:26Z
---

## Description

`@pando-ai/sdk/agui` is the client a browser app is meant to use, but the package cannot be consumed from a Vite build today: `src/http.ts:20` statically imports `node:https`, `agui/index.ts` re-exports `copilotkit.ts` whose dynamic `import()` warns under Vite, and the package declares no `sideEffects` or `browser` condition. Beyond the build, the client is a transport plus type declarations: the server emits 19 event types and the SDK types 14 of them, there is no transcript reducer, no RFC 6902 applier for `STATE_DELTA`, no interrupt/resume helper and no typed answer for `pando_permission_request` or `AskUserQuestion`. Every consumer re-derives the protocol's hard half from the Go source.

This epic makes the AG-UI subpath a first-class browser dependency and gives it the stateful helpers a chat panel needs. git-in-track (GIT-US-0053) depends on this epic directly: the product owner chose to depend on the package rather than vendor it.

## Acceptance Criteria

- [ ] A Vite + React 18 app imports `@pando-ai/sdk/agui` with no Node polyfills, no build warnings and no CopilotKit code in the bundle; a CI smoke test builds such an app.
- [ ] A `PandoThread` helper owns `threadId`, the transcript and the state document: it accumulates `TEXT_*`, `REASONING_*` and `TOOL_CALL_*` into messages, applies `STATE_DELTA`, detects `RUN_FINISHED{outcome:"interrupt"}` and exposes `resume(toolCallId, result)` producing the trailing-tool-message shape `internal/agui/input.go:216-231` requires.
- [ ] Typed helpers answer `pando_permission_request` (approve/deny, deny by default) and `AskUserQuestion` (`{cancelled, answers[]}`) and are accepted by the Go side in a round-trip test.
- [ ] Every event and input type in `internal/agui/{events,input}.go` has a TypeScript counterpart, including `REASONING_*`, `STEP_*`, `MESSAGES_SNAPSHOT`, `ACTIVITY_*`, `RAW`, the `pando.*` CUSTOM names, `forwardedProps` and `parentRunId`; CI diffs the two so drift is caught.
- [ ] Tests cover interrupt then resume, permission and question round-trips, `STATE_DELTA` application, aborted streams and non-SSE error responses, from fixtures recorded against a real `agui-serve`.

## Notes

Source analysis: `scratchpad` report `report-sdk-agui-client.md` of 2026-09-13, mirrored in git-in-track `docs/research/`. `@ag-ui/client` was evaluated and rejected as the alternative: its `RunFinishedEventSchema` has no `outcome` and `@ag-ui/core` lacks `REASONING_*`/`ACTIVITY_*`, so Pando's interrupts and reasoning would vanish. React 19 is not a constraint anywhere in this epic.
