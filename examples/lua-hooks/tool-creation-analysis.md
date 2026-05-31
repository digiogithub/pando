# Can Pando create tools from Lua?

## Short answer

Partially: there is now an MVP path for Lua-defined tools, but it is still intentionally limited.

## What exists today

The current Lua runtime supports:

- lifecycle hooks through `FilterManager.ExecuteHook(...)`
- MCP tool input filters through `FilterManager.ApplyInputFilter(...)`
- MCP tool output filters through `FilterManager.ApplyOutputFilter(...)`
- prompt helper functions registered with `RegisterPromptFunctions(...)`
- MVP tool registration via `pando_register_tool({...})`
- MVP tool execution via `pando_run_tool(ctx)`

One implementation detail matters for MCP filters: Pando resolves filter names with dashes, such as `fetch-input` or `global-output`, and looks them up as Lua globals. In Lua source, these should be assigned with `rawset(_G, "fetch-input", function(...) ... end)` because `function fetch-input(...)` is invalid syntax, and some editors/parsers also complain about direct `_G["..."] = function(...)` forms.

The native tool registry is still built in Go code. Tools are instantiated with Go constructors such as:

- `tools.NewBashTool(...)`
- `tools.NewFetchTool(...)`
- `tools.NewWriteTool(...)`
- `agent.NewMcpTool(...)`
- `tools.NewLuaTools(...)` for the current MVP adapter layer

The MVP now covers the basics through:

- `pando_register_tool({...})`
- automatic exposure as agent tools
- a generic `pando_run_tool(ctx)` dispatcher

What is still missing is a full-featured production design with richer outputs, stronger capability controls, and first-class integration across every subsystem.

## Current MVP constraints

- tool names must start with `lua_`
- execution uses a single dispatcher function: `pando_run_tool(ctx)`
- the tool should return a Lua table with `content`, `output`, or `error`
- advanced helper APIs are not yet exposed specifically for Lua tools
- permission-gated side effects are not yet implemented as a dedicated Lua capability model

## Why the original architecture did not allow it

The runtime model was originally separated like this:

1. **Tool registration** happens in Go when the agent starts.
2. **Lua execution** is used as a customization layer over prompts and MCP traffic.
3. **Tool metadata** (`ToolInfo`) and execution (`Run`) are implemented by Go structs that satisfy the internal tool interface.

Because of that, Lua could modify requests and responses for MCP-backed tools, but it could not contribute a new `BaseTool` implementation on its own. The MVP bridges that gap with a Go adapter over Lua-registered definitions.

## How a fuller Lua tool system could evolve

### 1. Richer registration schema

Extend:

```lua
pando_register_tool({
  name = "lua_repo_summary",
  description = "Summarize repository-local metadata",
  parameters = {
    path = { type = "string", description = "Path to inspect" }
  },
  required = { "path" }
})
```

with stronger schema validation and richer types.

### 2. Dedicated tool handlers

The MVP uses one dispatcher:

```lua
function pando_run_tool(ctx)
    -- branch on ctx.tool_name
end
```

A fuller design could also allow per-tool handlers such as:

- `tool_lua_repo_summary(ctx)`
- or explicit handler names declared during registration.

### 3. Safe helper APIs to Lua tools

Lua tools need limited, explicit capabilities, for example:

- read-only filesystem helpers,
- prompt/config helpers,
- optional outbound HTTP helper,
- optional permission-gated shell helper.

These helpers should be capability-scoped, not unrestricted.

### 4. Broader integration

Lua tools should become visible in:

- system prompts,
- MCP server exposure if desired,
- tool listings in the UI.

### 5. Preserve permission and audit behavior

Every Lua tool should still pass through:

- permission checks,
- logging,
- session cache interception,
- timeout handling,
- strict-mode error handling.

## Recommended implementation notes

- Keep Lua tools **opt-in** behind config if the feature expands further.
- Require explicit JSON-schema-like parameter definitions.
- Enforce per-tool execution timeouts.
- Do not expose arbitrary shell or network primitives without permission checks.
- Keep the `lua_` namespace to avoid collisions with built-in tools.

## Suggested next milestones

1. structured metadata outputs
2. explicit capability helpers for Lua tools
3. permission-gated side effects
4. MCP/server/UI integration
5. better diagnostics and tests
