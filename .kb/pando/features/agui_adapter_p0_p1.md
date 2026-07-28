---
created_at: 2026-07-28T14:13:06.717620935Z
updated_at: 2026-07-28T14:13:06.717620935Z
tags:
    - feature
    - agui
    - copilotkit
    - api
    - implementation
---
# Feature: AG-UI protocol adapter — P0 + P1 implemented

**Date:** 2026-07-28
**Status:** P0 (protocol layer) and P1 (runtime, agent pool, thread map, translation, endpoint) DONE.
**Plan:** [[pando/analysis/copilotkit_agui_integration_analysis.md]] (rev. 2, isolated side-car architecture)

## What was implemented

The AG-UI protocol (https://docs.ag-ui.com) as an **isolated side-car adapter**, so CopilotKit and other
Generative-UI frontends can drive a Pando agent from a browser. Per the user's explicit constraint, the
adapter does **not** flow through the same channel as the TUI, Web-UI or ACP surfaces.

### New package `internal/agui` (all new code)

| File | Contents |
|---|---|
| `doc.go` | The seven isolation invariants (I1-I7) and the implementation-status contract |
| `events.go` | Full AG-UI event catalogue (lifecycle, text, tool call, state, reasoning, activity, custom/raw) with SCREAMING_SNAKE type values and camelCase JSON fields, plus constructors |
| `input.go` | `RunAgentInput`, `Message` (with string-or-multimodal `MessageContent`), `Tool`, `Context`; decode + validation; `LastUserMessage`, `TrailingToolMessages`, `ContextBlock` |
| `sse.go` | `SSEWriter` — bare `data:` frames (AG-UI puts the discriminator inside the JSON), heartbeat comments, `X-Accel-Buffering: no` |
| `deps.go` | `Deps` (narrow dependency struct, no `*app.App`) and `Config` + `ConfigFromApp` defaults resolution |
| `agentpool.go` | Agent instances keyed by `(agentName, hash(frontend toolset))`, LRU + idle TTL |
| `translate.go` | `agent.AgentEvent` → AG-UI events, stateful per run |
| `hitl.go` | Non-blocking stand-in policy until P4 |
| `runtime.go` | `Runtime`, thread↔session store, agent resolution |
| `server.go` | `POST {path}/{agent}`, `GET {path}/info`, preflight, auth + CORS, the SSE stream pump |

### Why no core change was needed

`agent.NewAgent` and `agent.CoderAgentToolsWithMesnada` are already exported, and the latter takes the
permission and user-input services **as parameters** (`internal/llm/agent/tools.go:187`). The adapter therefore
builds its own agents exactly like `internal/app/app.go:636` does, with its own
`permission.NewPermissionService()` and `userinput.NewService()`. This removed rev. 1's only invasive step
(per-run tool injection into the shared agent).

### Existing files touched (~120 LOC, all additive, gated by a disabled-by-default flag)

- `internal/config/config.go`: new `AGUIConfig` struct + `Config.AGUI` field + `setDefaults` block
  (`agui.enabled=false`, `path=/api/v1/agui`, `agents=["coder"]`, `requireToken=true`, `frontendTools=true`,
  `agentPoolSize=4`, `agentPoolTtl=30m`, `autoApprove=false`).
- `internal/api/server.go`: `agui`/`aguiPath` fields, `setupAGUI()`, `isAGUIPath()`, CORS-middleware skip,
  auth-middleware skip, `Close()` on shutdown.
- `internal/api/routes.go`: three-line mount at the end of `registerRoutes`.

Two middleware skips were required and are part of the isolation contract, not a workaround:
`corsMiddleware` sets `Access-Control-Allow-Origin: *` on every route, which must never reach a
code-executing agent surface; and `authMiddleware` only understands `X-Pando-Token`, while AG-UI clients send
`Authorization: Bearer`. The adapter validates the *same* token itself.

## Behaviour

* `POST /api/v1/agui/{agent}` accepts a `RunAgentInput` and streams `RUN_STARTED … RUN_FINISHED|RUN_ERROR`.
* Only the trailing user message is forwarded to Pando (the client's full transcript is never replayed), so
  compaction, prompt caching and the skills pipeline are untouched. `context[]` is prefixed as a
  `<context>` block.
* Thread ids map to Pando sessions (in-memory in P1; a client may also pass an existing session id as its
  thread id). New sessions are titled `agui: <prompt>` so they are distinguishable in the session list.
* Reasoning, text and tool blocks are opened lazily and always closed before the run ends. Pando's growing
  tool-call snapshots become incremental `TOOL_CALL_ARGS` deltas.
* Pando-only events (`system_message`, `todos_updated`, `token_usage`, steering/conclusion/resurrection) are
  emitted as namespaced `CUSTOM` events until P2 turns the relevant ones into `STATE_DELTA`.
* Frontend tools are accepted and acknowledged with a `pando.frontendToolsUnsupported` custom event; the pool
  key is already toolset-aware so P3 only has to add the proxies.
* Until P4, permission requests inside an AG-UI run are denied unless `AutoApprove` is set, and agent
  questions are cancelled — non-blocking by design, so a run can never hang on a dialog nobody can see.

## Verification

* `go build ./...`, `go vet`, `gofmt` — clean.
* `go test ./internal/agui ./internal/api ./internal/config ./internal/llm/agent` — all pass, also under `-race`.
* New tests:
  * `internal/agui/translate_test.go` — text/reasoning/tool lifecycles, incremental args, implicit
    `TOOL_CALL_END` on a result, block closing on finish/fail, custom-event namespacing, and a wire-format
    test pinning the serialized `type`/field names.
  * `internal/agui/input_test.go` — validation, string vs multimodal content, trailing tool messages, size
    limit, config defaults and unknown-agent pruning.
  * `internal/agui/sse_test.go` — frame format (no `event:` field), heartbeat, post-close writes, flusher requirement.
  * `internal/agui/server_test.go` — auth (bearer/query/missing/fail-closed), origin allow-list, no implicit
    wildcard CORS, `/info`, agent resolution, pool-key semantics.
  * `internal/api/agui_isolation_test.go` — adapter invisible when disabled; CORS and token middleware skip
    AG-UI paths **without** regressing the existing routes.
* Not run: live E2E against CopilotKit / the AG-UI Dojo (needs provider credentials and a Node frontend).
  That is the P6 example-app deliverable.

## Usage

```toml
[AGUI]
Enabled        = true
AllowedOrigins = ["http://localhost:3000"]
```

```ts
new CopilotRuntime({
  agents: {
    pando: new HttpAgent({
      url: 'http://localhost:8080/api/v1/agui/coder',
      headers: { Authorization: `Bearer ${process.env.PANDO_TOKEN}` },
    }),
  },
})
```

## Next

P2 state (`STATE_SNAPSHOT`/`STATE_DELTA`), P3 frontend tools (blocking proxies + interrupt/resume),
P4 human-in-the-loop, P5 durable thread table + dedicated listener/`agui-serve`, P6 SDK subpath + example app.
