---
created_at: 2026-07-13T07:05:01.528353977Z
updated_at: 2026-07-13T07:05:01.528353977Z
tags:
    - plan
    - superpowers
    - slash-commands
    - skills
    - architecture
---
# Superpowers Opt-in Session Mode Implementation Plan

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

### Phase 0: Architecture and acceptance specification
1. Add a focused design note under project documentation covering the behavioral contract above, precedence with AGENTS.md/direct instructions, and the non-goals.
2. Define testable acceptance cases for activation, idempotence, active prompt injection, finish success, finish cancellation/error, concurrent sessions, and clean mode.
3. Confirm that no user-authored files conflict with the names `superpowers` and `superpowers-finish`; reserve these names as built-ins.

Exit criteria: reviewed behavior matrix and a clear no-destructive-automation guarantee.

### Phase 1: Core session mode and policy
1. Create `internal/superpowers/` with a small mode API:
   - `type State struct { Enabled bool }`
   - `SetEnabled(sessionID string, enabled bool)`
   - `Enabled(sessionID string) bool`
   - `EnabledForContext(ctx context.Context) bool`
   - `Instructions() string`
2. Use a `sync.Map` keyed by normalized session ID and the same `prompt.SessionIDKey` / tool context resolution convention as `internal/llm/agent/session_overrides.go`.
3. Encode the Pando-owned lifecycle policy in one concise instruction source. Keep it prescriptive about gates and verification, not a long copy of upstream prose.
4. Add unit tests in `internal/superpowers/` for normalization, enable/disable, context lookup, isolation, and parallel safety.
5. Decide whether closed/deleted session cleanup can be added cheaply. If no lifecycle hook is available without broad refactoring, document that map entries are process-bounded and defer cleanup.

Exit criteria: isolated core package with race-safe unit coverage and no dependency cycle.

### Phase 2: Prompt composition
1. Update `internal/llm/agent/agent.go:prepareProvider` so the fast path also considers Superpowers mode.
2. When enabled, append `prompt.InjectSkillInstructions("superpowers", superpowers.Instructions())` for every eligible agent turn, alongside automatically activated skills and Ponytail.
3. Preserve clean-mode behavior: clean mode remains authoritative and must not inject the policy.
4. Define ordering intentionally: user/project active skills first, Superpowers lifecycle policy next, Ponytail last; document why direct user/AGENTS instructions still take precedence.
5. Add agent-level tests proving injection occurs only for the enabled session, composes with Ponytail and ordinary skills, is absent when disabled, and cannot leak across concurrent contexts.

Exit criteria: every normal agent turn for the enabled session receives the policy, with existing modes unchanged.

### Phase 3: Slash command wiring on every surface
1. Register both commands in `internal/commands/registry.go`:
   - `superpowers`: “Enable the opt-in disciplined development workflow”
   - `superpowers-finish`: “Verify and close the active Superpowers workflow”
2. Add equivalent ACP kinds/specs to `internal/mesnada/acp/slash_commands.go`, including availability metadata and parser coverage.
3. Extend the ACP agent-service interface and its concrete adapter with a minimal `SetSuperpowersMode(sessionID, enabled)` operation, following `SetPonytailMode`.
4. Add a small ACP command handler analogous to `ponytail_commands.go`. Activation is synchronous; finish routes a dedicated final prompt through the existing agent execution path and clears state only after success.
5. Extend `internal/api/handlers_chat.go:handleSlashCommandStream`: activation is synchronous SSE feedback; finish submits the final agent prompt through the existing background runner and applies the same success-only clearing rule.
6. Ensure unknown/custom command fallback behavior remains unchanged and command completion/API lists include both built-ins.

Exit criteria: Web UI, TUI completion/API registry, and ACP show and execute the same commands.

### Phase 4: Verification, documentation, and release safety
1. Add unit tests for shared command registry parsing/listing; add SSE/API handler tests as appropriate to the existing test seams.
2. Extend `internal/mesnada/acp/agent_pando_test.go` for advertised commands, parser cases, activation, idempotence, finish success, and finish failure/cancellation retention.
3. Run `go test ./internal/llm/agent ./internal/api`, targeted `go test ./internal/mesnada/acp ./internal/commands ./internal/superpowers`, then `go test ./...` if the repository baseline permits.
4. Run `go test -race` on the mode package and the agent tests that exercise session isolation.
5. Update README command documentation and skill documentation to explain that the feature is opt-in, ephemeral in v1, and intentionally excludes automatic git side effects.
6. Record the completed implementation and verification in the Pando KB as required by AGENTS.md.

Exit criteria: all targeted tests pass, commands are documented, and defaults remain behaviorally identical for users who never invoke `/superpowers`.

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
