---
created_at: 2026-10-23T16:18:29.402736472Z
updated_at: 2026-10-23T16:18:29.402736472Z
tags:
    - feature
    - mesnada
    - delegation
    - engines
    - depth
    - config
    - webui
    - tui
    - api
    - i18n
---
# Feature: Mesnada engine toggles + real delegation nesting cap

Date: 2026-10-23. Status: implemented, uncommitted (jj working copy). Builds on [[delegation_phase5_config_ui]] and [[custom_engine_templates]].

## Why
Mesnada subagents used any installed CLI engine (`detectEngineForModel` auto-picked claude/gemini/opencode/vibe/copilot when on PATH), and `mesnada.delegation.maxDepth` was never enforced at spawn time; it only gated resurrection. Production telemetry showed depth-1 subagents spawning 10 depth-2 grandchildren. The user asked for:
- per-engine toggles in the Mesnada settings (native Pando engines locked);
- a real nesting limit;
- a dedicated "subagents cannot launch subagents" switch.

## Config
- `mesnada.orchestrator.disabledEngines []string` (json `disabledEngines`), default empty.
  - `config.NormalizeMesnadaDisabledEngines` lowercases, resolves the claude-code/gemini-cli aliases, dedupes, sorts, and strips the natives (`pando`, `warm-acp`, `acp-server`, empty).
  - Applied on load (`normalizeMesnadaOrchestratorDefaults`) and on save (`UpdateMesnada`, `UpdateMesnadaOrchestrator`).
  - No viper default is needed: nil is the default.
- `mesnada.delegation.allowNestedSubagents *bool`. nil means true.
  - It is a pointer so that an unset key, including the viper nested-default shadowing case, reads as true, while an explicit false survives both JSON `omitempty` and shadowing.
  - Accessors: `MesnadaDelegationConfig.NestedSubagentsAllowed()` and `EffectiveMaxDepth()`. The effective depth is 1 when nested subagents are off, else `MaxDepth` (default 3).
- `maxDepth` semantics: the deepest level a delegated task may run at. The main session is level 0, so 1 means subagents cannot spawn.

## Shared packages
- `internal/mesnada/depth` is a stdlib-only leaf package. It holds:
  - `EnvVar` = `PANDO_DELEGATION_DEPTH`, `MetaKey` = `pando:delegationDepth`, `HeaderName` = `X-Pando-Delegation-Depth`;
  - the context helpers `WithContext` / `FromContext`;
  - the per-session registry `SetSession` / `Session` / `ClearSession`;
  - `FromMeta` / `Meta`, `FromEnv`, and `Resolve(ctx, sessionID)`. `Resolve` checks the context, then the session record, then the env var.
- `internal/mesnada/engines` holds the engine catalogue and the live policy:
  - `Builtin`, `Normalize`, `IsNative`;
  - `Disabled`, `IsDisabled`, `CheckEnabled` (returns `*DisabledError`: `engine "claude" is disabled in Mesnada settings; use engine=pando`), `FilterEnabled`;
  - `MaxDepth` (effective), `NestedAllowed`, `CheckSpawnDepth` (returns `*DepthError`), `CanDelegate`;
  - `Detect(custom, disabled)`, which uses `exec.LookPath` and returns `Status{name,label,kind,binary,installed,path,enabled,locked}`.
  - Every check reads `config.Get()` live, so a settings change applies on the next call.

## Enforcement points
- **Orchestrator.**
  - `Spawn` and `Relaunch` reject a disabled engine.
  - `defaultEngineFor()` falls back to pando when the default engine is disabled, and logs it.
  - `CreateSwarm` pre-validates every role engine before spawning anything.
  - New `CustomEngineInfos()` (`EngineTemplateInfo` gained `Command`). `ListCustomEngines` and `ListPersonas` are now nil-safe.
- **Spawn tool** (`internal/llm/tools/mesnada.go`).
  - The engine enum and description are filtered live. `Info()` is rebuilt per request because the providers convert tools on every call.
  - A disabled configured `DefaultEngine` falls back to pando.
  - `CheckSpawnDepth(depth)` runs before spawn.
  - `mesnada_swarm` applies the same check.
  - `spawnCorrelation` uses `depth.Resolve`.
- **Mesnada MCP server** (`internal/mesnada/server`).
  - `detectEngineForModel` skips disabled engines.
  - The fallback `spawn_agent` enum is filtered.
  - `toolSpawnAgent` computes depth (Resolve+1), checks the cap and sets `SpawnRequest.Depth`.
  - `handleMCP` puts the `X-Pando-Delegation-Depth` header into the context.
- **Tool hiding.**
  - `agent.toolsForSession` drops spawn/swarm/await (`tools.WithoutDelegationSpawnTools`) when `!tools.SessionCanDelegate(sessionID)`. This is evaluated per request.
  - `cmd/mcp_server.go` omits the spawn tool when the process level is at the cap.
- **Resurrection.** `delegation_supervisor` now checks `task.Depth > MaxDepth`, and the app passes `EffectiveMaxDepth()`.

## Depth propagation per path
- **Cold pando CLI and ACP subprocesses.** `withDelegationDepth(env, task.Depth)` replaces any inherited value.
- **External CLI agents** (claude, copilot, gemini, opencode, ollama-*, vibe, templates).
  - Every spawner now also sets the env var, so the `pando mcp-server` grandchild inherits the right level.
  - The per-task dynamic MCP config's `pando` stdio entry carries `env: {PANDO_DELEGATION_DEPTH: <task depth>}` via `BuildSubagentMCPConfig(..., taskDepth)`.
  - The vibe TOML renderer ignores per-server env, so vibe relies on the inherited env.
- **Warm ACP.**
  - `tryStartWarm` passes `depth.WithContext(o.ctx, task.Depth)` to `RunWarm`.
  - `project.delegateOn` sets session/prompt `_meta["pando:delegationDepth"]`.
  - `PandoACPAgent.Prompt` records `depth.SetSession(pandoSessionID, level)`.
- **IPC** (external peer or web child). `protocol.DelegationRunParams.Depth *int` is filled from the context in `delegateViaIPC`. `RunDelegation` records it for the ephemeral session. An older peer ignores the field.
- **HTTP MCP.** The `X-Pando-Delegation-Depth` header goes into the context. It is also added to the CORS allow-list.

## API and UI
- `GET /api/v1/mesnada/engines` returns `{engines: Status[]}` (`internal/api/handlers_mesnada_engines.go`).
- Settings GET/PUT fields:
  - `orchestrator_disabled_engines` (`[]string`, PUT `*[]string` replaces the list);
  - `delegation_allow_nested_subagents` (bool).
  - `delegation_max_depth` must now be >= 1.
- **WebUI.**
  - `MesnadaSettings.tsx` gained a "Subagent nesting" section (toggle and max-levels input; the input is disabled and marked overridden when the toggle is off).
  - It also gained a "Subagent engines" section with a Switch per engine. Natives are locked with a "Built-in" badge. Engines that are not installed are greyed but still toggleable. A Custom badge marks template engines.
  - These save through the existing services PUT (`mesnada.orchestrator.disabledEngines`, `mesnada.delegation.*`).
  - `GeneralSettings.tsx` "Subagent Delegation" has the same toggle and max-levels hint (min 1) through the settings store.
  - New store `mesnadaEnginesStore.ts` (fetch and pure helpers) with vitest.
  - i18n keys `settings.mesnada.nesting.*` and `settings.mesnada.engines.*` added for en/es/fr/de/pt/ja/zh; `settings.general.delegationMaxDepth` relabelled.
- **TUI** (`internal/tui/page/settings.go`).
  - The Mesnada section has an "Allow Subagents To Launch Subagents" toggle and a "Max Delegation Levels" field, which is disabled and marked overridden when the toggle is off.
  - A "Subagent engines" header lists one toggle per detected engine (key `mesnada.engine.<name>`; natives disabled with the "Built-in" hint).
  - `saveMesnada` now routes every `mesnada.delegation.*` key to `saveMesnadaDelegation`. This also fixes a pre-existing bug where keys after warmInstanceIdleTimeout returned "unsupported".

## Verification
- `~/.g/go/bin/go build ./...`: OK.
- `go test ./internal/config/... ./internal/mesnada/... ./internal/llm/tools/... ./internal/llm/agent ./internal/api/... ./internal/app/... ./internal/tui/... ./internal/ipc/... ./internal/project/...`: all OK.
- web-ui: `bun run typecheck` OK; `bun run lint` 0 errors (6 pre-existing warnings); `bun run test` 27 files / 131 tests OK; `bun run build:embedded` OK.
- New tests:
  - config normalization and nested default;
  - engines policy and Detect;
  - depth Resolve and FromMeta;
  - orchestrator disabled engine, default fallback, relaunch, swarm, and dynamic MCP depth;
  - spawn tool enum, depth refusal, nested refusal, warm session depth, and tool hiding;
  - agent toolsForSession hiding;
  - ACP `_meta` recording; IPC RunDelegation depth; MCP header;
  - API engines endpoint and settings round trip;
  - resurrection at-cap test;
  - vitest for mesnadaEnginesStore.

## Open risks
- A cold subagent process reads its own config at startup. A settings change reaches an already-running subagent process only after its next config load. Its spawn refusals use that config, while the parent's orchestrator always reads live.
- The WebUI General page (settings store) and the Mesnada page (services store) both write delegation fields. Stale-snapshot overwrites between the two pages are possible, as for the other delegation fields before.
- The ACP engine binaries `claude-code-acp` / `codex-acp` are conventional names for detection only. The generic `acp` engine shows "not detected" because it depends on configured agents.
- The vibe MCP TOML has no per-server env, so it relies on the inherited spawner env.
- The per-session depth registry is never pruned. It holds one int per warm or IPC delegated session.
- `project.delegateOn` `_meta` injection has no unit test (no fake ACP conn). The receiving side is tested.
