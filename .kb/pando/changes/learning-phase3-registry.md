---
created_at: 2026-07-15T15:20:26.18905353Z
updated_at: 2026-07-15T15:20:26.18905353Z
tags:
    - change
    - learning
    - phase3
    - commands
    - registry
---
# Learning mode — Phase 3 (shared slash registry) COMPLETE

Implements Phase 3 of [[learning-opt-in-mode-implementation]]. Continues
[[learning-phase2-agent-bridge-composition]]. Registers the two commands in the interface-shared
registry so TUI/WebUI completion and `commands.Parse` recognize them.

## What changed
- **`internal/commands/registry.go`** `BuiltinCommands()`: added, right after `superpowers-finish`,
  - `{Name:"learning", AcceptsArgs:true}` — "Enable learner mode: read the KB more, document
    discoveries, ask questions, keep docs current"
  - `{Name:"learning-finish", AcceptsArgs:false}` — "Consolidate what was learned into KB/memory
    and return to normal mode"
- **`internal/commands/registry_test.go`**: new `TestBuiltinCommandsIncludeLearning` (presence +
  AcceptsArgs shape) and three `Parse` cases (`/learning`, `/learning <focus>`, `/learning-finish`).

## Notes
- `Match("super", ...)` still returns 2 (superpowers, superpowers-finish) — learning does not share
  that prefix, so the existing assertion is untouched. No total-count assertion exists in the test.

## Verification
- `go test ./internal/commands/` — ok.

## Next
Phase 4: slash-command handlers on every surface — ACP (`slash_commands.go` kinds/tokens/specs/parser,
new `learning_commands.go`, `types_interfaces.go` AgentService + both adapters in `cmd/root.go` and
`internal/app/app.go`), Web UI SSE (`internal/api/handlers_chat.go`), TUI (`internal/tui/page/chat.go`).