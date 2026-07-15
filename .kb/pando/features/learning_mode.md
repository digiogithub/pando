---
created_at: 2026-07-15T17:20:17.812947973Z
updated_at: 2026-07-15T17:20:17.812947973Z
tags:
    - feature
    - learning
    - slash-commands
    - complete
    - documentation
---
# Learning mode — feature reference (COMPLETE, 2026-07-15)

Opt-in per-session knowledge-capture policy, enabled with `/learning [focus]` and closed with
`/learning-finish`. Implements [[learning-opt-in-mode-implementation]] in full (Phases 0-6).
Modeled on [[superpowers_mode]]: same dependency-free core + agent-bridge + three-surface pattern.

## Behavior
- `/learning [focus]` — synchronous, no LLM turn, idempotent. Optional free-text focus steers what
  to learn. Injects the learner/documentarian policy into every subsequent turn of that session:
  1. **Recover context first** — search the KB (`kb_search_documents`, `hybrid_search_remembrances`)
     and read relevant memories (`recall`) before building on prior work, instead of re-deriving.
  2. **Ask what matters** — use `AskUserQuestion` for decisions that are genuinely the user's, rather
     than guessing.
  3. **Capture discoveries** — non-obvious findings via `kb_add_document`; short durable facts via
     `remember`.
  4. **Keep docs honest** — mark superseded plans/features/fixes outdated with `kb_mark_outdated` and
     add the up-to-date doc, instead of leaving contradictory docs behind.
  5. Documentation depth is independent from output brevity — composes with `/caveman`.
- `/learning-finish` — a real agent turn: consolidate what was learned into the KB/memory, then return
  to normal. Disables the mode **only** on a successful terminal response; cancelled/failed closes
  keep learning active. No git side effects (the finish prompt explicitly says "do NOT commit").
- Precedence: direct user instructions, AGENTS.md and the permission system outrank the policy.
- Ephemeral: in-memory, per-session, does not survive a restart.

## Architecture (where things live)
- `internal/learning/learning.go` — the whole policy: `State`, `SetEnabled`, `Enabled`,
  `Instructions()`, `FinishPrompt()`, `ActivationMessage()`, `AlreadyActiveMessage`, `NotActiveMessage`.
  **Dependency-free** (only `strings`+`sync`), same reason as superpowers: it is imported by ACP, so
  importing `llm/tools` here would create the cycle `learning → llm/tools → mesnada → acp → learning`.
- `internal/llm/agent/learning_session.go` — surface-facing wrappers (`SetLearningMode`,
  `LearningMode`), context resolution (`learningEnabledForContext` via `sessionIDFromContext`), and
  `RunLearningFinish` — the single place implementing the success-only clearing rule.
- `internal/llm/agent/agent.go` — `sessionPolicyActive(ctx)` ORs learning; `sessionPolicyInstructions`
  injects in order: automatic skills → Superpowers → **Learning** → Caveman → Ponytail.
- Surfaces: `internal/commands/registry.go` (shared registry / completion),
  `internal/mesnada/acp/{slash_commands,learning_commands,goal_commands,types_interfaces,session_state}.go`
  + adapters in `cmd/root.go` and `internal/app/app.go`,
  `internal/api/handlers_chat.go` (SSE `learning` / `learning-finish` via bgRunner),
  `internal/tui/page/chat.go:handleLearningCommand`.

## The kb_mark_outdated tool (Phase 5)
The harness names `kb_mark_outdated`; before this work no MCP tool exposed
`KBStore.MarkDocumentOutdated` (only the memory GC called it). Added `KBMarkOutdatedTool` in
`internal/llm/tools/remembrances_kb.go` (input `file_path`, checks existence first because the
underlying UPDATE silently no-ops on a missing doc, idempotent), registered in both
`internal/llm/agent/tools.go` and `internal/mesnada/server/tools.go`. The `outdated` flag is already
honored by `kb_search_documents`'s `exclude_outdated` (default true), closing the read/write loop.

## Phase 6 (this change): verification + documentation
- README: two new **Built-in Slash Commands** rows (`/learning`, `/learning-finish`), a Features
  bullet, and a "Learning mode (opt-in)" subsection covering recover/ask/document/keep-honest plus the
  caveats (opt-in, depth-not-verbosity/composes-with-caveman, user instructions win, no git side
  effects, ephemeral).
- No config keys and no i18n strings needed (the mode has no configurable default by design).

## Verification (full gate)
- `go build ./...` — OK; `go vet` on all touched packages — clean.
- `go test ./internal/learning ./internal/commands ./internal/llm/tools ./internal/mesnada/acp ./internal/api` — all ok.
- `go test ./internal/llm/agent -run 'Learning|SessionPolicy|Superpowers|Ponytail'` — ok.
- `go test -race`: `./internal/learning`, `./internal/llm/tools -run KBMarkOutdated`,
  `./internal/mesnada/acp -run Learning`, `./internal/llm/agent -run Learning` — all ok.
- Pre-existing HEAD failures unchanged and unrelated: `internal/mcpgateway` TOML tests + 2 data races
  in `internal/llm/agent` (`goal_runner_test.go`, `session_overrides_concurrency_test.go`).

## Deferred (same as superpowers)
Durable session-state persistence across restarts, a UI badge for the active mode, and unifying the
duplicated native/ACP slash-command specs.
