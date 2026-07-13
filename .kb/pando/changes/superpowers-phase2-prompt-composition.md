---
created_at: 2026-07-13T20:38:49.782747719Z
updated_at: 2026-07-13T20:38:49.782747719Z
tags:
    - change
    - superpowers
    - phase2
    - prompt
    - agent
---
# Superpowers — Phase 2 implemented: prompt composition

Implements Phase 2 of `pando/plans/superpowers-opt-in-mode-implementation.md`.
Builds on Phase 1 (`pando/changes/superpowers-phase1-core-session-mode.md`).

## What was changed

- `internal/llm/agent/superpowers_session.go` (new) — thin agent-level wrappers over the
  `internal/superpowers` state, mirroring `ponytail_session.go` so every surface talks to the
  agent package in Phase 3:
  - `SetSuperpowersMode(sessionID string, enabled bool)` — the single mutation point for
    ACP / Web UI / TUI.
  - `SuperpowersMode(sessionID string) bool`
  - `superpowersEnabledForContext(ctx) bool`
  The state itself stays in `internal/superpowers` so tools can read it without importing the
  agent package.

- `internal/llm/agent/agent.go:prepareProvider` — refactored the per-turn policy injection:
  - Clean mode still short-circuits first and injects nothing (unchanged, structurally
    guaranteed by the early return).
  - The fast path that reuses the pre-built provider is now gated by a new
    `sessionPolicyActive(ctx)` helper (`ponytail active || superpowers enabled`) instead of the
    ponytail-only check, so an enabled Superpowers session gets a request-scoped provider even
    with no skill manager and no persona.
  - Extracted the ruleset injection into `sessionPolicyInstructions(ctx) []string`, appended
    after any automatically activated skill. Deliberate order: **automatic skills → Superpowers
    → Ponytail** (Superpowers governs *how work is approached*, Ponytail *how much gets built*).
    The old inline ponytail block moved into this helper unchanged.
  - Superpowers is injected as `prompt.InjectSkillInstructions("superpowers", superpowers.Instructions())`.
  - Import added: `internal/superpowers`.

- `internal/llm/agent/superpowers_session_test.go` (new) — set/get, context resolution,
  `sessionPolicyActive` for both policies, injection only when enabled, superpowers-before-ponytail
  ordering when both are on, no leak into other sessions or into a context without a session ID,
  and a concurrent enabled/disabled session check.

## Why
Make the opt-in mode actually reach the model: every eligible turn of an enabled session now
receives the lifecycle policy in its system prompt, while sessions that never invoke
`/superpowers` are byte-for-byte unchanged.

## Design note
Extracting `sessionPolicyInstructions` (a pure ctx→[]string function) is what makes ordering and
isolation testable without constructing a real provider/config; `prepareProvider` itself needs a
full agent+config and has no existing test seam.

## Verification
- `go build ./...` — OK
- `go test -race ./internal/llm/agent -run 'Superpowers|SessionPolicy|Ponytail'` — 9/9 pass
- `go test ./internal/llm/agent ./internal/api ./internal/superpowers` — all ok (project's
  verified command; no regressions)

## Next
Phase 3: register `/superpowers` and `/superpowers-finish` in `internal/commands/registry.go`,
`internal/mesnada/acp/slash_commands.go` (+ `SetSuperpowersMode` on the ACP agent service,
handler analogous to `ponytail_commands.go`) and `internal/api/handlers_chat.go:handleSlashCommandStream`.
Activation is synchronous; finish runs a final agent turn and only clears state on a terminal
success.
