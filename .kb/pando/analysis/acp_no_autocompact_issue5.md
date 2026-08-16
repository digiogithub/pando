# Issue #5 — "Error no auto compact in ACP mode" (analysis, 2026-08-16)

Read-only investigation. No code changed.

Symptom reported: agent loop stops after a few tool steps in ACP mode, without feedback.

## Root causes found

### 1. Auto-compact is effectively OFF everywhere; TUI has a safety net, ACP does not

- `agent.shouldCompact` (`internal/llm/agent/agent.go:2027`) gates on the **per-agent** flag
  `cfg.Agents[coder].AutoCompact`. The **global** `cfg.AutoCompact` is never read there.
- The generated config template writes `AutoCompact = true` at top level
  (`internal/config/init.go:116`) but `AutoCompact = false` under `[Agents.coder]`
  (`internal/config/init.go:231`). The repo's own `.pando.toml` has the same combination
  (line 13 `true`, line 89 `false` for coder). No `viper.SetDefault` ever sets
  `agents.coder.autocompact`, so the default is Go zero value `false`.
- Consequence: `shouldCompact` returns false always in a default install. In-loop compaction
  (`agent.go:1178`) never runs.
- The TUI compensates: `internal/tui/tui.go:710` triggers a session compaction when
  `tokens >= 0.95 * contextWindow` **and** the *global* `config.Get().AutoCompact` is true.
  ACP (`internal/mesnada/acp`) and the REST/WebUI surface have no equivalent net.
  That is exactly why the bug reads as "no auto compact in ACP mode".

ACP itself does relay compaction status correctly
(`normalizeSystemMessage`, `internal/mesnada/acp/agent.go:970` maps "auto-compacting context" →
"Compacting…", "context compacted" → usage update). The relay never fires because the event is
never emitted.

### 2. Silent history trimming replaces compaction — no user feedback

`processGeneration` (`agent.go:1024-1026`) calls
`trimMessagesToContextBudget(msgs, contextWindow, 0.40)` on every turn: it drops the **oldest**
messages until the history fits 40% of the window. `trimMessagesToContextBudget`
(`agent.go:2775`) only writes `logging.InfoPersist` — it emits no `AgentEvent`, so ACP clients
see nothing. `fitMessagesToProviderBudget` (`agent.go:1426`) trims the same way, also silently.
`sanitizeToolCallHistory` then injects synthetic `"Tool execution was interrupted"` tool results
for calls whose originating assistant turn was trimmed away.

Net effect in a long ACP session: the model silently loses the head of the conversation and gets
fake "interrupted" tool results, so it wraps up early after a few tool steps. No error, no status
message — matches the report exactly.

### 3. Errored turns are reported to ACP as a normal end_turn

`mapFinishReasonToStopReason` (`internal/mesnada/acp/prompt_handler.go:1218`) has no case for
`message.FinishReasonError` (and none for `FinishReasonUnknown`); both fall into
`default: StopReasonEndTurn`. An assistant message finished with `error` is therefore presented
to Zed/VS Code as a clean end of turn — a second silent-stop path.

### 4. Tool pagination gap: session cache only exists for *newly created* sessions

Auto-pagination is `tools.InterceptToolResponse` (threshold 15000 bytes / 300 lines,
`internal/llm/tools/cache_interceptor.go`), applied to built-in tools at
`agent.go:1620` and to MCP tools at `internal/llm/agent/mcp-tools.go:194`. Both are conditional on
a session cache being present:

- `streamAndHandleEvents` injects it via `tools.GetSessionCacheByID(sessionID)` (`agent.go:1464`).
- The cache is registered **only** in `session.Create` (`internal/session/session.go:131`) and in
  the MCP server for its own session ids (`internal/mesnada/server/server.go:261`).
  `session.Get` does not register it.
- ACP `session/load` (`internal/mesnada/acp/agent.go:449`, `:709`) only calls
  `sessionService.GetSession`. So a **resumed** ACP session has no cache and therefore **no tool
  pagination at all**: every large bash/grep/view/search/remembrances result enters the context in
  full, while auto-compact is off. Fastest possible context exhaustion.
  (Same hole affects a TUI session resumed after restart, but the TUI 95% net catches it.)

Secondary pagination gaps, independent of the surface:
- `cacheBypassTools` marks `diagnostics` as never-cacheable — LSP diagnostics output can be large.
- `edit`/`write`/`patch` bypass is fine (small confirmations).

## Suggested fixes (not implemented)

1. `shouldCompact`: fall back to the global `cfg.AutoCompact` when the per-agent flag is unset, or
   default `Agents.coder.AutoCompact` to `true` in `ensureAgentDefaults` and stop writing `false`
   in the template.
2. Port the TUI 95% safety net into the shared agent loop (or add it to the ACP prompt handler) so
   every surface compacts.
3. Emit an `AgentEventTypeSystemMessage` from `trimMessagesToContextBudget` /
   `fitMessagesToProviderBudget` ("history trimmed, N messages dropped") so ACP shows it, and add
   an explicit "context window exhausted" notice.
4. Map `FinishReasonError`/`FinishReasonUnknown` to a non-`end_turn` stop reason in
   `mapFinishReasonToStopReason`, and surface the error text.
5. Register the session cache on resume: call `tools.RegisterSessionCache(id)` from `session.Get`
   (idempotent) or from ACP `LoadSession`/prompt setup.

## Verification

Static analysis of the code paths above (grep/read). No build or test run — no code was modified.
