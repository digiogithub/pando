---
created_at: 2026-07-13T20:36:32.378161235Z
updated_at: 2026-07-13T20:36:32.378161235Z
tags:
    - change
    - superpowers
    - phase1
    - skills
    - session-mode
---
# Superpowers — Phase 1 implemented: core session mode and policy

Implements Phase 1 of `pando/plans/superpowers-opt-in-mode-implementation.md`.
Phase 0 (architecture/acceptance spec) is considered covered by that plan document itself.

## What was changed
New package `internal/superpowers` holding the opt-in disciplined-development mode:

- `internal/superpowers/superpowers.go` (new)
  - `type State struct { Enabled bool }` — struct instead of bare bool so later phases can
    carry workflow stages (design-approved / plan-approved / implementing) without changing
    the storage shape.
  - `SetEnabled(sessionID string, enabled bool)` — single mutation point for every surface
    (ACP / Web UI / TUI). Enabling stores; disabling deletes the entry.
  - `Enabled(sessionID string) bool`
  - `EnabledForContext(ctx context.Context) bool` — resolves the session ID from
    `prompt.SessionIDKey` and falls back to `tools.SessionIDContextKey`, the same convention
    as `internal/llm/agent/session_overrides.go:sessionIDFromContext`.
  - `Instructions() string` — the Pando-owned lifecycle policy text.
  - Storage is a package-level `sync.Map` keyed by trimmed session ID, mirroring
    `ponytailModes` in `internal/llm/agent/ponytail_session.go`.
- `internal/superpowers/superpowers_test.go` (new) — enable/disable + idempotence, session-ID
  normalization (whitespace), empty session ID ignored, cross-session isolation, context lookup
  through both context keys, concurrent access, and instruction-content assertions.

## Design decisions taken during implementation
- **No configured default.** Unlike ponytail (which has `PonytailDefaultMode`), Superpowers has
  no config-derived default in v1: presence in the map == enabled. A session that never invokes
  `/superpowers` behaves exactly as before. This keeps `SetEnabled(false)` a plain delete.
- **Session cleanup deferred** (plan Phase 1, item 5): no cheap session-close hook exists, and
  each opted-in session costs one small struct, so entries are documented as process-bounded.
- **Policy content**: 7 numbered gates — understand-before-designing, design+approval,
  explicit prioritized planning for long/multi-file work (risk-and-dependency-ordered phases,
  durable KB plan), test-first small increments, reproduce-then-root-cause for bugs,
  verify-with-evidence, self-review before declaring ready. It opens with an explicit
  PRECEDENCE clause (direct user instruction / AGENTS.md / permissions outrank the policy;
  gates do not apply to read-only or trivial requests) as required by the plan's behavioral
  contract, and it names `/superpowers-finish` as the only off switch.
- No import cycle: `superpowers` imports `internal/llm/prompt` and `internal/llm/tools`; neither
  imports `superpowers`.

## Why
Give Pando an opt-in workflow that forces long/risky tasks through planning and verification
gates before code changes, without vendoring the upstream Superpowers plugin (no telemetry, no
worktrees, no forced subagents, no destructive git operations).

## Verification
- `go build ./internal/superpowers/` — OK
- `go test -race ./internal/superpowers/` — ok (all tests pass, no data races)
- `go vet ./internal/superpowers/` — clean
- `go build ./...` — OK (confirms no dependency cycle across the tree)

## Next
Phase 2: wire `superpowers.EnabledForContext` into `internal/llm/agent/agent.go:prepareProvider`
(fast-path condition + `prompt.InjectSkillInstructions("superpowers", superpowers.Instructions())`),
ordered after automatic skills and before ponytail, with clean mode remaining authoritative.
Phase 3: slash commands `/superpowers` and `/superpowers-finish` in `internal/commands/registry.go`,
`internal/mesnada/acp/slash_commands.go` (+ `SetSuperpowersMode` on the ACP agent service) and
`internal/api/handlers_chat.go:handleSlashCommandStream`.
