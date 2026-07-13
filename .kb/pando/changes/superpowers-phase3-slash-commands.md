---
created_at: 2026-07-13T21:25:09.081814314Z
updated_at: 2026-07-13T21:25:09.081814314Z
tags:
    - change
    - superpowers
    - phase3
    - slash-commands
    - acp
    - api
    - tui
---
# Superpowers — Phase 3 implemented: slash commands on every surface

Implements Phase 3 of `pando/plans/superpowers-opt-in-mode-implementation.md`.
Follows Phase 1 (`superpowers-phase1-core-session-mode.md`) and Phase 2
(`superpowers-phase2-prompt-composition.md`). `/superpowers` and `/superpowers-finish` are now
live on TUI, Web UI and ACP.

## Import-cycle correction to Phase 1 (important)
Phase 1 put `EnabledForContext(ctx)` in `internal/superpowers`, resolving the session ID via
`prompt.SessionIDKey` / `tools.SessionIDContextKey`. That only built while nothing else imported
the package. As soon as ACP imported `internal/superpowers` for the slash commands, it produced
a cycle: `superpowers -> llm/tools -> mesnada -> acp -> superpowers`.

Fix: `internal/superpowers` no longer imports `llm/prompt` or `llm/tools` (it now imports only
`strings` + `sync`). Context resolution moved to `internal/llm/agent/superpowers_session.go:superpowersEnabledForContext`,
which already had `sessionIDFromContext`. The context test moved from the core package to the
agent package, where it still covers both context keys. Rule of thumb going forward: the core
policy package must stay dependency-free so any surface can import it.

## What was changed

**Core (`internal/superpowers/superpowers.go`)** — added the shared text so three surfaces never
drift: `FinishPrompt()` (verify → report → state what's NOT done → offer next actions; explicitly
forbids commit/merge/push/PR/branch/worktree/discard/git-config), `ActivationMessage(objective)`,
`AlreadyActiveMessage`, `NotActiveMessage`.

**Success-only clearing, once (`internal/llm/agent/superpowers_session.go`)** —
`RunSuperpowersFinish(ctx, svc, sessionID)` runs the closing prompt as a normal agent turn and
wraps the event channel: it disables the mode **only** after observing a terminal
`AgentEventTypeResponse{Done:true, Error:nil}`. Cancellation, an error event, or a `Run` that
never starts all keep the workflow active. Returns `ErrSuperpowersNotActive` when the mode is off.
Putting this in the agent layer (instead of in each command handler) is what makes ACP, Web UI and
TUI behave identically.

**Shared registry (`internal/commands/registry.go`)** — `superpowers` (AcceptsArgs) and
`superpowers-finish` (no args). Drives TUI/WebUI completion + the API command list.

**ACP** — `session_state.go`: two new tokens. `slash_commands.go`: kinds + specs (activation has
an input hint, finish has none). `goal_commands.go`: two dispatch cases. New
`superpowers_commands.go`: activation is synchronous (no agent turn, idempotent); finish calls
`agentService.SuperpowersFinish` and streams it through `processAgentEventStream`, reporting
"Close cancelled. Superpowers mode stays active." on `StopReasonCancelled`.
`types_interfaces.go`: `SetSuperpowersMode` / `SuperpowersMode` / `SuperpowersFinish` added to the
`AgentService` interface, implemented by both adapters (`cmd/root.go:acpAgentAdapter`,
`internal/app/app.go:appACPAgentAdapter`) via `agent.RunSuperpowersFinish` + `forwardEvents`.

**Web UI API (`internal/api/handlers_chat.go:handleSlashCommandStream`)** — `superpowers` writes a
synchronous SSE confirmation; `superpowers-finish` submits `agent.RunSuperpowersFinish` through
the existing `bgRunner` and streams via `streamSessionEvents`, exactly like `improve-agents-md`.

**TUI (`internal/tui/page/chat.go`)** — `handleSuperpowersCommand` wired into `sendMessage`.
Activation is a toast; finish calls `RunSuperpowersFinish` and drains the returned channel in a
goroutine (the chat renders the turn from the pubsub stream, so the channel is drained only so the
wrapper can observe the terminal event).

## Tests added
- `internal/commands/registry_test.go` (new — the package had none): built-ins present with the
  right `AcceptsArgs`, `Parse` for both commands with/without objective, and `Match("super")`
  returning both (completion must not let finish shadow activation).
- `internal/mesnada/acp/superpowers_commands_test.go` (new): parser cases, both commands
  advertised, activation sets the mode without an agent turn, activation idempotent, finish while
  inactive runs no turn, finish success clears the mode, finish failure retains it.
- `internal/llm/agent/superpowers_session_test.go`: `RunSuperpowersFinish` — inactive session
  returns `ErrSuperpowersNotActive` and runs nothing; success clears; error event retains;
  cancellation retains; `Run` error propagates and retains. Uses a `fakeFinishService` embedding
  `Service` so only `Run` needs implementing (no provider/model/DB).
- `internal/mesnada/acp/agent_pando_test.go`: mock `AgentService` gained the three methods;
  `TestAvailableCommands_ExposeGoalSlashCommands` updated 9 → 11 commands and now asserts the new
  tokens and their input-hint presence/absence.

## Verification
- `go build ./...` — OK
- `go test ./internal/superpowers ./internal/commands ./internal/llm/agent ./internal/mesnada/acp ./internal/api` — all ok
- `go test -race ./internal/superpowers ./internal/mesnada/acp` — ok;
  `go test -race ./internal/llm/agent -run 'Superpowers|SessionPolicy|Ponytail'` — ok
- **Pre-existing failures, confirmed unrelated** by running the same commands in a clean worktree
  at HEAD: 2 data races in `internal/llm/agent` (`goal_runner_test.go` + `session_overrides_concurrency_test.go`/`persona_selector.go`)
  and `internal/mcpgateway` failing with `toml: expected character =`. Both reproduce without any
  Superpowers change.

## Next (Phase 4)
Documentation (README command list + skills docs: opt-in, ephemeral in v1, no automatic git side
effects) and the remaining release-safety checks. Deferred by the plan: durable session-state
persistence, a UI badge for the active mode, and fine-grained workflow stages.
