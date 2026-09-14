---
id: PANDO-US-0009
type: story
title: Complete the AG-UI event and input typings, with a CI drift check
status: done
priority: medium
parent: PANDO-EP-0001
milestone: PANDO-M-0001
author: claude
labels: [sdk, typescript, agui, ci]
estimate: 5
created: 2026-09-13T21:14:35Z
updated: 2026-09-13T21:14:35Z
---

## Description

As an SDK consumer, I want every event the server can emit and every field `RunAgentInput` accepts to have a TypeScript counterpart, so that I stop casting `unknown` and stop discovering protocol fields by reading Go.

Gaps, all in `sdk/typescript/src/agui/types.ts` unless noted:

- **Untyped events.** Only 14 of the 19 names the server can emit have a dedicated interface. `REASONING_START`, `REASONING_MESSAGE_START`, `REASONING_MESSAGE_END`, `REASONING_END`, `STEP_STARTED`, `STEP_FINISHED`, `MESSAGES_SNAPSHOT`, `ACTIVITY_SNAPSHOT`, `ACTIVITY_DELTA`, `RAW` and `TEXT_MESSAGE_CHUNK` degrade to `OtherAguiEvent` with an `unknown` index signature (`types.ts:173-176`), so `event.messageId` is `unknown`. Type all of them from `internal/agui/events.go`, including the ones with no current call site — they are part of the declared protocol.
- **CUSTOM names.** `CustomEvent.name` is `string` and `value` is `unknown` (`types.ts:145`). Add a union of the emitted `pando.*` names with payload types: `pando.frontendToolsDisabled` `{count, reason}` (`internal/agui/server.go:322`), `pando.todos` and `pando.tokenUsage` (`translate.go:219,225`), `pando.summarize` `{progress, done}` (`translate.go:228`), and `pando.<agentEventType>` for the six `agent.AgentEventType` spellings emitted at `translate.go:233-242`, keeping an open-ended fallback.
- **Input fields.** `forwardedProps` and `parentRunId` exist on the request type (`types.ts:229-238`) but `buildInput` never sets them and `RunOptions` cannot express them (`src/agui/client.ts:77-95,205-221`). Expose both.
- **Message shape.** `AguiMessage.content` is `string | undefined` (`types.ts:204-212`) while the server accepts a string or an array of multimodal parts (`internal/agui/input.go:76-102`); `activityType` (`input.go:164`) is missing entirely. Type both.
- **`PandoTodo`** is `[key: string]: unknown` — tighten it to the Go `tools.TodoItem` shape.
- **Name collision.** `RunOptions` is exported from both `./index` (subprocess mode, `src/client.ts`) and `./agui` with different shapes. Rename the AG-UI one to `AguiRunOptions`, keeping a deprecated alias for one minor version.

Then add the drift check: a script that parses `internal/agui/events.go` and `internal/agui/input.go` for event-type constants and struct fields and diffs them against the TypeScript declarations, failing CI on a mismatch. Generation from Go is acceptable; a diff-only check is the minimum.

Do NOT change the wire format, and do NOT drop unknown-event tolerance — the fallback `OtherAguiEvent` stays so a newer server does not break an older client. Note that `forwardedProps` is currently decoded and dropped server-side (`input.go:46`, its only reference); typing it on the client is still correct and is tracked separately on the Pando side.

## Acceptance Criteria

- [ ] Every event constant in `internal/agui/events.go` has a named TypeScript interface; `OtherAguiEvent` remains only as the unknown-name fallback.
- [ ] A typed union covers the `pando.*` CUSTOM names with their payloads, with an open fallback for unknown names.
- [ ] `AguiRunOptions` can set `forwardedProps` and `parentRunId`, and `buildInput` forwards them; a unit test asserts they appear in the posted body.
- [ ] `AguiMessage` types multimodal `content` parts and `activityType`.
- [ ] A CI job runs the drift check and fails on an event constant or `RunAgentInput` field with no TypeScript counterpart; a deliberate test mutation of `events.go` makes the job red.
- [ ] `tsc --noEmit` stays clean under `exactOptionalPropertyTypes` and `noUncheckedIndexedAccess`.

## Notes

Size: M. Depends on the browser-safe build story for the export surface; `PandoThread` and the HITL helpers consume these types, so landing this early reduces churn in both. Evidence: `report-sdk-agui-client.md` §2.1-§2.2 and §7.
