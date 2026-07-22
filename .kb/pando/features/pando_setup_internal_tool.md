---
created_at: 2026-07-22T22:11:24.3677983Z
updated_at: 2026-07-22T22:11:24.3677983Z
tags:
    - feature
    - tools
    - agent
    - config
    - models
    - slash-commands
---
# Feature: `pando_setup` internal tool (2026-07-23)

## What was built

A new **always-on, non-configurable** internal agent tool named `pando_setup`. It is Pando's
own control panel for the agent: read-only inspection of the whole configuration and runtime
state, model/provider discovery, session usage reporting, and autonomous activation of slash
commands.

The design deliberately mirrors a CLI (`pando desktop --help` style): the tool schema stays
minimal and homogeneous — two parameters, `command` and `args` — and the detail is discovered
on demand through `help` / `--help`. This keeps the always-loaded tool description cheap in
tokens while still exposing a rich command surface.

### Command surface

| Command | Arguments | Purpose |
| --- | --- | --- |
| `help` | `[command]` | List commands, or print one command's usage |
| `config` | `[section] [--search TERM]` | Read the active configuration (same visibility as the TUI/WebUI settings panels), read-only |
| `providers` | `[--all]` | Provider accounts: id, type, credential kind, base URL, model count |
| `models` | `[--provider P] [--account ID] [--search T] [--detail] [--limit N]` | Selectable models by canonical id (`copilot.gpt-5.4`), with models.dev cost/context/knowledge under `--detail` |
| `session` | — | Last turn's token counters, accumulated cost, model, active modes |
| `commands` | `[--all]` | Slash commands, marking which ones the agent may activate |
| `run` | `<command> [args]` | Activate a slash command for this session |

Every command also answers `--help` (and `-h`), which returns the same text as `help <command>`.

## Files and symbols

### New — `internal/llm/tools/pando_setup.go`
- `PandoSetupToolName = "pando_setup"`, `NewPandoSetupTool(bridge SetupBridge) BaseTool`.
- `SetupBridge` interface + `SetupSessionInfo`, `SetupMode`, `SetupRunnableCommand` DTOs.
  The interface exists **because `internal/session` imports `internal/llm/tools`**, so the
  tool cannot import the session store or the agent package — the dependency is inverted and
  injected instead. A nil bridge is tolerated: `config`/`providers`/`models` still work.
- `setupCommands()` — the command table (name, summary, usage, run func).
- `buildSetupConfigTree(cfg)` — marshals `*config.Config` to JSON and redacts it. Going
  through JSON rather than a hand-written view is what keeps the tool in sync with the
  settings panels: a new config section appears automatically.
- `redactSetupValue` / `isSetupSecretKey` / `maskSetupSecret` / `maskSetupStringMap` /
  `maskSetupEnv` — secret masking (last 4 chars kept so the agent can tell "configured" from
  "unset" without seeing the value).
- `parseSetupArgs` / `tokenizeSetupArgs` / `setupArgs` — quote-aware CLI argument parser with
  a declared boolean-flag set (`setupBooleanFlags`).

### New — `internal/llm/agent/setup_bridge.go`
- `NewSetupBridge(sessions session.Service) tools.SetupBridge`.
- `setupRunnableSpecs()` — the slash commands the agent may activate, each with either
  `Apply` (mutates session mode) or `Prompt` (returns instructions to follow this turn).
- `setupBlockedCommands` — commands refused to the agent, each with its reason.

### New — `internal/commands/custom.go`
- `Content(dataDir, name) (string, bool)` — resolves a `user:`/`project:` command id back to
  its markdown body, with path-traversal rejection (`isWithin`).
- `customCommandDirs(dataDir)` — extracted from `AllCommands`, now shared by both.

### Modified
- `internal/commands/registry.go` — `AllCommands` now loops over `customCommandDirs`.
- `internal/llm/agent/tools.go` — `CoderAgentTools` and `CoderAgentToolsWithMesnada` gained a
  `sessions session.Service` parameter and register `tools.NewPandoSetupTool(NewSetupBridge(sessions))`
  on both the gateway and non-gateway paths. `pando_setup` was added to `alwaysIncludedTools`
  so the ContextTrimmer cannot strip it on the first message (the need for it appears mid-task).
- `internal/llm/tools/builtin_names.go` — `PandoSetupToolName` added so the MCP gateway never
  claims it and returns an actionable redirect instead.
- `internal/app/app.go` — passes `app.Sessions` to `CoderAgentToolsWithMesnada`.
- `README.md` — new "Agent Self-Service (`pando_setup`)" section before the slash-command table.

## Design decisions

1. **Self-discovery over a fat schema.** The tool description points at `command="help"` and
   never enumerates the commands; a test enforces that the description stays under 600 chars
   and does not inline any command usage.
2. **Configuration is strictly read-only.** No write path exists. Secrets are masked by key
   suffix — `apikey`, `token` (singular, so `promptTokens`/`maxOutputTokens`/`tokenOptimization`
   survive), `secret`, `password`, `credential`, `privatekey`, `agekeys`, `codeverifier`,
   `oauthstate` — plus header maps (`headers`, `extraHeaders`) and `NAME=value` env entries.
3. **`run` cannot hijack the session.** Mode commands (`/caveman`, `/caveman-finish`,
   `/ponytail`, `/superpowers`, `/learning`) mutate session state and report that they apply
   from the next turn. Instruction commands (`/improve-agents-md`, `/vulnhunt`,
   `/vulnhunter-fix`, `/vulnhunt-fix-verify`, and custom `user:`/`project:` commands) return
   their prompt framed as "follow these instructions in this turn" — no nested agent run.
   `/goal*`, `/compact`, `/db-compact` and the `-finish` closing turns are **blocked** with a
   reason: they are surface-driven, and the `-finish` commands clear their mode only on a
   successful terminal response (`RunSuperpowersFinish` / `RunLearningFinish`), which a tool
   call cannot observe.
4. **Session token semantics.** `session.Session.PromptTokens`/`CompletionTokens` are
   overwritten each turn by `agent.TrackUsage` (only `Cost` accumulates), so the report labels
   them "last turn" and the cost "accumulated over the session". Reporting them as session
   totals would have been wrong.
5. **Models come from the live registry** (`models.GetAllModels()`), which
   `app.RefreshDynamicModels` populates at startup with account-scoped canonical ids and
   models.dev enrichment. No network call from the tool.

## Verification

- `go build ./...` — clean.
- `go vet ./internal/llm/... ./internal/commands ./internal/app` — clean (the pre-existing
  `internal/mesnada/agent/spawner_template.go` cancel-leak warning is unrelated).
- `go test ./internal/llm/agent ./internal/api ./internal/commands ./internal/llm/tools` — all pass.
- New tests: `internal/llm/tools/pando_setup_test.go` (schema self-description, help routing,
  argument parsing incl. quoting and boolean flags, redaction of a **real** `config.Config`
  with asserted absence of every planted secret, config index/section lookup, models
  filtering/detail/limit/truncation, session rendering, commands/run dispatch, nil-bridge
  behaviour, builtin registration), `internal/llm/agent/setup_bridge_test.go` (mode apply,
  argument rejection without state change, instruction framing, blocked commands, unknown
  command, runnable/blocked invariants, session modes), `internal/commands/custom_test.go`
  (project/nested command bodies, path-traversal rejection).
- Manual smoke run of `help`, `run --help`, `models --provider anthropic --detail --limit 2`
  and `providers` through the real tool entrypoint; output reviewed then the scratch test removed.

Related: [[feature_modelsdev_catalog]], [[superpowers_mode]], [[caveman-persistent-output-brevity-mode]],
[[feature_vulnhunter_security_commands]].
