---
created_at: 2026-07-13T07:05:01.898807806Z
updated_at: 2026-07-13T21:44:51.211167466Z
tags:
    - plan
    - superpowers
    - slash-commands
    - skills
    - architecture
    - complete
---
# Superpowers Opt-in Session Mode Implementation Plan

## STATUS: COMPLETE (Phases 0-4, 2026-07-13)
Shipped. Feature reference: `pando/features/superpowers_mode.md`. Per-phase records:
`pando/changes/superpowers-phase1-core-session-mode.md`, `…-phase2-prompt-composition.md`,
`…-phase3-slash-commands.md`.

**One deviation from this plan, deliberate:** Phase 1 specified `EnabledForContext(ctx)` inside
`internal/superpowers`, resolving the session ID with `prompt.SessionIDKey` / the tools context key.
That creates an import cycle as soon as ACP imports the package for the slash commands
(`superpowers → llm/tools → mesnada → acp → superpowers`). The core package is therefore
dependency-free (`strings` + `sync` only) and the context resolution lives in
`internal/llm/agent/superpowers_session.go:superpowersEnabledForContext`, reusing the agent's
existing `sessionIDFromContext`. The plan's "no dependency cycle" exit criterion is met; its exact
API shape is not. Keep the core package import-free.

**Additions beyond the plan:** `RunSuperpowersFinish` in the agent package centralizes the
success-only clearing rule so ACP, Web UI and TUI cannot diverge; the TUI (not listed in Phase 3)
was also wired, since it would otherwise send `/superpowers` to the model as literal text.

## Objective
Add an internal, opt-in Pando workflow mode enabled by `/superpowers` and disabled by `/superpowers-finish`. It must make a disciplined development lifecycle available without changing normal Pando behavior or importing/executing the upstream Superpowers plugin.

## Research and decisions
- Upstream source reviewed: https://github.com/obra/superpowers (MIT, v6.1.1 at research time). Its essential model is a bootstrap policy plus composable skills: explore/design, plan, TDD, systematic verification/review, and a structured finish flow.
- Pando already has a native SKILL.md discovery, lazy instruction loading, prompt injection, and per-session mode precedent in `internal/llm/agent/ponytail_session.go`.
- Pando exposes slash commands separately through Web/TUI shared registry and ACP; current entries are duplicated between `internal/commands/registry.go` and `internal/mesnada/acp/slash_commands.go`.
- Decision: implement a Pando-owned policy mode, not a vendored copy of upstream skills. It has no telemetry, no automatic worktrees/subagents, and no destructive git operation. Existing user/project skills remain available and compose normally.
- Decision: mode state is session-scoped, concurrent-safe, and ephemeral in the first release, matching Ponytail. It is disabled on process restart and when `/superpowers-finish` completes. Persisting it across restarts is deliberately deferred until session metadata has a general extension mechanism.
- Decision: `/superpowers-finish` is a safe close-and-report command. It asks the agent to verify work and summarize outstanding risks, then disables the policy. Git integration decisions remain user-directed through normal tools.

## Behavioral contract
### /superpowers [optional objective]
- Activates the policy immediately without consuming an LLM turn; optional objective is retained only in the confirmation text, not persisted.
- Returns a concise confirmation stating that normal work remains unchanged until enabled, then directs the next request through: clarify/design -> approved plan -> test-first implementation -> verification/review -> explicit finish.
- Re-invoking is idempotent and reports that the mode remains active.

### Active mode
- Before implementation or behavior changes, the system prompt requires the agent to inspect context, identify requirements/constraints, present alternatives and a bounded design, and obtain user approval before code changes.
- After approval, it requires an actionable plan with exact affected areas and verification commands.
- During implementation, it requires focused increments, tests before behavior changes where feasible, and evidence from relevant tests/builds rather than unsupported completion claims.
- For bugs, it requires reproducible evidence and root-cause analysis before modifying behavior.
- It requires a review/checkpoint before claiming work is ready.
- It must respect stronger direct user instructions, AGENTS.md, permissions, and existing Pando policies. It must not force clarification for simple read-only requests or block emergency user-directed fixes; it should state the deviation and preserve verification.

### /superpowers-finish
- If inactive, responds that normal mode is already active.
- If active, executes a final normal agent turn with a finish prompt: inspect git status, run the narrowest relevant verification (or explicitly report why it cannot), summarize changes, tests, remaining risks, and offer non-destructive next actions.
- It clears mode state only after the final turn reaches a terminal result. On cancellation/error, retain the mode so the workflow is not silently abandoned.
- It never merges, pushes, creates a PR, discards work, removes a worktree, or modifies git configuration.

## Phases

### Phase 0: Architecture and acceptance specification — DONE
Covered by this document (behavioral contract + acceptance cases + reserved built-in names).

### Phase 1: Core session mode and policy — DONE
`internal/superpowers/superpowers.go` + tests. See the deviation note above regarding
`EnabledForContext`. Session cleanup deferred (no cheap session-close hook): entries are
process-bounded, one small struct per opted-in session.

### Phase 2: Prompt composition — DONE
`prepareProvider` fast path gated by `sessionPolicyActive(ctx)`; `sessionPolicyInstructions(ctx)`
injects automatic skills → Superpowers → Ponytail. Clean mode still short-circuits first and injects
nothing.

### Phase 3: Slash command wiring on every surface — DONE
Shared registry, ACP (specs/parser/handler/`AgentService` + both adapters), Web UI SSE handler, TUI.
Success-only clearing centralized in `agent.RunSuperpowersFinish`.

### Phase 4: Verification, documentation, and release safety — DONE
Tests: `internal/commands/registry_test.go` (new), `internal/mesnada/acp/superpowers_commands_test.go`
(new), `RunSuperpowersFinish` cases in `internal/llm/agent/superpowers_session_test.go`, advertised-command
count 9→11 in `agent_pando_test.go`. Docs: README gained a **Built-in Slash Commands** section (none
existed) plus a Superpowers subsection with the opt-in / precedence / no-git-side-effects / ephemeral
caveats, and a Features bullet.

Verified: `go build ./...`, `go vet` clean, targeted tests pass, `-race` clean on the new code.
`go test ./...` shows only pre-existing failures (`internal/mcpgateway` TOML tests; 2 data races in
`internal/llm/agent` from `goal_runner_test.go` and `session_overrides_concurrency_test.go`/`persona_selector.go`),
all reproduced at HEAD in a clean worktree without any Superpowers change.

## Risks and mitigations
- Prompt bloat: keep policy compact and inject only for enabled sessions.
- Duplicate command registries: phase 3 must add parity tests; a later refactor may unify specs, but is out of scope for this change.
- Overriding user intent: explicit precedence language and no automatic destructive/branch operations.
- Finish falsely claims verification: require command output evidence or an explicit unable-to-run explanation in the final report.
- Feature creep from upstream: use its workflow principles, not its plugin runtime, visual companion, telemetry, worktree, or forced subagent machinery.

## Deferred follow-ups
- Durable session-state persistence and UI badge/status for Superpowers mode.
- Fine-grained workflow states (design-approved, plan-approved, implementing) enforced by structured session data.
- Optional project-configurable policy templates and organization-specific gates.
- Unified slash-command specification shared by native and ACP command surfaces.
- Pre-existing debt surfaced during this work (not caused by it): the two `internal/llm/agent` data
  races and the `internal/mcpgateway` TOML test failures.
