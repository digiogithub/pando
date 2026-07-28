---
created_at: 2026-07-28T07:54:23.204597927Z
updated_at: 2026-07-28T09:33:01.294093788Z
tags:
    - analysis
    - agui
    - copilotkit
    - genui
    - web
    - sdk
    - api
    - plan
---
# Analysis: Pando + Pando SDK as agents for CopilotKit / Generative UI web apps (AG-UI protocol)

**Date:** 2026-07-28 (rev. 2 — isolated-adapter architecture)
**Status:** Analysis + implementation proposal (not implemented)
**Question:** How can Pando (Go binary/server) and `@pando-ai/sdk` (TypeScript) be plugged into web frontends built with CopilotKit / Generative UI, the same way Mastra, LangGraph, Agno, PydanticAI or Microsoft Agent Framework do?

Related: [[pando/serve-app-mode-part1-overview.md]], [[pando/plans/acp_implementation_plan.md]], [[project_ask_user_question_tool_plan]], [[pando/plans/webui_settings_complete_plan.md]]

> **Rev. 2 constraint (explicit user requirement):** the AG-UI integration must **not** be a drastic change to
> Pando. It must **not** flow through the same channel/instance the ACP, WebUI and TUI surfaces use. AG-UI is a
> *side-car adapter*: its own agent instance, its own permission/user-input services, its own run registry,
> its own HTTP surface. Existing surfaces must be bit-for-bit unaffected when the feature is disabled — and
> behaviourally unaffected even when it is enabled. §3 was rewritten around this; §0 states the invariants.

---

## 0. Architectural invariants (rev. 2)

| # | Invariant | Enforcement |
|---|---|---|
| I1 | **No change to `agent.NewAgent` / `agent.Run` signatures.** No per-run tool injection into the shared agent. | AG-UI builds *its own* `agent.Service` instances through the already-exported `agent.NewAgent(...)` + `agent.CoderAgentToolsWithMesnada(...)`, exactly as `internal/app/app.go:636` does. |
| I2 | **`app.CoderAgent` is never touched.** TUI, WebUI (`/api/v1/chat/stream`), ACP and `bgRunner` keep using it untouched. | AG-UI never reads `app.CoderAgent`; it holds `agui.Runtime` with its own agent pool. |
| I3 | **No AG-UI prompt ever reaches a TUI/desktop user.** | AG-UI gets its own `permission.NewPermissionService()` and its own `userinput` service instance, passed into *its* tool factory call. Nothing published on the shared brokers. |
| I4 | **No new event types in `agent.AgentEvent`.** Translation is one-way and lives in the adapter. | `internal/agui/translate.go` consumes the existing public event stream only. |
| I5 | **Off by default, and removable.** Deleting `internal/agui` + ~30 lines of wiring restores the previous tree. | Config gate `[AGUI] Enabled=false`; handler mounted only when non-nil. |
| I6 | **No shared mutable state with other surfaces** beyond read-mostly singletons (DB, config, MCP gateway, LSP). | Deps passed explicitly through a narrow `agui.Deps` struct; no `*app.App` import (avoids the cycle too). |
| I7 | **Separate transport surface.** Own route namespace, own auth/CORS policy, optionally its own port/process. | `/api/v1/agui/*` on the existing mux, or `pando agui-serve --port` as a standalone process. |

Consequence: the risky item of rev. 1 (mutating the shared agent's tool wiring to inject frontend tools)
**disappears**. It was the only invasive step, and per-instance agents remove the need for it entirely.

---

## 1. Findings — how CopilotKit actually connects to agents

### 1.1 The integration point is AG-UI, not a CopilotKit-proprietary API

CopilotKit is the *frontend* stack (React/Angular/mobile chat surfaces + Generative UI). The wire contract
between CopilotKit and any backend agent is the **AG-UI protocol** (Agent-User Interaction Protocol),
authored by CopilotKit and implemented by LangGraph, Mastra, CrewAI, Agno, PydanticAI, Microsoft Agent
Framework, trpc-agent-go, etc.

Therefore: **"integrating Pando with CopilotKit" == "making Pando speak AG-UI"**. Every first-party framework
integration (`@ag-ui/mastra`, `@ag-ui/langgraph`, …) is just an AG-UI adapter — which is precisely why it can
live at the edge of Pando instead of inside it.

### 1.2 Runtime topology

```
Browser (React)                 Node/Next runtime              Pando (AG-UI adapter)
┌───────────────────────┐      ┌──────────────────────┐       ┌────────────────────────┐
│ <CopilotKit           │      │ CopilotRuntime       │       │ POST /api/v1/agui/{a}  │
│    runtimeUrl=/api/ck │─────▶│  agents: {           │──────▶│ RunAgentInput → SSE    │
│    agent="pando" />   │ HTTP │   pando: new         │ AG-UI │ (own agent pool,       │
│ CopilotChat / Sidebar │      │    HttpAgent({url})  │  SSE  │  own permissions)      │
│ useCopilotAction(...) │      │  }                   │       └────────────────────────┘
└───────────────────────┘      └──────────────────────┘
```

* `CopilotRuntime` takes a **map of AG-UI agent instances**; a remote one is `new HttpAgent({ url, headers })`.
* The runtime exposes `/info` for agent discovery; each agent is wrapped in a `ProxiedCopilotRuntimeAgent`
  (thin subclass of AG-UI `HttpAgent`).
* Frontend selects by name: `<CopilotKit runtimeUrl="..." agent="pando">`.
* Headers set on the `HttpAgent` (`Authorization: Bearer …`) take precedence over forwarded inbound headers —
  where Pando's token auth plugs in.
* Mastra's `registerCopilotKit({ path, resourceId })` mounts a CopilotKit-compatible route inside the agent
  server, removing the Node hop. Deferred to P7 here; the plain AG-UI endpoint is universally supported.

### 1.3 The AG-UI contract Pando must implement

**Request** — `POST <endpoint>` with JSON `RunAgentInput`:

```ts
type RunAgentInput = {
  threadId: string
  runId: string
  parentRunId?: string
  state: any            // shared state (agent ↔ UI)
  messages: Message[]   // full conversation history
  tools: Tool[]         // FRONTEND-defined tools { name, description, parameters(JSONSchema) }
  context: Context[]    // [{ description, value }] ambient page context
  forwardedProps: any
}
```

Message roles: `developer | system | assistant | user | tool | activity | reasoning`.
`UserMessage.content` may be `string` or multimodal `InputContent[]` (text/image/audio/video/document).

**Response** — stream of typed `BaseEvent`s. Default transport SSE (`text/event-stream`):

| Group | Events |
|---|---|
| Lifecycle | `RunStarted`(threadId,runId) · `RunFinished`(outcome,result) · `RunError`(message,code) · `StepStarted`/`StepFinished`(stepName) |
| Text | `TextMessageStart`(messageId,role) · `TextMessageContent`(delta) · `TextMessageEnd` · `TextMessageChunk` |
| Tools | `ToolCallStart`(toolCallId,toolCallName,parentMessageId) · `ToolCallArgs`(delta) · `ToolCallEnd` · `ToolCallResult`(messageId,toolCallId,content) · `ToolCallChunk` |
| State | `StateSnapshot`(snapshot) · `StateDelta`(RFC-6902 JSON Patch) · `MessagesSnapshot` |
| Reasoning | `ReasoningStart` · `ReasoningMessageStart/Content/End` · `ReasoningEnd` · `ReasoningEncryptedValue` |
| Activity | `ActivitySnapshot`(messageId,activityType,content,replace) · `ActivityDelta`(patch) |
| Escape hatch | `Raw`(event,source) · `Custom`(name,value) |

Canonical ordering: `RunStarted → (StepStarted/StepFinished)* → RunFinished | RunError`.

**Frontend tools / HITL / Generative UI** — the defining AG-UI mechanic:
1. Frontend declares tools (`useCopilotAction` / `useFrontendTool` / `useHumanInTheLoop`); they arrive in
   `RunAgentInput.tools`.
2. Agent emits `ToolCallStart/Args/End` for one and **ends the run** (`outcome:"interrupt"`).
3. The browser executes it (or renders a confirmation UI and waits for the human).
4. Next `POST` carries a `ToolMessage` with the result; the agent resumes.

Generative UI is a frontend rendering concern (`render` in `useCopilotAction`, `useCoAgentStateRender`) driven
by streamed tool-call args + `StateSnapshot`/`StateDelta`. The backend only emits well-typed tool calls and state.

### 1.4 Go SDK availability

`github.com/ag-ui-protocol/ag-ui/sdks/community/go` — community Go SDK: core event structs, JSON
encoder/decoder (`JSONDecoder`, strict mode), and an **`SSEWriter`** that serializes AG-UI events to SSE wire
format for HTTP handlers. Client-leaning, but the event types + `SSEWriter` are the server-side pieces needed.
`trpc-agent-go` proves the pattern (native AG-UI Go server over SSE).

Decision: **vendor/copy the event structs into `internal/agui/events.go` unless the community SDK proves
stable.** The surface is plain JSON (~400 LOC). This also keeps invariant I5 (removable) and avoids adding a
third-party dependency to the whole module for an off-by-default feature.

---

## 2. What Pando already has

| AG-UI need | Pando today | Verdict for an isolated adapter |
|---|---|---|
| Streaming HTTP | `internal/api` + `pando serve`/`app`; SSE at `/api/v1/chat/stream`, `/api/v1/sessions/{id}/stream` | Mount a **new** route namespace; do not modify existing handlers |
| Agent event stream | `agent.AgentEvent`: `content_delta`, `thinking_delta`, `tool_call`, `tool_result`, `response`, `error`, `summarize`, `todos_updated`, `system_message`, `token_usage`, `steering_*`, `conclusion_*`, `resurrected` | Consume via public `Run()`/`Subscribe()`; translate in the adapter |
| Agent construction | `agent.NewAgent(name, sessions, messages, tools, skillManager)` + `agent.CoderAgentToolsWithMesnada(orchestrator, remembrances, gateway, permissions, history, lspProvider, userInput, sessions)` — **both exported** | **This is the whole trick**: the adapter can build its own agents with its own tools/permissions with zero core edits |
| Threads | `session.Service`, sessions in SQLite | `threadId → sessionID` map owned by the adapter |
| HITL | `internal/permission.Request()` blocks until answered; `internal/userinput.Ask()` blocks (`AskUserQuestion`) | Adapter instantiates **its own** instances so prompts never reach TUI/WebUI |
| Auth | API token (`/api/v1/token`), basic auth (`internal/api/basicauth.go`) | Reused as `HttpAgent({headers})`; AG-UI route gets a stricter CORS policy |
| Durability | `internal/api.bgRunner` survives HTTP disconnects | **Not reused** (api-internal); adapter has its own equivalent `runRegistry` — smaller and interrupt/resume-aware |
| SDKs | `sdk/typescript` (`@pando-ai/sdk`: subprocess + ACP stdio, typed event parser); `dotnet`, `java`, `python` | Add an *additive* `@pando-ai/sdk/agui` subpath export; existing modes untouched |
| Multi-agent | `config.KnownAgentNames`, mesnada, personas | Maps to CopilotKit's `agents: {}` map |

**Model mismatch to design around:** Pando keeps state server-side (history in SQLite, tools run server-side),
while AG-UI clients resend the full `messages` array each run and expect some tools to run in the browser.
Reconciled in §3.4/§3.5 — inside the adapter, not inside the agent.

---

## 3. Proposed implementation — the isolated adapter

### 3.1 Package layout — new code only

```
internal/agui/
  doc.go            // states invariants I1-I7
  events.go         // AG-UI event structs + JSON tags (vendored)
  sse.go            // SSE writer: "data: {json}\n\n", flush, heartbeat, backpressure
  input.go          // RunAgentInput decode + validation
  deps.go           // agui.Deps: the narrow dependency struct (no *app.App)
  runtime.go        // agui.Runtime: agent pool, thread map, run registry — the adapter's core
  agentpool.go      // agent.Service instances keyed by (agentName, toolsetHash), LRU + TTL
  translate.go      // agent.AgentEvent -> []agui.Event
  state.go          // per-thread state doc + RFC-6902 delta generation
  frontend_tool.go  // tools.BaseTool proxies for RunAgentInput.tools (blocking, resolved by ToolMessage)
  hitl.go           // adapter-local permission + userinput services -> AG-UI tool calls
  server.go         // http.Handler: POST run endpoint, GET /info, auth, CORS
  *_test.go
```

`agui.Deps` (constructed by the caller, mirroring `app.go:636-651`):

```go
type Deps struct {
    Sessions     session.Service
    Messages     message.Service
    History      history.Service
    Skills       *skills.SkillManager
    Gateway      *mcpgateway.Gateway            // read-mostly, shared
    Orchestrator *orchestrator.Orchestrator     // optional, may be nil
    Remembrances *rag.RemembrancesService
    LSP          tools.LSPProvider              // *app.App satisfies this today
}
```

Note what is **absent**: `permission.Service` and `userinput.Service` are *not* injected — the runtime creates
its own (invariant I3). And `agent.Service` is absent — the runtime creates its own (I2).

### 3.2 Agent pool — why no core change is needed

Per AG-UI thread, the runtime needs an agent whose toolset = Pando's normal coder tools **+** the frontend
tools declared by that page. Rev. 1 wanted to inject them per run into the shared agent (invasive). Rev. 2
instead does what `app.go` already does, in a loop:

```go
func (p *pool) get(agentName config.AgentName, frontend []agui.Tool) (agent.Service, error) {
    key := agentName.String() + ":" + hashToolSchemas(frontend)
    if a, ok := p.cache.Get(key); ok { return a, nil }

    base := agent.CoderAgentToolsWithMesnada(
        p.deps.Orchestrator, p.deps.Remembrances, p.deps.Gateway,
        p.perms,      // adapter-local permission service
        p.deps.History, p.deps.LSP,
        p.userInput,  // adapter-local user-input service
        p.deps.Sessions,
    )
    all := append(base, newFrontendToolProxies(frontend, p.pending)...)

    a, err := agent.NewAgent(agentName, p.deps.Sessions, p.deps.Messages, all, p.deps.Skills)
    ...
    p.cache.Add(key, a)  // LRU + idle TTL
    return a, err
}
```

Cost: one extra provider client per distinct toolset (not per thread — pages share a toolset, so in practice
1–3 instances). Bounded by LRU size + TTL, both configurable. Zero lines changed in `internal/llm/agent`.

### 3.3 Event translation (`translate.go`) — pure function, adapter-local

| Pando `AgentEvent` | AG-UI emission |
|---|---|
| run start (handler-level) | `RunStarted{threadId,runId}` |
| `content_delta` | `TextMessageStart`(once, role=assistant) → `TextMessageContent{delta}` |
| `thinking_delta` | `ReasoningStart` → `ReasoningMessageContent{delta}` → `ReasoningEnd` |
| `tool_call` | `ToolCallStart{toolCallId,toolCallName}` → `ToolCallArgs{delta}` → `ToolCallEnd` |
| `tool_result` | `ToolCallResult{toolCallId,content}` |
| `response` | `TextMessageEnd` + `RunFinished{result}` |
| `error` | `RunError{message}` |
| `summarize` / `system_message` / `resurrected` / `steering_*` / `conclusion_*` | `Custom{name:"pando.<type>"}` or `ActivitySnapshot` |
| `todos_updated` | `StateDelta` on `state.todos` |
| `token_usage` | `StateDelta` on `state.tokenUsage` |
| adapter-local permission request | `ToolCallStart/Args/End` for `pando_permission_request`, then interrupt |
| adapter-local `AskUserQuestion` | same pattern with the existing `AskUserQuestion` schema |

Testable in isolation: feed a recorded `[]agent.AgentEvent`, assert the AG-UI JSON sequence. No server needed.

### 3.4 Thread ↔ session reconciliation (adapter-owned)

* `threadId → sessionID` map persisted in an adapter-owned table (`agui_threads`) or in-memory + session
  metadata. **No schema change to `sessions`** — a new table keeps the migration additive and drop-safe (I5).
* Unknown `threadId` → create a Pando session (title prefixed, e.g. `agui: <first prompt>`), so AG-UI threads
  are visible but distinguishable in the session list.
* **Never replay the whole `messages` array into Pando.** Diff against stored history, submit only the trailing
  user message(s). Preserves compaction, prompt caching, skills and the whole prompt pipeline untouched.
* `ToolMessage` entries are *not* prompts: they resolve a pending frontend-tool channel (§3.5).
* Reconnect/refresh: replay from the adapter's run registry, or emit `MessagesSnapshot` from Pando's DB so the
  UI resyncs from the server (server is the source of truth for context; CopilotKit for the visual transcript).

### 3.5 Frontend tools — proven blocking pattern, no core change

Each `RunAgentInput.tools` entry becomes a `tools.BaseTool` proxy whose `Run()`:
1. is called by the normal agent loop (so it emits the ordinary `tool_call` event → translated to `ToolCall*`),
2. registers `toolCallId` in the runtime's pending map and **blocks on a channel**,
3. the HTTP handler, having flushed `ToolCallEnd`, closes the SSE stream with `RunFinished{outcome:"interrupt"}`
   while the agent goroutine stays alive and blocked,
4. the next `POST` for the same `threadId` carries the `ToolMessage`; the handler pushes the result into the
   channel and re-subscribes to the *same live run*, continuing the SSE stream.

This is the exact shape `internal/permission.Request()` and `userinput.Ask()` already use — proven in this
codebase, and entirely implementable with the public `tools.BaseTool` interface.

### 3.6 Shared state

Per-thread JSON doc: `{ session, model, tokenUsage, todos, files, permissions, mesnadaTasks }`.
`StateSnapshot` right after `RunStarted`, then `StateDelta` (RFC-6902) as things change. Frontend consumes it
with `useCoAgent` / `useCoAgentStateRender`. Inbound `RunAgentInput.state` lets the page push UI state
(open file, selected repo) into the run, mapped to Pando's project/context mechanisms — read-only, no writes
into shared config.

### 3.7 HITL isolation (invariant I3, spelled out)

The adapter constructs `permission.NewPermissionService()` and its own `userinput` service. Consequences:

* A web user approving a `bash` command approves it **only** for AG-UI threads. TUI/WebUI/ACP sessions keep
  their own permission service and their own auto-approve state.
* `SetGlobalAutoApprove` on the AG-UI service cannot leak into the desktop surfaces.
* Both services' pending prompts are surfaced as AG-UI tool calls, so CopilotKit's `useHumanInTheLoop` renders
  them natively — no `/api/v1/permissions/respond` coupling required (that route stays for the WebUI).

### 3.8 Transport isolation — three deployment shapes

1. **Mounted (default):** `POST /api/v1/agui/{agent}` + `GET /api/v1/agui/info` on the existing server, gated by
   `[AGUI] Enabled`. Wiring cost: one nil-check block in `routes.go`.
2. **Own port:** `pando serve --agui-port 8090` — a second `http.Server` with only the AG-UI mux. Nothing else
   is reachable from the web origin. Recommended for public deployments.
3. **Own process:** `pando agui-serve --port 8090 --cwd /project` — a dedicated instance; the strongest
   isolation from any TUI/desktop instance (separate DB connection, separate agent, separate permissions).

Auth/CORS: token required by default; explicit `AllowedOrigins` list. **Never** copy the
`Access-Control-Allow-Origin: *` used by `handleChatStream` (`internal/api/handlers_chat.go:150`) — that would
expose a code-executing agent to any origin.

### 3.9 Config

```toml
[AGUI]
Enabled        = false          # opt-in network surface
Path           = "/api/v1/agui"
Port           = 0              # 0 = mount on the main server; >0 = dedicated listener
Agents         = ["coder"]      # names exposed in /info
AllowedOrigins = []             # required when Enabled
RequireToken   = true
FrontendTools  = true           # allow RunAgentInput.tools proxying
AgentPoolSize  = 4
AgentPoolTTL   = "30m"
AutoApprove    = false          # adapter-local only; never touches other surfaces
```

### 3.10 Exact touch list in existing code (the whole "drastic change" budget)

| File | Change | ~LOC |
|---|---|---|
| `internal/config/config.go` | `AGUI` struct + defaults | ~25 |
| `internal/api/routes.go` | `if s.agui != nil { s.agui.Register(mux) }` | ~3 |
| `internal/api/server.go` | optional `agui *agui.Runtime` field | ~3 |
| `internal/app/app.go` | build `agui.Deps` + `agui.New(...)` when enabled (mirrors the existing agent block) | ~20 |
| `cmd/*.go` | `--agui-port` flag / `agui-serve` subcommand | ~40 |
| **Total existing-file diff** | | **~90 LOC, all additive and behind a disabled-by-default gate** |

Everything else is new files under `internal/agui/`. No existing function signature changes. No existing
behaviour changes. `git revert` of the wiring + `rm -rf internal/agui` is a clean rollback.

### 3.11 SDK work (additive)

**TypeScript (`sdk/typescript`)** — a third mode alongside subprocess and ACP, as a **separate subpath export**
so existing consumers' bundles are unaffected:

```ts
// @pando-ai/sdk/agui
export class PandoAgent extends HttpAgent {}          // thin, typed, auth-aware
export function registerPandoCopilotKit(opts): Route  // Mastra-style one-liner (P7)
```

Consumer code:

```ts
// app/api/copilotkit/route.ts
import { CopilotRuntime } from '@copilotkit/runtime'
import { HttpAgent } from '@ag-ui/client'

const runtime = new CopilotRuntime({
  agents: {
    pando: new HttpAgent({
      url: 'http://localhost:8090/api/v1/agui/coder',
      headers: { Authorization: `Bearer ${process.env.PANDO_TOKEN}` },
    }),
  },
})
```

```tsx
<CopilotKit runtimeUrl="/api/copilotkit" agent="pando">
  <CopilotSidebar labels={{ title: 'Pando' }} />
</CopilotKit>
```

`@ag-ui/client` is a peer dependency of the `agui` subpath only — it must not become a hard dependency of
`@pando-ai/sdk`. Python/Java/.NET SDKs: nothing required for CopilotKit.

### 3.12 Explicitly out of scope

* **Migrating Pando's own WebUI to AG-UI.** Rev. 1 floated it; rev. 2 rejects it — the WebUI depends on many
  Pando-only events, and rewriting it would be exactly the drastic change this revision exists to avoid.
* Changing ACP, TUI, `bgRunner`, or the `/api/v1/chat/*` shape in any way.
* Replacing `agent.AgentEvent` with AG-UI events internally.

---

## 4. Phasing (rev. 2)

| Phase | Scope | Touches existing code? | Outcome |
|---|---|---|---|
| **P0** | `internal/agui` skeleton: vendored events, SSE writer, `RunAgentInput` decode, `doc.go` invariants | No | Unit-testable protocol layer, zero risk |
| **P1** | `Runtime` + agent pool + thread map + `translate.go` + `POST /api/v1/agui/{agent}` mounted behind config | ~90 LOC (§3.10) | `CopilotChat` talks to Pando: streaming text, reasoning, tool calls |
| **P2** | State: `StateSnapshot`/`StateDelta` for todos, token usage, model, files | No | `useCoAgent` dashboards / GenUI state rendering |
| **P3** | Frontend tools: proxies + pending registry + interrupt/resume | No (§3.2 removed the core change) | `useCopilotAction` / `useFrontendTool` work — real Generative UI |
| **P4** | HITL: adapter-local permission + userinput services surfaced as AG-UI tool calls | No | Approve diffs / answer questions from the web app, without touching TUI |
| **P5** | Hardening: auth, `AllowedOrigins`, `/info`, dedicated port + `agui-serve` process, multi-agent map | ~40 LOC (cmd) | Production-ready, isolatable surface |
| **P6** | `@pando-ai/sdk/agui` subpath + Next.js example under `examples/copilotkit/` + docs | No (SDK only) | One-liner adoption |
| **P7 (opt.)** | Mastra-style embedded CopilotKit route (skip the Node hop); AG-UI for mesnada sub-agents | New files | Zero-Node deployments |

Rough size: P0+P1 ≈ 800–1000 LOC of **new** Go, ~90 LOC of existing-file wiring. P3 is no longer the risky
phase — the agent pool absorbed that risk in P1.

## 5. Risks / open questions (rev. 2)

* **Agent-pool cost** — each distinct toolset builds a provider client. Mitigate with LRU + idle TTL and by
  hashing tool *schemas* (pages usually share one toolset). Watch memory when many origins connect.
* **Message-history divergence** — CopilotKit owns the browser transcript, Pando's DB owns model context.
  Diff-based ingestion (§3.4) must be conservative; prefer `MessagesSnapshot` on reconnect.
* **Long runs vs run boundaries** — Pando runs last minutes; AG-UI expects `RunFinished` per turn. Keep the
  background run alive across interrupt/resume rather than restarting the agent loop.
* **Security** — a code-executing agent exposed to a browser origin. Off by default, token required, pinned
  origins, adapter-local permissions with `AutoApprove=false`, and the dedicated-process option for anything public.
* **Shared read-mostly singletons** — MCP gateway, LSP provider and config are shared with other surfaces. They
  are read-mostly, but any adapter code path that *reloads* MCP servers or mutates config must be forbidden
  (the AG-UI agent should not expose `pando_setup`-style self-configuration by default).
* **Vendored event structs** — must track upstream AG-UI spec revisions; pin the spec version in `doc.go`.
* **Multimodal `InputContent`** — Pando's `message.Attachment` covers images; audio/video/document need mapping
  or explicit rejection with `RunError`.

## 6. Verification plan

* **Isolation tests (the point of rev. 2):**
  * with `[AGUI] Enabled=false`, `routes.go` registers no AG-UI route and `app.CoderAgent` construction is byte-identical;
  * an AG-UI permission request never appears on the shared `permission.Service` broker (assert no subscriber event);
  * an AG-UI run does not appear in `bgRunner` and does not disturb a concurrent WebUI run on the same server.
* Unit: `translate.go` golden tests (recorded `agent.AgentEvent` stream → expected AG-UI JSON sequence),
  `RunAgentInput` decode, RFC-6902 patch generation, agent-pool key hashing/eviction.
* Integration: `httptest` server asserting canonical ordering (`RunStarted … RunFinished`) and the
  tool-call interrupt/resume cycle across two POSTs on one `threadId`.
* E2E: AG-UI **Dojo** (official conformance/demo app) against `pando agui-serve`, plus a minimal
  Next.js + CopilotKit example.
* Regression: existing suites must be untouched — `go test ./internal/llm/agent ./internal/api`.

## 7. Sources

- AG-UI docs: events, architecture, tools, core types — https://docs.ag-ui.com
- AG-UI protocol repo + Go SDK — https://github.com/ag-ui-protocol/ag-ui , `sdks/community/go`
- CopilotKit docs — https://docs.copilotkit.ai/ , https://docs.copilotkit.ai/backend/ag-ui
- CopilotKit AG-UI landing — https://www.copilotkit.ai/ag-ui
- Mastra × CopilotKit — https://mastra.ai/guides/build-your-ui/copilotkit/overview
- LangChain/LangGraph × CopilotKit — https://docs.langchain.com/oss/python/langchain/frontend/integrations/copilotkit
- trpc-agent-go native AG-UI Go server — https://trpc-group.github.io/trpc-agent-go/agui/
- Microsoft Agent Framework AG-UI — https://learn.microsoft.com/en-us/agent-framework/integrations/ag-ui/

## 8. Code anchors (verified 2026-07-28)

- `internal/app/app.go:636` — `agent.NewAgent(config.AgentCoder, app.Sessions, app.Messages, agent.CoderAgentToolsWithMesnada(...), app.SkillManager)` — the exact pattern the adapter replicates.
- `internal/llm/agent/tools.go:187` — `CoderAgentToolsWithMesnada(orchestrator, remembrances, gateway, permissions, history, lspProvider, userInput, sessions)` — exported, takes the permission and user-input services as parameters, which is what makes HITL isolation free.
- `internal/llm/agent/agent.go:139-184` — `AgentEventType` catalogue consumed by `translate.go`.
- `internal/llm/agent/agent.go:801` — `Run(ctx, sessionID, content, attachments...) (<-chan AgentEvent, error)` — unchanged public entry point.
- `internal/permission/permission.go:129` — `Request()` blocking pattern reused for frontend tools.
- `internal/userinput/userinput.go:59-71` — `Ask`/`Respond`/`Cancel`/`PendingRequests` — same pattern.
- `internal/api/routes.go:8` — `registerRoutes(mux)`, the single mount point.
- `internal/api/handlers_chat.go:150` — `Access-Control-Allow-Origin: *` — the anti-pattern the AG-UI route must not copy.
