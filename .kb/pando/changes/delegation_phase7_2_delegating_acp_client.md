---
created_at: 2026-06-21T21:28:42.950801538Z
updated_at: 2026-06-21T21:28:42.950801538Z
tags:
    - change
    - mesnada
    - delegation
    - phase7
    - acp
    - projects
    - warm-instance
---
# Change: Delegation Phase 7.2 — Delegating (capturing) ACP client + Manager.Delegate

Implemented 2026-06-21. Status: DONE, verified. Second sub-phase of the Phase 7
re-plan `pando/plans/delegation_phase7_warm_instance_replan.md`. Builds on 7.1
(per-session concurrency hardening). Provides the warm-instance TRANSPORT that
captures a delegated subagent's conclusion over the ACP wire. Not yet wired into
the orchestrator — that routing (reuse-then-autostart, Task synthesis, pipeline
feed) is Phase 7.3.

## Problem
The project `Manager` spawns child `pando acp` processes but its ACP client
(`projectACPClient`) was a no-op that DISCARDS every `SessionUpdate`, and the
connection was never even initialized (no `Initialize` handshake) — it only
tracked process lifecycle. To reuse a warm instance for delegation we need to
open a session, send a prompt, and CAPTURE the agent's streamed output to recover
the `<pando:conclusion>` block, instead of cold-spawning a CLI and scanning stdout.

## What changed
### New capturing client — `internal/project/delegation_client.go`
- `captureSink`: mutex-guarded `strings.Builder` accumulating one session's agent
  text (safe because the ACP SDK dispatches inbound notifications on their own
  goroutine, concurrent with the Delegate caller).
- `delegationClient`: EMBEDS `*projectACPClient` (reuses all no-op lifecycle/file/
  terminal methods + auto-approving `RequestPermission`) and overrides
  `SessionUpdate` to demultiplex `AgentMessageChunk` text to a per-session sink by
  `n.SessionId`. Registry `map[SessionId]*captureSink` guarded by `sync.RWMutex`;
  `register`/`unregister`/`sinkFor`. Unregistered sessions are ignored (preserves
  the previous discard-everything behavior for non-delegated/editor sessions).

### Manager delegation — `internal/project/delegation.go`
- `Manager.Delegate(ctx, projectID, promptText) (*DelegateResult, error)`:
  1. reuse the running manager-owned instance (`ErrInstanceNotRunning` if none —
     auto-start is 7.3's job);
  2. `ensureInitialized` — lazy one-time ACP `Initialize` handshake (`sync.Once` +
     `initErr` on the Instance) since `NewSession`/`Prompt` require it;
  3. `conn.NewSession(Cwd=project.Path, McpServers=[]{})`;
  4. register a capture sink for the new child session id (defer unregister);
  5. `conn.Prompt` synchronously (caller runs it in its own goroutine);
  6. a watcher goroutine mirrors `ctx` cancellation onto the child via
     `conn.Cancel(session/cancel)` so a stopped/aborted delegation never leaves the
     child running its turn (decision 3 of the re-plan);
  7. best-effort `UnstableCloseSession` of the ephemeral child session;
  8. return `DelegateResult{ChildSessionID, Output, StopReason}`. On `ctx` cancel
     it returns the context error so the caller can take the cold-path fallback.
- `DelegateResult` deliberately carries only what the transport knows (child
  session id, captured Output, stop reason). The orchestrator (7.3) wraps it into a
  terminal `models.Task` and fills software-owned launch metadata (model, parent
  session, correlation, depth). Crucially it sets `Output` so the EXISTING pipeline
  `conclusion.Enrich` (which parses `task.Output` then `task.OutputTail`) extracts
  the `<pando:conclusion>` block unchanged — so `internal/project` does NOT import
  the `conclusion` package, and the conclusion-brief is appended by the caller
  (like the orchestrator already does for cold tasks).

### Instance / spawn wiring
- `internal/project/instance.go`: `Instance` gains `delClient *delegationClient`,
  `initOnce sync.Once`, `initErr error`.
- `internal/project/manager.go` `spawnChild`: builds `newDelegationClient(proj)`
  (instead of the no-op `newProjectACPClient`) and stores it on the Instance, so the
  same connection that tracks lifecycle can also capture delegated output.
- `internal/project/errors.go`: new `ErrInstanceNotRunning`.

### Engine constant
- `pkg/mesnada/models/task.go`: new `EngineWarmACP = "warm-acp"` (+ `ValidEngine`)
  to tag tasks that ran inside a warm instance (used by 7.3 when synthesizing the
  Task).

## Verification
- `go build ./...` OK.
- New `internal/project/delegation_internal_test.go` (internal `package project`
  to reach unexported types) — all pass, incl. under `-race`:
  - `TestDelegationClientCapturesRegisteredSessionOnly`: demux + accumulation;
    unregistered session ignored; post-unregister drops.
  - `TestDelegateCapturesConclusionOverWire`: a minimal `acpsdk.Agent` fake paired
    to the client over two `io.Pipe`s (mirroring the SDK's own connection-pairing
    tests) streams a `<pando:conclusion>` block; asserts captured Output, stop
    reason, that Initialize ran and the child session was closed.
  - `TestDelegateNoRunningInstance`: `ErrInstanceNotRunning`.
  - `TestDelegateContextCancelCancelsChildSession`: blocking Prompt + ctx cancel →
    child receives `session/cancel` and Delegate returns an error.
- `go test -race ./internal/project` and `go test ./pkg/mesnada/models` green.

## Next (Phase 7.3)
Warm-target routing: a narrow `WarmTargetResolver` interface injected into the
orchestrator (no import cycle), reuse-then-autostart with a no-activeID
`EnsureInstance` Manager path, `Delegation.ReuseWarmInstances`/`AutoStartWarmInstance`
flags, MaxConcurrent cap, and synthesizing the terminal `models.Task` (engine=
warm-acp) from `DelegateResult` to feed the Phases 1-6 pipeline.
