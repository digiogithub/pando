---
created_at: 2026-09-12T10:46:45.720040507Z
updated_at: 2026-09-12T10:46:45.720040507Z
tags:
    - fix
    - config
---
# Cleanup: project `.pando.toml` keys whose empty value masked a default

Date: 2026-09-12. Applies to the repo's own `/www/MCP/Pando/pando/.pando.toml`.

## Why
Before [[pando/fixes/config_update_zero_value_overwrite.md]], every `config.Update*` call rewrote the whole project file from a `Config` struct with no `omitempty` TOML tags, so keys the file never had were persisted as zero values (`''`, `0`, `false`, `[]`). Those zero values then WON over `setDefaults` on the next `Load`, silently turning features off or to 0.

The patching save path added by that fix stops new damage but cannot remove the existing keys: it cannot tell a value the user set on purpose from one the old save invented.

## What was done
A script compared every flattened key in `.pando.toml` against the `viper.SetDefault` table in `internal/config/config.go:setDefaults` and selected keys where the file held a zero value AND the default was non-zero. 52 keys matched and were deleted (file 572 → 520 lines); a backup was kept outside the repo. `tomllib` parses the result and `pando ipc status` loads it.

Keys removed, by area:
- `acp.enabled/max_sessions/idle_timeout/log_level` (defaults true / 10 / 30m / info)
- `agui.host/path/agents/agentPoolSize/agentPoolTtl/requireToken/frontendTools/humanInTheLoop` (AG-UI stays disabled: `agui.enabled` default is false)
- `design.outputDir/systemDir/defaultKind` and `design.critique.enabled/maxRounds/threshold/policy`
- `goal.autoApprove/maxIterations/maxDuration/stallIterations/dangerousPatterns`
- `image.maxWidth/maxHeight/maxBase64Bytes/quality`
- `internalTools.browserHeadless/fetchMaxSizeMb/perplexitySearchEnabled/perplexityApiKey` and the five `desktop*` knobs
- `shell.path/args` (default is the login shell with `-l`)
- `mesnada.server.host`, `mesnada.orchestrator.defaultModel`, `mesnada.tui.webui`, `mesnada.delegation.autoStartWarmInstance`
- `openlit.endpoint/serviceName/insecure` (OpenLit stays disabled), `lua.timeout`, `server.requireAuth`, `skillsCatalog.baseUrl/defaultScope`, `tokenOptimization.readModeDefault`, top-level `contextPaths`

`server.requireAuth` returning to its `true` default is safe here because `[Server.BasicAuth]` is already enabled in this project.

## How to repeat it elsewhere
Any project whose `.pando.toml` was saved by an affected build has the same keys. Diff the file's zero-valued keys against `setDefaults` and delete the matches, or regenerate the file with `pando init`. Keys whose default is also the zero value are harmless and were left alone.

Related: [[pando/fixes/config_update_zero_value_overwrite.md]], [[pando/changes/ipc_other_entrypoints_p4.md]].
