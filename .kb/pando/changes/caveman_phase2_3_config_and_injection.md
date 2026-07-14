---
created_at: 2026-07-14T14:09:41.717377951Z
updated_at: 2026-07-14T14:09:41.717377951Z
tags:
    - change
    - caveman
    - token-optimization
    - phase2
    - phase3
    - config
    - prompt-injection
---
# Caveman output-brevity mode — Phases 2 & 3 COMPLETE

Continues [[caveman_phase1_core_package]]. Implements Phase 2 (durable TOML
config) and Phase 3 (per-turn prompt injection) of
[[caveman-persistent-output-brevity-mode]].

## Phase 2 — TOML configuration and durable update path

### `internal/config/config.go`
- `UpdateCaveman(CavemanConfig) error`, following the existing
  save-and-rollback pattern around `updateCfgFile` (same shape as
  `UpdateTokenOptimization`):
  - normalizes the level before saving (`"ULTRA"` → `"ultra"`, `"off"` → `""`);
  - **rejects** an unsupported level with an error instead of silently coercing
    it to off, so a typo in the TUI/WebUI/API cannot quietly disable (or enable)
    the feature;
  - restores the previous in-memory value when the file write fails.
- (`CavemanConfig` + `(*Config).CavemanDefaultMode()` already landed in Phase 1.)

### `internal/config/init.go`
- New `[Caveman]` section in `DefaultConfigTemplate`, shipping `DefaultMode = ''`
  (off) with the plan's user-facing caveat: savings apply to **output tokens
  only**; input and reasoning tokens are not reduced; code/commands/errors/
  verification stay intact.

### Tests — `internal/config/caveman_test.go`
Persisted TOML write + unrelated-field preservation (`Debug` survives), invalid
level rejected without touching memory or disk, `off` clears the default,
in-memory rollback when persistence fails (unparseable config file), absent
`[Caveman]` section resolves to off, template ships off.

## Phase 3 — Per-turn prompt injection

### `internal/llm/agent/agent.go`
- `sessionPolicyActive(ctx)` now also returns true when
  `cavemanModeForContext(ctx).IsActive()`, so a caveman-enabled session leaves
  the `prepareProvider` fast path that reuses the pre-built provider. This is
  what makes a *config-only* default work with no skill manager and no persona.
- `sessionPolicyInstructions(ctx)` injects
  `prompt.InjectSkillInstructions("caveman ("+mode+")", caveman.Instructions(mode))`.
- **Composition order (deliberate, tested):** automatic skills → **superpowers**
  (how work is approached) → **caveman** (how the result is written up) →
  **ponytail** (how much gets built). Clean mode still returns before any policy
  composition, so caveman is never injected there.

### Tests — `internal/llm/agent/caveman_session_test.go`
Policy-path activation, injected block names the active level and carries the
policy body, injection driven by the configured default, explicit session `off`
suppresses injection *and* the policy path despite a non-off default, clean-mode
invariant, and three-way composition order superpowers → caveman → ponytail.

## Verification
- `go build ./...` — clean.
- `go test ./internal/caveman ./internal/config ./internal/llm/agent ./internal/api` — all ok.
- `go test -race ./internal/llm/agent -run 'Caveman|SessionPolicy'` — ok.

## State after this change
Caveman is now fully functional end-to-end **from configuration**: setting
`[Caveman] DefaultMode = "full"` in `.pando.toml` makes every session without an
override receive the brevity policy each turn. What is still missing is the
*interactive* control surface.

## Next
- Phase 4: `/caveman [lite|full|ultra|wenyan]` + `/caveman-finish` across
  `internal/commands/registry.go`, `internal/api/handlers_chat.go`
  (`handleSlashCommandStream`) and ACP (`slash_commands.go`,
  `types_interfaces.go`, new `caveman_commands.go` modeled on
  `ponytail_commands.go`) — synchronous, no LLM turn.
- Phase 5 TUI settings, Phase 6 WebUI settings + API (`caveman_default_mode`),
  Phase 7 docs.
