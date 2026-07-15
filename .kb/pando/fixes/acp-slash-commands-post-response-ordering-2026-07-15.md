---
created_at: 2026-07-15T17:35:38.272684266Z
updated_at: 2026-07-15T17:35:38.272684266Z
tags:
    - fix
    - acp
    - slash-commands
    - zed
---
# ACP slash commands dropped by Zed — post-response ordering fix

## Symptom
Slash commands stopped working over ACP in Zed (e.g. `/goal`, `/caveman` "not a recognized command", "Available commands for pando: none"). Intermittent. Worked in earlier versions. Relates to prior investigation [[investigate-zed-goal-slash-command-june-2026]] and registry fix [[acp-slash-command-registry-and-input-hints-2026-06-22]].

## Root cause
`available_commands_update` notification raced the `session/new` / `session/load` JSON-RPC response onto the wire.

Pando fired `go a.sendAvailableCommandsUpdate(...)` **before** the request handler returned:
- `internal/mesnada/acp/agent.go` NewSession (was line 292)
- `internal/mesnada/acp/agent.go` LoadSession (was line 511)

That goroutine competes for the SDK connection's single `writeMu` (`acp-go-sdk@v0.15.0/connection.go:617`) against the handler's response write (`connection.go:606`, executed synchronously right after the handler returns in `handleInbound`). When the notification won the race it was delivered before the response, so Zed received commands for a session it had not registered yet and silently dropped them. Non-deterministic → intermittent.

Modern Zed requires the session response first. Confirmed against the canonical wrapper `claude-agent-acp/src/acp-agent.ts` (updated 2026-07-15): it sends commands via `setTimeout(0)` AFTER returning the session — comment "Needs to happen after we return the session" (newSession ~L1298-1301, loadSession ~L1340-1343, and "Send available commands after replay so it doesn't interleave with history"). The Go SDK's own test `TestSendRequest_DoesNotWaitForPostResponseNotification` models the correct path as a post-return goroutine + delay; `TestSendRequest_WaitsForPreResponseNotification` shows a synchronous in-handler send is ordered BEFORE the response (the broken case for Zed).

Wire format itself was already correct: protocol v1, `available_commands_update`, `AvailableCommand{name,description,input.hint}` — identical between Pando and the wrapper.

## Change
`internal/mesnada/acp/session_state.go`
- Added `availableCommandsPostResponseDelay = 50 * time.Millisecond` and `scheduleAvailableCommandsUpdate(sessionID)` helper: spawns a goroutine that sleeps the delay then calls `sendAvailableCommandsUpdate`. Go equivalent of the wrapper's `setTimeout(0)` — the response writes synchronously after the handler returns, so the short pause lets it acquire `writeMu` first. Added `time` import.

`internal/mesnada/acp/agent.go`
- NewSession: `go a.sendAvailableCommandsUpdate(...)` → `a.scheduleAvailableCommandsUpdate(sessionID)` (deferred, post-response).
- LoadSession: replaced the two concurrent goroutines (commands + history, which also interleaved) with a single goroutine that streams history first, then `scheduleAvailableCommandsUpdate` — matches the wrapper's "commands after replay" ordering and keeps the post-response delay.
- `SetConnection` reconnect path (agent.go ~1230) left unchanged: not inside a request handler, so no response race.

## Verification
- `go build ./internal/mesnada/acp/` — OK
- `go test ./internal/mesnada/acp/` — ok (0.449s)