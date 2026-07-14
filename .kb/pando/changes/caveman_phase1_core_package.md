---
created_at: 2026-07-14T13:34:15.599677089Z
updated_at: 2026-07-14T13:34:15.599677089Z
tags:
    - change
    - caveman
    - token-optimization
    - phase1
---
# Caveman output-brevity mode — Phase 1 (core package) COMPLETE

Implements Phase 1 of [[caveman-persistent-output-brevity-mode]]. Prior to this
change there was zero `caveman` code in the repository; only the plan existed.

## What was changed

### `internal/caveman/caveman.go` (new)
Dependency-free core package, modeled on [[internal/ponytail]]:
- `type Mode string` + `ModeOff` (""), `ModeLite`, `ModeFull`, `ModeUltra`, `ModeWenyan`.
- `ParseMode(string) (Mode, bool)` — case/space-insensitive, accepts the off
  synonyms `off|stop|normal|none|disable|disabled`; unknown tokens return
  `(ModeOff, false)` so callers can reject instead of silently disabling.
- `Mode.IsActive()`, `Mode.String()` ("off" for the empty mode), `Description(Mode)`.
- `Instructions(Mode)` — Pando's own neutral output policy (upstream caveman is
  NOT vendored): a common core (cut filler/preamble/restatement; keep code,
  commands, paths, errors, test output verbatim; never skip verification,
  evidence or safety warnings; answer in the user's language; a direct request
  for detail overrides brevity) plus a per-level snippet. `wenyan` renders prose
  in Classical Chinese while keeping code/commands/errors verbatim.
  Returns "" when off.

### `internal/llm/agent/caveman_session.go` (new)
Race-safe per-session resolver, mirroring `ponytail_session.go`:
- `SetCavemanMode(sessionID, mode)`, `CavemanMode(sessionID)`,
  `cavemanModeForContext(ctx)` (will be consumed by `prepareProvider` in Phase 3).
- `sync.Map` override store. Presence semantics: an explicit session "off" is
  *stored* when a non-off global default is configured (so it wins over the
  default) and *deleted* when the default is already off.
- Falls back to `config.CavemanDefaultMode()`; unknown values resolve to off.

### `internal/config/config.go`
- New `CavemanConfig{DefaultMode string}` + `Caveman` field on `Config`
  (`toml:"Caveman"` / `json:"caveman"`).
- `(*Config).CavemanDefaultMode() string` normalizing only `lite|full|ultra|wenyan`,
  nil-safe. No env override (per plan decision). This is the minimal slice of
  Phase 2 needed for Phase 1 to compile; the durable `UpdateCaveman` write path,
  the `.pando.toml` template section and the rollback tests remain Phase 2.

## Reason
Reduce output-token consumption by constraining expression only — not reasoning,
tool use, testing or verification. Default OFF; no behavior change for existing
installs until the setting or slash command is used.

## Verification
- `go build ./...` — clean.
- `go test ./internal/caveman ./internal/config` — ok.
- `go test -race ./internal/llm/agent -run Caveman` — ok (covers session
  isolation under concurrency, global-default fallback, explicit-off-over-default,
  invalid default → off, empty session id, ctx lookup).

## Next
Phase 2 (durable TOML update path + template), Phase 3 (per-turn injection in
`agent.prepareProvider`, ordering: skills → superpowers → caveman → ponytail),
Phase 4 (`/caveman`, `/caveman-finish` across registry/API/ACP), Phases 5-7.
