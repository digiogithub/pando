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
//   - P8 (PANDO-EP-0003): the native thread API — GET {path}/threads (paginated,
//     newest-first, scoped to the agui_threads table this adapter owns), GET
//     {path}/threads/{id}/messages and DELETE {path}/threads/{id} — replaces the
//     interim workaround of proxying the Web-UI REST API's session endpoints, so
//     a browser client can rebuild a conversation without co-mounting it (see
//     threads.go). The AG-UI Message[] conversion it needed (transcript.go) is
//     reused by MESSAGES_SNAPSHOT: the first run this process serves for a
//     pre-existing thread's attach resynchronises the client in-band, right
//     after STATE_SNAPSHOT, capped to a configured message count/byte budget and
//     flagged truncated when it is (server.go's runPrelude).
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
//
// # Run lifetime and durability (PANDO-EP-0003)
//
//   - PANDO-US-0017: a browser disconnecting mid-run no longer cancels it. The
//     run's underlying agent event stream is drained by one long-lived
//     goroutine, pump (run.go), started once per run and independent of any
//     particular HTTP request; a disconnect merely detaches the request and,
//     once the last attach is gone, arms Config.DisconnectGrace before
//     tearing the run down. Every event the pump produces is also appended
//     to a bounded ring (eventBuffer) for a later reattach to replay; once
//     full it drops the oldest event and marks itself lossy rather than
//     growing.
//
//   - PANDO-US-0018: GET {path}/threads/{id}/stream (and a POST carrying no
//     new user message) reattaches to a thread's live run: subscribe, replay
//     the buffer, then continue live — any number of attaches (the original
//     stream, a reattach, read-only followers) may be registered on one run
//     at once. Only the pump ever touches a run's translator, which is what
//     keeps translate.go's single-writer, stateful design safe under
//     concurrent attaches; a resumption after an interrupt installs a new
//     translator (Runtime.beginResumeSegment) strictly before delivering the
//     tool result that would otherwise let the pump observe a new segment's
//     event through the old, already-closed-out one. handleRun's
//     decide-whether-to-resume/reject/start-a-run section is serialized per
//     thread (runStore.lockThread), closing the TOCTOU where two POSTs could
//     both see no live run and both race into svc.Run.
//
//   - PANDO-US-0019: POST {path}/runs/{id}/cancel ends a thread's run,
//     live or parked, from any request — not only the one streaming it.
//     pendingRegistry.cancelAll force-delivers a cancellation to every call
//     still waiting on the client, releasing a permission/question wait
//     (which selects on the adapter's base context, not the run's) that a
//     mere context cancellation would not reach. Cancellation, like a natural
//     finish, is applied by the pump (activeRun.requestCancel wakes it via a
//     dedicated signal channel) so RUN_ERROR{code:"cancelled"} reaches every
//     attached stream through the one translator, never raced against it
//     from the HTTP handler's own goroutine.
//
// # Operability for embedded deployments (PANDO-EP-0004)
//
//   - PANDO-US-0020: GET {path}/healthz is the one route Register mounts
//     outside authorize() — see server.go's handleHealthz. It answers 200
//     with status, version, uptime and (below) the concurrency gauge and
//     draining flag, and nothing else: no agent/profile list, no origins, no
//     token, no session or thread identifier.
//
//   - PANDO-US-0021: Config.MaxConcurrentRuns caps runs the adapter admits,
//     enforced by runAdmission (admission.go) before any session, agent
//     instance or thread binding is created. A slot is held for a run's
//     whole lifetime, including while suspended waiting on a client — a
//     parked run still occupies one — and released exactly once, by
//     Runtime.finishRun, guarded by activeRun.stop()'s once-only return so a
//     run finalized from more than one path never double-releases. A
//     resumption of an already-admitted run never calls tryAdmit again. Over
//     the cap, handleRun answers 503 + Retry-After through
//     Runtime.rejectOverCapacity before opening any stream.
//
//   - PANDO-US-0022: Config.ShutdownGrace bounds how long Runtime.Close waits
//     for in-flight runs to finish before falling back to the hard cancel it
//     always did for whatever is left, logging the cut count. Close begins
//     by draining (Runtime.StartDraining), so handleRun rejects every new
//     run through the same PANDO-US-0021 path for the rest of shutdown. A
//     suspended run is never waited on — nobody is going to answer a
//     human-in-the-loop prompt inside a shutdown window — so it is released
//     immediately, cancelAll'd first so a hitl.go wait (which selects on the
//     adapter's base context, not the run's) is actually unblocked.
//     cmd/agui_serve.go orders listener.Shutdown before the deferred
//     Runtime.Close, both bounded by the same configured grace.
//
//   - PANDO-US-0023: agui-serve resolves its bearer token in precedence
//     --token > --token-file > PANDO_AGUI_TOKEN > generated
//     (cmd/agui_serve.go's resolveAGUIToken), rejecting an explicitly
//     supplied-but-empty source as a startup error rather than falling
//     through. The token is never logged at any level, including the
//     startup config dump (New's "AG-UI adapter ready" line never carries
//     Deps.Token), and is printed to stdout only in the generated case —
//     the only one the operator has no other way to learn it.
//
// # Reverse-proxy contract (PANDO-US-0024)
//
// Everything a product's own backend needs to know to sit between a browser
// and this adapter. Each fact is pinned to the line that enforces it today,
// verified against the source at the time this section was written — if a
// future refactor moves these lines, update the citations, not just the
// prose.
//
//   - Origin: authorize (server.go:71-95) skips the AllowedOrigins check
//     entirely when the Origin header is absent (server.go:72-73: "origin
//     != \"\" && !r.originAllowed(origin)") — a server-to-server proxy that
//     does not forward the browser's own Origin needs no
//     Config.AllowedOrigins entry at all, and that is the recommended
//     shape: the browser authenticates to the proxy, the proxy talks to
//     Pando, and Pando never sees a browser Origin to check.
//     agui-serve's startup warning "AG-UI server has no allowed origins"
//     (cmd/agui_serve.go:125-127) is therefore correct to ignore in this
//     deployment shape — it exists to warn about the OTHER shape, a
//     browser reaching this adapter directly. If a proxy instead forwards
//     the browser's Origin verbatim, the exact string must be listed:
//     originAllowed (server.go:109-116) is exact, case-insensitive match
//     (strings.EqualFold) or the literal "*", with no wildcard subdomain or
//     port pattern support.
//
//   - Token: bearerToken (server.go:97-103) accepts "Authorization: Bearer
//     <token>" first, falling back to a "?token=" query parameter. The
//     fallback exists only because the browser's native EventSource API
//     cannot set request headers, and it must never be used from a
//     browser-originated request: a query string is captured in access
//     logs, in the Referer header of any same-page navigation, and in
//     browser history. A proxy in front of this adapter should strip any
//     inbound "?token=" and set the real "Authorization" header itself,
//     keeping the Pando token entirely server-side — the browser
//     authenticates to the PROXY under whatever scheme the product already
//     uses, and never learns Pando's own token. A RequireToken deployment
//     with no Deps.Token configured fails closed with 500 rather than
//     silently degrading into an open endpoint (server.go:83-88).
//
//   - Streaming: NewSSEWriter (sse.go:33-46) sets "X-Accel-Buffering: no"
//     (sse.go:43) and flushes after every event (Write/Comment both call
//     flusher.Flush(), sse.go:63,88) plus a ": keep-alive" comment every
//     15s of otherwise-silent heartbeat (defaultHeartbeat, deps.go:162;
//     ticker armed in attachLoop, server.go:790-791; emitted at
//     server.go:801-803 — an SSE comment, invisible to a client parsing
//     "data:" frames). A Go httputil.ReverseProxy therefore needs
//     FlushInterval: -1 (flush after every write, never batch), and any
//     intermediary must not buffer the response body at all. The dedicated
//     listener sets WriteTimeout: 0 on purpose, because a run's response is
//     exactly as long-lived as the agent takes (listener.go:88-92); a
//     proxy's own write/idle timeout must be 0 or comfortably above the 15s
//     heartbeat — below it, a slow tool call reads as a dead connection and
//     the intermediary cuts the stream.
//
//   - /info URL rewriting: requestBaseURL (server.go:346-358) builds every
//     Agents[].url from req.Host, honouring X-Forwarded-Proto for the
//     scheme ONLY when the request did not already arrive over TLS, and
//     deliberately never reads X-Forwarded-Host (an attacker-controlled
//     value there would let /info hand out URLs pointing at somebody
//     else's server). Behind a proxy that rewrites the request path (e.g.
//     strips a "/pando" prefix) those URLs come back wrong to use as-is:
//     either rewrite the Host header upstream to the public host the
//     browser actually used, or ignore /info's URLs entirely and construct
//     the run endpoint yourself from the proxy's own configured origin plus
//     the path /info reports.
//
//   - TLS: agui-serve self-signs a certificate into the data directory
//     unless --no-tls is given (cmd/agui_serve.go:144-157); --tls-cert /
//     --tls-key substitute a certificate of your own. On loopback behind a
//     proxy that already terminates TLS for the browser, --no-tls is the
//     pragmatic choice for the Pando-facing hop; across any other network
//     boundary, pin the certificate instead of disabling TLS.
//
//   - Body limit: a RunAgentInput body over defaultMaxRequestBytes (8 MiB,
//     sse.go:13) is truncated by the io.LimitReader DecodeRunAgentInput
//     wraps the request body in (input.go:169-182) and then fails to
//     decode — a proxy must not impose a tighter body-size limit of its own
//     without raising it to match, or a legitimate long conversation's
//     resent transcript can be cut off before Pando ever sees it.
//
// A copy-pasteable Go proxy implementing all of the above (newReverseProxy)
// lives at examples/vite-react/proxy/main.go, compiled by
// examples/vite-react/proxy/example_test.go so it cannot silently stop
// building; the same snippet is mirrored in sdk/typescript/README.md for SDK
// consumers who never clone this repository. examples/vite-react/ is the
// worked SPA client for it — see PANDO-US-0024's story for scope.
package agui
