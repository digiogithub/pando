---
created_at: 2026-07-15T15:29:32.488329045Z
updated_at: 2026-07-15T15:29:32.488329045Z
tags:
    - change
    - learning
    - phase4
    - acp
    - webui
    - tui
    - slash-commands
---
# Learning mode — Phase 4 (slash commands on every surface) COMPLETE

Implements Phase 4 of [[learning-opt-in-mode-implementation]]. Continues
[[learning-phase3-registry]]. Wires `/learning [focus]` and `/learning-finish` on ACP, Web UI and
TUI, in exact parity with the Superpowers commands ([[superpowers-phase3-slash-commands]]).

## ACP
- `session_state.go`: new tokens `learningCommandToken` = "learning", `learningFinishCommandToken`
  = "learning-finish".
- `slash_commands.go`: new kinds `slashCommandLearning` / `slashCommandLearningFinish`; two specs
  (activation has an "optional focus" input hint + usage; finish has none).
- `goal_commands.go` `handleSlashCommand`: two dispatch cases.
- **New `learning_commands.go`**: `processLearningCommand` (synchronous, no agent turn, idempotent
  via `AlreadyActiveMessage`) and `processLearningFinishCommand` (streams `LearningFinish` through
  `processAgentEventStream`; on `StopReasonCancelled` reports "Close cancelled. Learning mode stays
  active.").
- `types_interfaces.go`: `AgentService` gains `SetLearningMode` / `LearningMode` / `LearningFinish`.
- Both adapters implement them via `agent.SetLearningMode` / `agent.LearningMode` /
  `agent.RunLearningFinish` + `forwardEvents`: `cmd/root.go:acpAgentAdapter`,
  `internal/app/app.go:appACPAgentAdapter`.
- **New `learning_commands_test.go`**: parse cases, advertised tokens, activation (enables, no turn),
  idempotency, finish-inactive (no turn), finish-success clears, finish-failure retains.
- `agent_pando_test.go`: `mockAgentService` gained `learningModes` + the three methods;
  `TestAvailableCommands_ExposeGoalSlashCommands` updated 13 -> 15 and asserts the new tokens (learning
  has an input hint, learning-finish does not).

## Web UI
- `internal/api/handlers_chat.go`: added `learning` import and SSE cases `"learning"` (synchronous
  activation / already-active) and `"learning-finish"` (streams `agent.RunLearningFinish` via
  `bgRunner.Submit` + `streamSessionEvents`), mirroring the superpowers cases.

## TUI
- `internal/tui/page/chat.go`: added `learning` import, dispatch call `handleLearningCommand`
  (alongside `handleSuperpowersCommand`), and the handler itself — synchronous toast on activate;
  finish drains `RunLearningFinish` (chat renders the turn from pubsub), `ErrLearningNotActive` ->
  NotActiveMessage.

## Verification
- `go build ./...` — clean.
- `go test ./internal/learning/ ./internal/commands/ ./internal/mesnada/acp/ ./internal/api/` — ok.
- `go test ./internal/llm/agent/ -run 'Learning'` — ok.
- `go vet ./internal/mesnada/acp/ ./internal/api/ ./internal/tui/page/ ./cmd/ ./internal/app/` — clean.

## State after this change
`/learning` and `/learning-finish` are fully functional end-to-end on all three surfaces. The harness
references `kb_mark_outdated`, which does not exist yet — Phase 5 adds that MCP tool. Phase 6 = README
+ feature doc + full-suite verification.