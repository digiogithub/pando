---
created_at: 2026-09-14T16:32:29.281860869Z
updated_at: 2026-09-14T16:32:29.281860869Z
tags:
    - feature
    - sdk
    - typescript
    - agui
    - ci
---

# PANDO-US-0009 — Complete the AG-UI event and input typings, with a CI drift check

Status: implemented and verified (2026-09-14). Story:
`.kb/.pmngr/stories/PANDO-US-0009-complete-the-ag-ui-event-and-input-typings-with-a-ci-drift.md`.
Epic: [[PANDO-EP-0001]]. Branch: `feat/pando-us-0006-browser-safe` in the separate
`sdk/typescript` git repository (remote `madeindigio/pando-typescript-sdk`). Builds on
[[PANDO-US-0007-pandothread-stateful-transcript]] and [[PANDO-US-0008-typed-hitl-helpers]].

## What changed

All in `sdk/typescript/src/agui/types.ts` unless noted.

- **All 25 event constants in `internal/agui/events.go` now have a dedicated TypeScript
  interface**: added `StepStartedEvent`, `StepFinishedEvent`, `TextMessageChunkEvent`,
  `MessagesSnapshotEvent`, `ReasoningStartEvent`, `ReasoningMessageStartEvent`,
  `ReasoningMessageEndEvent`, `ReasoningEndEvent`, `ActivitySnapshotEvent`,
  `ActivityDeltaEvent`, `RawEvent`. `TextMessageChunkEvent` and `ActivityDeltaEvent` have no
  Go constructor/call site today (declared constants only) — their field shapes are inferred
  (documented inline) rather than copied from a Go struct, since none exists.
  `KnownAguiEvent` now lists all of them; `OtherAguiEvent`'s `type` is `Exclude<AguiEventType,
  KnownAguiEvent["type"]>`, which now resolves to `never` (every known name is covered). The
  interface itself stays exported/documented as the escape hatch for a future Go adapter
  version's new event names — the drift check (below) is what catches that gap before release.
  **Important finding**: `AguiEventType` must stay a *closed* string-literal union. Adding a
  `(string & {})` catch-all member (the same pattern already used elsewhere for
  `PandoFileState.action`) breaks TypeScript's discriminated-union narrowing across the
  *entire* switch in `thread.ts` — not just the fallback branch — verified with an isolated
  `tsc` repro. Forward-compat with an unrecognized event name is a runtime property instead
  (`parseSSE` casts JSON to `AguiEvent` without a structural check; every reducer switch ends
  in `default:`).
- **`CUSTOM` names are now a discriminated union.** `CustomEvent` (still that name) is a union
  of `CustomEventFrontendToolsDisabled`, `CustomEventTodos`, `CustomEventTokenUsage`,
  `CustomEventSummarize`, `CustomEventSystemNotice` (the six `pando.<agentEventType>` spellings
  from `translate.go:233-242`: `system_message`, `steering_queued`, `steering_injected`,
  `conclusion_queued`, `conclusion_injected`, `resurrected`), and an open
  `CustomEventOther` fallback. New `PandoCustomEventName` type documents the known names.
  `thread.ts`'s own `PandoCustomEvent = Extract<AguiEvent, {type:"CUSTOM"}>` picks up the full
  union automatically — no change needed there.
- **`AguiRunOptions` can set `forwardedProps` and `parentRunId`.** Renamed `RunOptions` (in
  `agui/client.ts`) to `AguiRunOptions` — it collided in name (different shape) with
  `RunOptions` exported from the main `@pando-ai/sdk` entry (subprocess mode). Kept
  `export type RunOptions = AguiRunOptions` as a `@deprecated` alias for one minor version.
  `buildInput` now forwards both fields when set. Unit test in `tests/agui.test.ts` asserts
  they appear (and are omitted when unset) in the posted body.
- **`AguiMessage.content`** is now `string | AguiMessageContentPart[]` (new
  `AguiMessageContentPart` mirrors Go's `InputContent`: `type/text/url/data/mimeType`), and
  `activityType?: string` was added (`internal/agui/input.go:164`). Fixed a knock-on type error
  in `thread.ts`'s `TEXT_MESSAGE_CONTENT` reducer (`message.content = (message.content ?? "") +
  event.delta` no longer compiles once `content` can be an array; guarded with a
  `typeof === "string"` check — streamed assistant text is always a string).
- **`PandoTodo`** tightened to `{content: string; status: ...; priority: ...}`, matching Go
  `tools.TodoItem` (`internal/llm/tools/todo_write.go:13-17`) exactly: no `id` field (Go has
  none), no index signature. `status`/`priority` keep literal unions with an open
  `(string & {})` fallback, same convention as `PandoFileState.action`.

## Drift check

New `sdk/typescript/scripts/check-agui-drift.mjs` (plain Node ESM, no new dependency): parses
`internal/agui/events.go` (event constants) and `internal/agui/input.go`
(`RunAgentInput`/`Message` struct JSON tags) **at check time**, and diffs them against
`src/agui/types.ts` (pinned `type: "X";` literals for events; interface field names for the two
structs). Exports pure parsing/diff functions (`computeAguiDrift`, `isClean`, etc.), typed via a
hand-written `scripts/check-agui-drift.d.mts` sibling declaration (the script must stay a
runnable plain `.mjs` with no build step).

**Compares against the live Go source, not a committed snapshot** — the coordinator noted
`internal/agui` was being edited concurrently by other agents during this task, and the story
does not mandate a snapshot, so this reads `internal/agui/{events,input}.go` fresh every run.
Default path is `../../../internal/agui` relative to the script (this package's home inside the
`pando` monorepo checkout); override with `PANDO_AGUI_GO_DIR` for a CI job that checked the Go
source out elsewhere. New npm script `check:agui-drift`.

Verified the check actually catches drift: copied the real Go source to a scratch dir, appended
a fake `EventFooBarBaz EventType = "FOO_BAR_BAZ"` constant, reran with `PANDO_AGUI_GO_DIR`
pointed at the mutated copy — exit 1, correctly named `FOO_BAR_BAZ` as the missing interface.
Also codified as `tests/agui-drift.test.ts` (5 Jest tests, no filesystem mutation needed —
fixture Go/TS source strings passed directly to `computeAguiDrift`): one regression guard against
the real live Go source (currently clean), and three deliberate-mutation cases (a new event
constant, a new `RunAgentInput` field, a new `Message` field), each asserted to report as
missing.

**CI wiring**: new `sdk/typescript/.github/workflows/ci.yml` (none existed before — first CI for
this package, following up on the note in
[[PANDO-US-0006-sdk-browser-safe-agui-client]]). Two jobs: `test` (build, typecheck, test,
test:browser-build) and `agui-drift`, which checks out `sdk/typescript`'s own repo plus a sparse
checkout of `digiogithub/pando`'s `internal/agui` directory (needs a `PANDO_MONOREPO_TOKEN` PAT
secret if that repo is private — commented placeholder left in the workflow; **untested against
real GitHub Actions**, since this sandbox cannot run workflows).

## Files touched

- `src/agui/types.ts` — event interfaces, CUSTOM union, `AguiMessage`/`AguiMessageContentPart`,
  `PandoTodo`.
- `src/agui/client.ts` — `AguiRunOptions` (renamed from `RunOptions`, deprecated alias kept),
  `parentRunId`/`forwardedProps` wiring in `buildInput`.
- `src/agui/thread.ts` — import rename to `AguiRunOptions`; `TEXT_MESSAGE_CONTENT` reducer fix
  for the widened `content` type.
- `src/agui/index.ts`, `src/agui/client-entry.ts` — export both `AguiRunOptions` and the
  deprecated `RunOptions` alias.
- `scripts/check-agui-drift.mjs` (new), `scripts/check-agui-drift.d.mts` (new).
- `package.json` — `check:agui-drift` script.
- `.github/workflows/ci.yml` (new).
- `tests/agui.test.ts` — 2 new tests (forwardedProps/parentRunId present/omitted).
- `tests/agui-drift.test.ts` (new) — 5 tests.

## Verified

From `sdk/typescript`: `npm run build` (pass), `npm run typecheck` (pass, both `tsconfig.json`
and `tsconfig.agui-browser.json` — `exactOptionalPropertyTypes`/`noUncheckedIndexedAccess`
stay clean), `npm test` (125/125 pass across 10 suites), `npm run test:browser-build` (pass —
Vite + React 18 fixture still builds clean, no CopilotKit/`node:` leakage), `npm run
check:agui-drift` (pass against the live Go source).

## Known gap

The CI workflow's cross-repo checkout of `digiogithub/pando` for the drift job has not been
exercised on real GitHub Actions infrastructure (no such access from this task) — the YAML was
validated for syntax only (`python3 -c "import yaml; yaml.safe_load(...)"`). If
`digiogithub/pando` is private, a `PANDO_MONOREPO_TOKEN` secret must be added and the commented
`token:` line uncommented.