// Package agui implements the AG-UI protocol (Agent-User Interaction Protocol,
// https://docs.ag-ui.com) as an isolated side-car adapter on top of Pando.
//
// AG-UI is the wire contract CopilotKit and other Generative-UI frontends speak
// to any agent backend. Making Pando speak it is what lets a React application
// drive a Pando agent with `<CopilotKit agent="pando">` and zero Pando-specific
// frontend code.
//
// # Architectural invariants
//
// This package is deliberately a leaf adapter. It must never become a second
// implementation of the agent, and it must never disturb the surfaces that
// already exist (TUI, Web-UI, ACP). The following invariants are load-bearing:
//
//   - I1: no change to agent.NewAgent / agent.Run signatures. This package
//     builds its OWN agent.Service instances through the already-exported
//     constructors, exactly like internal/app/app.go does.
//   - I2: app.CoderAgent is never read or mutated here. TUI/Web-UI/ACP and the
//     api BackgroundSessionManager keep using it untouched.
//   - I3: no AG-UI prompt ever reaches a desktop user. The runtime creates its
//     own permission.Service and userinput.Service, so approvals and questions
//     raised by a web run stay inside this adapter.
//   - I4: no new event types in agent.AgentEvent. Translation to AG-UI events is
//     one-way and lives in translate.go.
//   - I5: off by default and removable. Deleting this package plus the ~90 lines
//     of wiring in config/app/api restores the previous tree.
//   - I6: no *app.App import (that would also be an import cycle); dependencies
//     arrive through the narrow Deps struct.
//   - I7: its own route namespace, its own auth/CORS policy, optionally its own
//     listener.
//
// # Implementation status
//
// P0 (protocol layer), P1 (runtime, agent pool, thread map, translation,
// endpoint), P2 (shared state), P3 (frontend tools) and P4 (human in the loop)
// are implemented:
//
//   - P2: STATE_SNAPSHOT after every RUN_STARTED and STATE_DELTA (RFC-6902) for
//     todos, token usage and touched files. See state.go.
//
//   - P3: RunAgentInput.Tools become blocking tools.BaseTool proxies; a call
//     suspends the run with RUN_FINISHED{outcome:"interrupt"} and is resolved by
//     the tool message of the next request on the thread. Runs are therefore
//     detached from their HTTP request; see run.go and frontend_tool.go.
//
//   - P4: permission prompts reach the client as a synthetic
//     pando_permission_request tool call, and AskUserQuestion is substituted by
//     a tool that waits on the client instead of on a local overlay. Both fail
//     closed (deny / cancel) when nobody answers. Gated by
//     Config.HumanInTheLoop; with it off the pre-P4 policy applies. See hitl.go.
//
//   - P5: durable thread->session mapping in the adapter-owned agui_threads
//     table (threads.go), hardened /info discovery, and the dedicated listener
//     of invariant I7 (listener.go), reachable through `pando serve
//     --agui-port` or the standalone `pando agui-serve` process.
//
//   - P6: the client half, outside this package — the `@pando-ai/sdk/agui`
//     subpath export and the Next.js application under examples/copilotkit.
//     No Go code was involved, which is the point: the protocol is the contract.
//
//   - P7: mesnada sub-agents are projected into the shared-state document as
//     StateDoc.SubAgents (subagents.go), derived from the mesnada_* tool traffic
//     the adapter already observes — the orchestrator is never consulted and no
//     agent event type was added.
//
// # Tool calls that never reach the event stream
//
// agent.processEvent only publishes AgentEventTypeToolCall for providers that
// stream tool use incrementally (EventToolUseStart/Delta/Stop). A provider that
// reports its tool calls in one final EventComplete leaves the assistant message
// correct while the event stream stays silent about them. Every other surface
// renders from the database and never notices; this adapter has only the stream,
// so it must reconstruct what the stream omits: a suspending call carries its
// own name and arguments (suspension.call) so the handler can emit
// START/ARGS/END before the interrupt, and a tool result for a call that was
// never opened opens it first (translate.go), because a bare TOOL_CALL_END is
// rejected by AG-UI clients as a protocol error that aborts the whole run.
//
// # Tool results are not JSON
//
// tools.NewStructuredResponse renders TOON when it can, TOML next and indented
// JSON only as a last resort, and it chooses per value. Any code here that reads
// a tool result structurally must go through decodeToolResult (subagents.go);
// json.Unmarshal alone silently sees nothing, which is what kept the sub-agent
// board empty until 2026-07-29.
//
// # The state document belongs to the thread
//
// StateDoc is per thread and lives in the Runtime's stateStore, not in the run:
// a conversation's todos, touched files and sub-agents must survive its turns.
// A per-run document made them vanish on every new message. The store is memory
// only — the durable half is the thread->session binding in threads.go — and it
// evicts the least recently used threads past maxStateThreads.
//
// # Deliberately not implemented
//
// CopilotKit's own runtime protocol (GraphQL) is not served by Pando. The
// remaining half of P7 was "skip the Node hop" by embedding that runtime; it is
// declined on purpose. AG-UI is the protocol every agent backend implements and
// CopilotKit's runtime already translates it, so reimplementing that runtime in
// Go would buy one process hop at the cost of tracking a second, faster-moving
// protocol — and it would move the API token into the browser, which is the one
// place it must not be.
//
// The AG-UI spec revision targeted here is the one documented at
// https://docs.ag-ui.com as of 2026-07-28.
package agui
