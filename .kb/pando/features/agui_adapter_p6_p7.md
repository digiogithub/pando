---
created_at: 2026-07-28T22:29:28.005086037Z
updated_at: 2026-07-28T22:29:28.005086037Z
tags:
    - feature
    - agui
    - copilotkit
    - sdk
    - typescript
    - mesnada
    - implementation
---
# Feature: AG-UI adapter — P6 (SDK + example) and P7 (sub-agents) implemented

**Date:** 2026-07-29
**Status:** P6 DONE. P7 done for the sub-agent half; the "embedded CopilotKit runtime" half
declined on purpose (see below).
**Plan:** [[pando/analysis/copilotkit_agui_integration_analysis.md]]
**Builds on:** [[pando/features/agui_adapter_p5_hardening.md]], [[pando/features/agui_adapter_p0_p1.md]]

## P6 — `@pando-ai/sdk/agui` subpath + Next.js example

No Go code was touched, which was the point of the phase: the protocol is the contract.

### New files under `sdk/typescript/src/agui/`

| File | Contents |
|---|---|
| `types.ts` | AG-UI event catalogue, `RunAgentInput`, message/tool/context types, the `PandoState` shared-state document (incl. `subAgents`) and the `/info` payload. Mirrors the Go structs one-to-one. |
| `client.ts` | `PandoAguiClient` — dependency-free: `run()` (async generator of events), `runText()`, `info()`, `agentUrl()`. Own SSE parser (`parseSSE`), bearer auth, `PandoAguiError` carrying the HTTP status. |
| `copilotkit.ts` | `createPandoAgent`, `discoverPandoAgents`, `registerPandoCopilotKit` (P7 one-liner route). |
| `index.ts` | Subpath entry point. |

`package.json`: new `./agui` export condition, both entry points in the `tsup` build,
`@ag-ui/client` and `@copilotkit/runtime` declared as **optional** peers
(`peerDependenciesMeta`). They are never statically imported.

**Design decisions worth keeping:**

- **Zero hard dependencies.** A consumer that only wants to stream events must not install
  CopilotKit's client stack, so the AG-UI types are declared locally rather than imported
  from `@ag-ui/client`.
- **Peers are injectable.** `createPandoAgent({ HttpAgent })` /
  `registerPandoCopilotKit({ runtimeModule })` accept the modules directly. The dynamic
  `import(variableSpecifier)` fallback exists for plain Node, but a bundler cannot analyse
  it — hence the example passes them explicitly. A missing peer raises an actionable
  `PandoError` naming the package instead of `MODULE_NOT_FOUND`.
- **`discoverPandoAgents` keeps the configured origin** and takes only the *path* from
  `/info`. Behind a proxy that rewrites Host, the absolute URL the server reports can be
  unreachable from the Node process.
- **SSE reassembly from the byte stream**: frames are split on `\n\n` across chunk
  boundaries; a non-JSON frame is skipped rather than ending the run; a final frame that
  arrived without its trailing blank line is still yielded.

### `examples/copilotkit/` (Next.js 15 App Router)

- `app/api/copilotkit/route.ts` — `registerPandoCopilotKit` with the peers passed in,
  `maxDuration = 300`, `dynamic = 'force-dynamic'`.
- `app/page.tsx` — `useCoAgent<PandoState>` dashboard (model, token budget, todos, files,
  **sub-agents**), a frontend tool (`useCopilotAction` + handler) and the HITL approval card
  bound to `pando_permission_request` via `renderAndWaitForResponse`.
- `README.md` — how to start `pando agui-serve`, why the origin allow-list and token are not
  optional, why the Next.js hop exists (the token must not reach the browser), and a
  troubleshooting table (401 / 403 / no agents / stalled run).

## P7 — mesnada sub-agents in the shared state

New `internal/agui/subagents.go` + `StateDoc.SubAgents []SubAgentState` (`/subAgents` JSON
pointer) fed from `stateTracker.observeToolResultLocked`.

The list is **derived from the mesnada_* tool traffic the adapter already observes** — the
orchestrator is never consulted and no `agent.AgentEvent` type was added (invariants I1/I4
hold). A delegated task appears because the agent called a tool that mentions it, and
advances when the agent looks it up again: the board is as fresh as the agent's own
knowledge, which is what the page should show.

Rules that matter:

- `mesnada_spawn_agent` / relaunch: upsert; the prompt is recovered from the **call input**
  (the result does not echo it) and truncated to 240 bytes on a rune boundary.
- `mesnada_swarm`: publishes the whole topology at once (`worker_ids`, `verifier_id`,
  `synthesizer_id`) with `status: pending` and a `role`, so a fan-out renders before any
  worker reports.
- `mesnada_get_task` / `mesnada_wait_task`: upsert from the full task, including
  `conclusion.status` + summary.
- `mesnada_list_tasks`: **refreshes known ids only.** A listing queries the whole store,
  including other projects' tasks; adopting them would turn the board into a global task
  table.
- `mesnada_await`: marks the awaited ids (`status: awaited`) from the call arguments.
- Updates **merge**: a cancel result reports a status and nothing else and must not blank
  the prompt a spawn recorded.
- Bounded at `maxSubAgents = 50` — the whole document is re-sent on every run.
- A prose (non-JSON) tool answer produces no delta and no entry.

### Declined on purpose: the embedded CopilotKit runtime

The other half of P7 was "skip the Node hop" by serving CopilotKit's runtime protocol from
Go. Not implemented, and documented as such in `doc.go`: that protocol is GraphQL and
faster-moving than AG-UI, CopilotKit's own runtime already translates it, and removing the
hop would move the API token into the browser — the one place it must not be.

## Verification

- `go build ./...`, `go vet ./internal/agui ./internal/api`, `go test -race ./internal/agui
  ./internal/api` — all clean. `gofmt -l` reports only pre-existing offenders elsewhere.
- New `internal/agui/subagents_test.go`: spawn→progress in place, list-does-not-adopt,
  swarm topology + roles, await marking, file tools unaffected, board bounded, prose
  ignored, `subAgents` serialized as `[]` not `null` (an absent array is an invalid target
  for the first `add` patch).
- New `sdk/typescript/tests/agui.test.ts` (14 tests): request shape + bearer header, SSE
  reassembly across a chunk boundary, `runText`, `RUN_ERROR`, 401→`PandoAguiError`, no
  token→no header, discovery, malformed/blank-line-less frames, and the CopilotKit helpers
  with injected fakes. Full SDK suite: 90 tests pass.
- `npx tsc --noEmit` clean under `strict` + `exactOptionalPropertyTypes` +
  `noUncheckedIndexedAccess`.
- **Live end-to-end**, against `pando agui-serve --port 8098 --no-tls --allow-origin
  http://localhost:3000` driven by the new TS client:
  - `/info` with the token → the discovery payload (coder / copilot.gpt-5-mini / all four
    capabilities); without it → `PandoAguiError` 401.
  - A real agent turn streamed
    `RUN_STARTED, STATE_SNAPSHOT, TEXT_MESSAGE_START, TEXT_MESSAGE_CONTENT×2, STATE_DELTA,
    TEXT_MESSAGE_END, RUN_FINISHED` and returned `PONG`. This also closes the gap left by
    P5, which had only been verified at the `/info` level.

Not exercised live: a delegated run populating `subAgents` (would spawn real sub-agent
processes; covered by unit tests), and the CopilotKit example in a browser (needs
`npm install` of the CopilotKit stack).