---
created_at: 2026-07-23T07:04:03.866298119Z
updated_at: 2026-07-23T07:04:03.866298119Z
tags:
    - fix
    - mesnada
    - copilot
    - mcp
---
# Fix: Copilot subagent MCP config used wrong schema (servers vs mcpServers)

## Problem
Copilot-engine subagent tasks with an MCP config failed immediately:
```
Invalid MCP server configuration in --additional-mcp-config: mcpServers: Required
```
`ConvertMCPConfigForCopilot` in `internal/mesnada/agent/mcpconfig.go` serialized the
config with `mcpconv.RenderVSCode(cfg)`, producing `{"servers": {...}}` (VS Code
`.vscode/mcp.json` schema). GitHub Copilot CLI expects the canonical
`{"mcpServers": {...}}` schema (same as `--additional-mcp-config`).

## Root cause
Historical: copilot was originally the primary engine and always defined its own MCP
config file. When the primary engine changed to pando, the default conversion path for
copilot subagents was not updated. `ConvertMCPConfigForCopilot` (a pando-only wrapper
not present in standalone `madeindigio/mesnada`) picked `RenderVSCode` by mistake.

## Evidence that canonical is correct for copilot
- `CanonicalConfig.MCPServers json:"mcpServers"` vs `VSCodeConfig.Servers json:"servers"`
  (`internal/mesnada/mcpconv/mcpconv.go` lines 49-50, 116-117).
- `RenderByFormat` already maps format `"copilot"` to the raw canonical `cfg`
  (mcpServers), NOT VS Code, and skips relative-path absolutization — so mcpconfig.go
  contradicted the package's own contract.
- Standalone mesnada `CopilotSpawner.buildArgs` passes the MCP config file directly
  (canonical) with no conversion.

## Change
`internal/mesnada/agent/mcpconfig.go`, `ConvertMCPConfigForCopilot`:
replaced `mcpconv.WriteJSONFile(tempDir, "copilot-mcp-config.json", mcpconv.RenderVSCode(cfg))`
with the existing helper `WriteCanonicalConfigToFile(cfg, tempDir, "copilot-mcp-config.json")`,
which writes the canonical `mcpServers` schema. Chosen over the PR's inline
`WriteJSONFile(..., cfg)` because the helper already exists in the same package and is
semantically explicit, consistent with `RenderByFormat`'s "copilot" case.

`RenderVSCode` remains used by `RenderByFormat` (FormatVSCode) — not removed.

## Verification
- `go build ./internal/mesnada/...` OK.
- Confirmed no `_test.go` in `internal/mesnada` depends on RenderVSCode/copilot "servers" schema.
- Struct tags confirm canonical output = `{"mcpServers": {...}}`.

## Files
- `internal/mesnada/agent/mcpconfig.go` — `ConvertMCPConfigForCopilot`

Source description: `/home/sevir/Descargas/PR-fix-copilot-mcp-config.md`.
