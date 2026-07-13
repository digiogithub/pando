---
created_at: 2026-07-13T07:09:29.791614465Z
updated_at: 2026-07-13T07:09:29.791614465Z
tags:
    - plan
    - caveman
    - slash-commands
    - settings
    - toml
    - token-optimization
---
# Caveman Persistent Output-Brevity Mode Implementation Plan

## Objective
Implement a Pando-owned Caveman-compatible output-brevity mode. It is enabled per session with `/caveman [lite|full|ultra|wenyan]`, disabled in that session with `/caveman-finish`, and configured globally from TUI and Web UI settings. The global default must persist in Pando's TOML configuration.

The feature reduces output-token consumption by removing filler and unnecessary explanations while preserving technical substance, code, commands, file paths, errors, requested detail, and verification. It constrains expression, not reasoning, tool use, or execution quality. Therefore it must not affect the agent's general task performance or relax Pando safety, testing, and evidence requirements.

## Research and decisions
- Upstream source reviewed: https://github.com/juliusbrussee/caveman (MIT, v1.9.1 at research time). It describes output-style compression only: keep code/commands/errors exact, remove filler, and retain the user's language. Its reported output-token reduction is a benchmark claim, not a product guarantee.
- Upstream documents that input and reasoning tokens are not reduced and that its injected prompt can make total-session savings smaller or negative for already terse work. Pando documentation and UI must state this limitation.
- Pando has a directly relevant precedent in `internal/ponytail`, `internal/llm/agent/ponytail_session.go`, and `/ponytail`: default configuration plus concurrent-safe session override and per-turn prompt injection.
- Pando persists runtime configuration through `config.updateCfgFile` helpers; TUI uses `persistSetting` in `internal/tui/page/settings.go`; Web UI reads/writes `/api/v1/settings` and receives configuration-change events.
- Decision: implement a compact internal policy instead of vendoring Caveman assets, hooks, telemetry, stats, MCP middleware, memory rewriting, or upstream subagents.
- Decision: use the neutral product description “Caveman output brevity” in Pando interfaces. Do not require caricature language: responses remain in the user's language and are concise, direct, and technically complete.
- Decision: use the four upstream style levels (`lite`, `full`, `ultra`, `wenyan`); `full` is the default when a user calls `/caveman` without an argument. `wenyan` is explicit opt-in and may render natural-language prose in Classical Chinese; code, commands, paths, errors, and user-requested language requirements remain authoritative.
- Decision: global config controls the default for sessions without an override. Slash commands override only the current session. `/caveman-finish` records explicit session-off so it wins over a non-off global default; it does not alter the user's persistent config.
- Decision: v1 retains session overrides only in process memory, like Ponytail. The persisted TOML setting survives restart and applies on subsequent sessions. Durable per-session overrides are deferred.

## User-visible behavior
### Persistent setting
- New config table: `[Caveman]` with `DefaultMode = ""` (disabled), or `lite|full|ultra|wenyan`.
- UI label: **Caveman Output Brevity**.
- UI description: **“Reduces output-token usage by giving shorter explanations and removing filler. It keeps code, commands, errors, reasoning quality, tool use, and verification intact. Output savings vary; input and reasoning tokens are not reduced.”**
- The select control offers: `Off`, `Lite`, `Full`, `Ultra`, and `Wenyan`.
- Changing the setting updates Pando's in-memory config and the active TOML file atomically using the existing configuration writer. It affects new/unset sessions immediately; it never overwrites explicit session choices.

### Slash commands
- `/caveman [lite|full|ultra|wenyan]`: enable a mode for the current session; no argument selects `full`. It is synchronous and does not run an LLM task.
- `/caveman-finish`: disable Caveman output brevity for the current session; it is synchronous and does not run an LLM task.
- Activation response explains that Pando will use fewer words and less explanatory filler to reduce output tokens, without reducing reasoning, correctness, tool use, testing, or verification.
- Invalid arguments return exact supported values and do not change current state.
- Commands are exposed consistently in native Web UI/TUI completion, command API, and ACP.

### Prompt policy
- Preserve the language selected by the user.
- Prefer direct conclusions, bullets/fragments, exact commands, and short findings.
- Omit greetings, restatement, filler, generic transitions, redundant summaries, and optional explanation unless requested.
- Never abbreviate or mutate code, command lines, file paths, URLs, JSON/YAML/TOML, error text, API signatures, security warnings, test output, approval questions, or explicitly requested detail.
- Never skip root-cause evidence, test commands/results, permissions, or safety caveats to be shorter.
- A direct request for a detailed explanation overrides the brevity preference for that reply.

## Phases

### Phase 0: Acceptance contract and configuration compatibility
1. Write a concise project design note that fixes the precedence order: direct user instruction and project rules -> explicit session mode -> global `Caveman.DefaultMode` -> off.
2. Reserve `caveman` and `caveman-finish` in the built-in command registry; verify no existing custom-command convention is broken.
3. Define acceptance cases for all five modes, invalid input, global default, explicit off over global default, process restart, two concurrent sessions, clean mode, detailed-user override, and settings writes that fail.
4. Decide and document TOML schema/casing with existing configuration conventions:
   ```toml
   [Caveman]
   # Output brevity default for sessions with no explicit slash-command choice.
   # Valid: "", "lite", "full", "ultra", "wenyan".
   DefaultMode = ""
   ```

Exit criteria: approved compatibility and precedence matrix; default installations retain existing output behavior.

### Phase 1: Core Caveman mode package
1. Create `internal/caveman/` with:
   - `type Mode string` and constants `ModeOff`, `ModeLite`, `ModeFull`, `ModeUltra`, `ModeWenyan`;
   - `ParseMode(string) (Mode, bool)`, `IsActive()`, `String()`, and `Description()`;
   - `Instructions(Mode) string`, containing Pando's neutral, safety-preserving output policy.
2. Create `internal/llm/agent/caveman_session.go`, patterned after Ponytail:
   - `SetCavemanMode(sessionID string, mode caveman.Mode)`;
   - `CavemanMode(sessionID string) caveman.Mode`;
   - `cavemanModeForContext(ctx context.Context) caveman.Mode`.
3. Store explicit session choices in a concurrency-safe map. On off, delete the override only when the global default is off; otherwise retain explicit off so it overrides the configured default.
4. Resolve global default via `config.CavemanDefaultMode()`. Reject unknown values defensively as off.
5. Add package and agent-session unit tests for parser normalization, descriptions, global fallback, explicit off, context lookup, session isolation, and concurrent access.

Exit criteria: independent, race-safe mode resolution with no configuration or prompt dependency cycle.

### Phase 2: TOML configuration and durable update path
1. Add `CavemanConfig` and `Caveman CavemanConfig` to `internal/config/config.go`, with JSON/TOML tags matching `PonytailConfig`.
2. Implement `(*Config).CavemanDefaultMode()` to normalize only supported values. Do not introduce an environment override in v1 unless Pando has a documented requirement for it.
3. Implement `config.UpdateCaveman(CavemanConfig) error` or a focused `UpdateCavemanDefaultMode(string) error`, following the existing save-and-rollback pattern around `updateCfgFile`.
4. Add the `[Caveman]` section and explanatory comments to the shipped `.pando.toml` template/default configuration.
5. Add config tests that load absent/valid/invalid values, verify a persisted TOML write, verify in-memory rollback on write error, and preserve unrelated config fields.

Exit criteria: configuration round-trips safely and default mode changes take effect without restart.

### Phase 3: Per-turn prompt injection
1. Update `internal/llm/agent/agent.go:prepareProvider` so its fast path also checks whether Caveman is active for the request context.
2. Append `prompt.InjectSkillInstructions("caveman ("+mode.String()+")", caveman.Instructions(mode))` for an active session, using the same prompt composition mechanism as ordinary skills and Ponytail.
3. Keep clean mode authoritative: clean mode must not inject Caveman.
4. Define composition behavior explicitly: automatic skills -> Superpowers policy if enabled -> Caveman output-brevity policy -> Ponytail. Each policy remains subject to direct user and project instructions.
5. Add agent-level tests for enabled/disabled prompt content, a configuration-derived mode, session override precedence, clean mode, concurrency, and coexistence with Ponytail/Superpowers.

Exit criteria: only the intended session receives the terse-output policy; agent execution capability is unchanged.

### Phase 4: Slash command support across native and ACP surfaces
1. Add `caveman` and `caveman-finish` to `internal/commands/registry.go`. Mark activation as accepting arguments and finish as argument-free.
2. Add ACP slash-command kinds/specifications/parser cases in `internal/mesnada/acp/slash_commands.go`.
3. Extend `internal/mesnada/acp/types_interfaces.go` and its concrete agent adapter with `SetCavemanMode(sessionID, mode string) (applied string, ok bool)`.
4. Add `internal/mesnada/acp/caveman_commands.go`, modeled on `ponytail_commands.go`, to apply/clear state and send explanatory confirmation without an LLM turn.
5. Extend `internal/api/handlers_chat.go:handleSlashCommandStream` with the same synchronous behavior for Web UI/TUI chat.
6. Ensure `/caveman-finish` accepts no level; if arguments are supplied, return usage rather than silently ignoring them.
7. Add parity tests for native command registry, ACP advertised commands, parsing, success states, invalid modes, global-default override behavior, and finish behavior.

Exit criteria: all interactive surfaces advertise and execute consistent commands.

### Phase 5: TUI settings
1. Add a `Caveman Output Brevity` settings section, located beside token/output optimization settings or another existing output-control group. Do not create a duplicate general setting.
2. Add a `caveman.defaultMode` `FieldSelect` with Off/Lite/Full/Ultra/Wenyan, initial value resolved from `cfg.CavemanDefaultMode()`.
3. Show the user-facing description from this plan in a read-only help/info field or concise adjacent help text, including that savings apply to output tokens only and do not compromise performance or verification.
4. Extend `persistSetting` and its focused persistence helper to validate and call the shared config updater.
5. Add TUI tests for field construction, accepted/rejected selections, persistence success/failure alerts, and immediate readback after a save.

Exit criteria: the TUI changes and persists the global default without affecting explicit session overrides.

### Phase 6: Web UI settings and API
1. Extend `SettingsResponse`, `SettingsUpdateRequest`, and `buildSettingsResponse` in `internal/api/handlers_settings.go` with `caveman_default_mode`.
2. In the PUT handler, validate the field and call the shared configuration updater; return a client error for unsupported values and retain previous state.
3. Add `caveman_default_mode` to `web-ui/src/types/index.ts` and the defaults/store state in `web-ui/src/stores/settingsStore.ts`.
4. Add a `Caveman Output Brevity` control in the relevant settings component, reusing existing select and help-text patterns. It must display the exact concise description and the output-only caveat.
5. Ensure config-events refresh state after changes made in the TUI or another Web UI instance.
6. Add Go HTTP tests for GET/PUT success, validation failure, persistence error, and response consistency. Add Web UI component/store tests for loading, selecting, saving, reset, error state, and remote refresh.

Exit criteria: Web UI settings persist to TOML, accurately reflect current configuration, and synchronize with other configuration surfaces.

### Phase 7: Documentation, quality gates, and measurement honesty
1. Document config, commands, levels, scope precedence, and constraints in README/config reference.
2. State accurately: Caveman aims to reduce verbose output; output savings depend on model/task; input and reasoning tokens are not reduced; concise output never permits lower-quality execution or skipped verification.
3. Do not ship a percentage saving claim without a reproducible Pando-specific benchmark. If telemetry/stats are ever considered, make them opt-in, local-first, and a separate design.
4. Run targeted tests:
   ```bash
   go test ./internal/caveman ./internal/config ./internal/llm/agent ./internal/api
   go test ./internal/mesnada/acp ./internal/commands
   go test -race ./internal/caveman ./internal/llm/agent
   ```
5. Run `go test ./...` and the Web UI test/build commands supported by the repository, recording pre-existing failures separately.
6. Save a final implementation summary and verification results in Pando KB as required by AGENTS.md.

Exit criteria: documented, tested, default-off feature with no unsupported savings promises.

## Risks and mitigations
- Compression may omit important detail: instruction rules explicitly protect requested detail, code, commands, errors, safety, tests, and proof.
- “No performance impact” cannot be guaranteed solely by prompting: define it as a quality constraint, test representative tasks, and do not claim measured invariance without benchmarks.
- Prompt overhead can offset savings: default off, inject only when active, and document the output-only limitation.
- Cross-surface divergence: use shared config updater and parity tests for registry/API/TUI/Web UI.
- Config setting unexpectedly changes active work: global default applies only where there is no explicit session override.
- User language distortion: retain user language except explicit `wenyan`.

## Deferred follow-ups
- Persist session-specific override state in session metadata/database.
- A transparent local token-usage dashboard with reproducible Pando baselines.
- Per-project Caveman defaults that layer above user-global settings.
- A unified declarative slash-command registry shared by native and ACP implementations.
