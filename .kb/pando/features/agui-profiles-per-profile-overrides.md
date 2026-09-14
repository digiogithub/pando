---
created_at: 2026-09-14T16:18:43.422604731Z
updated_at: 2026-09-14T16:18:43.422604731Z
tags:
    - feature
    - agui
    - config
---
# PANDO-EP-0002: AG-UI named agent profiles (US-0012/0013/0014)

Status: implemented 2026-09-14. Builds on [[pando/features/agui-tools-glob-allowlist-mesnada-switch.md]] (PANDO-US-0011).

## What changed

### PANDO-US-0012 — `[AGUI.Profiles.<name>]` config schema and validation
- `internal/config/config.go`: added `AGUIConfig.Profiles map[string]AGUIProfile`
  and the new `AGUIProfile{Base AgentName, Model models.ModelID, Persona, Prompt
  string, Tools, DenyTools *[]string, Mesnada *bool}` struct, right after
  `AGUIConfig`.
  - `Tools`/`DenyTools` are `*[]string`, not `[]string`, deliberately: a plain
    slice cannot survive a decode/encode round-trip distinguishing "key
    absent" (nil → inherit the adapter-wide fallback) from "key present but
    empty" (non-nil, len 0 → explicit override to unrestricted). Confirmed
    empirically against both go-toml/v2 and viper/mapstructure — neither
    `omitempty` nor its absence preserves that distinction for a bare
    `[]string`; a pointer does, on both sides.
  - Added `validateAGUIProfiles(cfg)` (called from `Validate()` right after
    the agent-model validation loop): refuses an unknown `Base`, a profile
    name colliding with a `KnownAgentNames` entry, and — reading
    `viper.Get("agui.profiles")` directly, since decode into `AGUIProfile`
    silently drops what it doesn't recognize — an unrecognized key inside a
    profile block (named `aguiProfileKnownKeys`), unlike a stray top-level
    agent entry, which is pruned silently.
  - `KnownAgentNames` and the `Agent` struct are untouched.
  - Tests: `internal/config/agui_profiles_test.go` (6 tests) — unknown base,
    unknown key, name collision, absent-Profiles-loads-unchanged, Agent
    struct/KnownAgentNames pinned unchanged, and a real read-modify-write
    round-trip through `updateCfgFile` proving the nil-vs-empty-Tools
    distinction survives.

### PANDO-US-0013 — resolve profiles in the adapter, key the pool by profile, list in `/info`
- `internal/agui/deps.go`: new `Profile` struct (the adapter's *resolved*
  shape, mirroring `Config`/`AGUIConfig`) with `Name, Base, Model, Persona,
  Prompt, Tools, DenyTools, Mesnada` — all fallbacks already applied.
  `Config.Profiles map[string]Profile`; `ConfigFromApp` resolves
  `c.Profiles`, letting each profile inherit `Tools`/`Mesnada`/`Persona` from
  the adapter-wide resolved values unless it set its own (an explicit empty
  `Tools` list is a deliberate override, not "unset"). Added
  `Config.resolveProfile(name)`.
- `internal/agui/runtime.go`: `resolveAgent` now returns
  `(config.AgentName, *Profile, error)` — a declared profile resolves to its
  `Base` plus itself; an undeclared name still 404s. `New()` also validates
  every profile's resolved `Persona` at startup (fail fast, same as the
  adapter-wide one).
- `internal/agui/agentpool.go`: `filterAGUITools`/`aguiToolAllowed` gained a
  `deny []string` parameter (DenyTools glob deny-list, checked before the
  allow-list and before the Mesnada-tool prefix check; the `tool_search`
  drop-once-an-allow-list-is-set rule is unchanged and still the
  authorization boundary). `poolKey` now takes a plain route-key `string`
  instead of `config.AgentName`; `agentPool.get`/`buildLocked`/
  `buildToolsLocked` take a `routeKey`/`*Profile` so the pool is keyed by
  route (profile name, or bare agent name) rather than by `Base` agent —
  two profiles sharing one `Base` get two distinct pooled instances that
  never evict each other on lookup.
- `internal/agui/server.go`: `handleRun` computes `routeKey` from the raw
  path segment and threads `profile` through `pool.get` and
  `sessionForThread`. `handleInfo` lists every declared profile (sorted by
  name) alongside built-in agents, via new `profileModelDescriptor` (profile
  `Model` override, else `Base`'s configured model) — no agent is
  instantiated to answer `/info`.
- Tests: `internal/agui/deps_test.go` (profile resolution fallback/override
  semantics, defensive skip of an invalid profile), plus additions to
  `agentpool_test.go` (DenyTools filtering, two profiles → two pooled
  instances, profile allow-list closes the `tool_search` bypass) and
  `server_test.go` (`resolveAgent` profile case, `/info` profile listing
  with no pool touched, pool-key distinctness).

### PANDO-US-0014 — per-profile persona/prompt/model via `SetSessionLLMOverrides`
- `internal/llm/agent/session_overrides.go`: added `Prompt string` to
  `SessionLLMOverrides` (only honored when `PersonaScoped` is true, like
  `Persona`) — reusing the existing per-session override mechanism rather
  than adding a second injection path, per the story's explicit "do NOT add
  a second override mechanism".
- `internal/llm/agent/persona_selector.go`: `getPersonaContent` now appends
  `ov.Prompt` (via new `appendSessionPrompt` helper) to whatever persona
  content the `PersonaScoped` branch resolves, on every return path of that
  branch — so a bare `Prompt` override (no persona name) still reaches the
  system prompt.
- `internal/agui/runtime.go`: `applySessionPersona` renamed/extended to
  `applySessionOverrides(sessionID string, profile *Profile)`: resolves
  `Persona`/`Prompt`/`Model` from the profile when one is serving the run,
  else just the adapter-wide `Persona` (unchanged behaviour when neither is
  set). `sessionForThread` gained a `profile *Profile` parameter threaded
  from `handleRun`, calling this at all three session-(re)bind points.
- Tests: `internal/agui/persona_test.go` additions — profile overrides all
  three fields instead of the adapter-wide persona; a profile with no
  persona/prompt/model installs nothing (today's behaviour preserved); two
  concurrent goroutines applying two different profiles to two different
  session IDs never see each other's values (run with `-race`).
  `internal/llm/agent/persona_prompt_override_test.go` (new, in that
  package since `getPersonaContent` is unexported): persona+prompt joined,
  prompt-only with no persona name, and an unrelated session seeing neither
  — this is the direct proof the profile `Prompt` reaches the session's
  system text.

## Why

Lets one `pando agui-serve`/`pando serve --agui-port` process serve several
restricted, named assistants (e.g. a backlog bot and a docs bot) instead of
one Pando process per assistant, each with its own tool allow/deny list,
Mesnada switch, persona, extra system prompt and model — without touching
`agent.NewAgent`/`agent.Run` or the shared `config.Agent`/`KnownAgentNames`
built-in-agent machinery (I1/I2 in `internal/agui/doc.go` are preserved).

## Verification

- `go build ./...` — clean.
- `go test ./internal/agui/... ./internal/config/...` — all green (115 tests
  in `internal/agui`, full `internal/config` suite).
- `go test ./internal/llm/agent/...` — 4 pre-existing failures
  (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`,
  `TestCavemanSessionPolicyInstructions`,
  `TestApplyToolDiscoveryWithoutManagerIsUnchanged`), unrelated to this work
  (caveman/tool-discovery test-isolation leakage under full-package run
  order — each passes in isolation); confirmed pre-existing per the task
  brief.
- `go test ./internal/api/...` — green.
- `go vet ./internal/agui/... ./internal/config/... ./internal/llm/agent/...`
  — clean. `gofmt -l` clean on every touched file.

## Scope note

`internal/llm/agent/session_overrides.go` and
`internal/llm/agent/persona_selector.go` were edited even though the task
brief scoped work to `internal/agui/` + `internal/config/config.go`: US-0014
explicitly names `SessionLLMOverrides` (`session_overrides.go:23-38`) as the
mechanism to extend and forbids a second one, and there is no way to get a
literal `Prompt` string into the assembled system prompt without it — the
existing per-session pipe is a name→content persona lookup, not a raw-text
carrier. The change is a single additive field plus a same-shape branch
change in `getPersonaContent`, exercised by both `internal/agui` tests
(override installation) and one small `internal/llm/agent` test (persona
content assembly, since `getPersonaContent` is unexported).
