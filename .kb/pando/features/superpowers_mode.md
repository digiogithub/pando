---
created_at: 2026-07-13T21:44:08.349179296Z
updated_at: 2026-07-13T21:44:08.349179296Z
tags:
    - feature
    - superpowers
    - slash-commands
    - complete
    - documentation
---
# Superpowers mode — feature reference (COMPLETE, 2026-07-13)

Opt-in per-session workflow discipline, enabled with `/superpowers [objective]` and closed with
`/superpowers-finish`. Implements `pando/plans/superpowers-opt-in-mode-implementation.md` in full
(Phases 0-4). Inspired by the workflow principles of https://github.com/obra/superpowers (MIT),
reimplemented natively: no plugin runtime, no telemetry, no worktrees, no forced subagents.

## Behavior
- `/superpowers` — synchronous, no LLM turn, idempotent. Injects the lifecycle policy into every
  subsequent turn of that session: understand → design + approval → prioritized written plan for
  multi-file work (phases ordered by risk/dependency, exit criteria + verification command each) →
  test-first small increments → reproduce-before-fixing bugs → verify with real output → self-review.
- `/superpowers-finish` — a real agent turn: verify, summarize what changed, state what is NOT done,
  offer next actions. Disables the mode **only** on a successful terminal response; cancelled/failed
  closes keep the workflow active.
- Precedence: direct user instructions, AGENTS.md and the permission system outrank the policy; the
  gates do not apply to trivial/read-only requests.
- Guarantees: never commits, merges, pushes, opens PRs, touches branches/worktrees, discards work or
  changes git config. Clean mode is unaffected (it short-circuits before any injection).
- Ephemeral in v1: in-memory, per-session, does not survive a restart.

## Architecture (where things live)
- `internal/superpowers/superpowers.go` — the whole policy: `State`, `SetEnabled`, `Enabled`,
  `Instructions()`, `FinishPrompt()`, `ActivationMessage()`, `AlreadyActiveMessage`, `NotActiveMessage`.
  **Must stay dependency-free** (only `strings`+`sync`): it is imported by ACP, and importing
  `llm/tools` from here creates the cycle `superpowers → llm/tools → mesnada → acp → superpowers`.
- `internal/llm/agent/superpowers_session.go` — surface-facing wrappers (`SetSuperpowersMode`,
  `SuperpowersMode`), context resolution (`superpowersEnabledForContext`, using the agent's own
  `sessionIDFromContext`), and `RunSuperpowersFinish` — the single place implementing the
  success-only clearing rule, so ACP/WebUI/TUI cannot diverge.
- `internal/llm/agent/agent.go` — `prepareProvider` fast path gated by `sessionPolicyActive(ctx)`;
  `sessionPolicyInstructions(ctx)` injects, in order: automatic skills → Superpowers → Ponytail.
- Surfaces: `internal/commands/registry.go` (shared registry / completion),
  `internal/mesnada/acp/{slash_commands,superpowers_commands,goal_commands,types_interfaces,session_state}.go`
  + adapters in `cmd/root.go` and `internal/app/app.go`,
  `internal/api/handlers_chat.go:handleSlashCommandStream` (SSE + bgRunner),
  `internal/tui/page/chat.go:handleSuperpowersCommand`.

## Phase 4 (this change): verification + documentation
- README: new **Built-in Slash Commands** section (the repo had none — `/ponytail`, `/goal`,
  `/compact`, `/db-compact`, `/improve-agents-md` were undocumented too) with a Superpowers
  subsection covering the four caveats: opt-in/inert by default, user instructions win, no automatic
  git side effects, ephemeral in v1. Plus a Features bullet linking to it.
- No config keys and no i18n strings were needed (the mode has no configurable default by design).

## Verification (full Phase 4 gate)
- `go build ./...` — OK; `go vet` on all touched packages — clean.
- `go test ./internal/superpowers ./internal/commands ./internal/llm/agent ./internal/mesnada/acp ./internal/api` — all ok.
- `go test -race`: `./internal/superpowers`, `./internal/mesnada/acp` ok;
  `./internal/llm/agent -run 'Superpowers|SessionPolicy|Ponytail'` ok.
- `go test ./...` — only `internal/mcpgateway` fails (3 tests, `toml: expected character =`), and the
  2 data races in `internal/llm/agent` (`goal_runner_test.go`, `session_overrides_concurrency_test.go`
  / `persona_selector.go`) are **pre-existing**: both reproduce at HEAD in a clean worktree with no
  Superpowers change. Not introduced here, not fixed here.

## Deferred (from the plan)
Durable session-state persistence across restarts, a UI badge/status for the active mode,
fine-grained workflow stages (design-approved / plan-approved / implementing) in structured session
data, project-configurable policy templates, and unifying the duplicated native/ACP slash-command
specs.

## Repo caution learned here
`.kb/` documents are versioned in git and rewritten by KB sync; `git stash` on this repo can fail to
pop cleanly because of them. Use a separate `git worktree` to compare against a baseline.
