# Can Pando create tools from Lua?

## Short answer

No, not today.

## What exists today

The current Lua runtime supports:

- lifecycle hooks through `FilterManager.ExecuteHook(...)`
- MCP tool input filters through `FilterManager.ApplyInputFilter(...)`
- MCP tool output filters through `FilterManager.ApplyOutputFilter(...)`
- prompt helper functions registered with `RegisterPromptFunctions(...)`

One implementation detail matters for MCP filters: Pando resolves filter names with dashes, such as `fetch-input` or `global-output`, and looks them up as Lua globals. In Lua source, these should be assigned with `rawset(_G, "fetch-input", function(...) ... end)` because `function fetch-input(...)` is invalid syntax, and some editors/parsers also complain about direct `_G["..."] = function(...)` forms.

The native tool registry is still built in Go code. Tools are instantiated with Go constructors such as:

- `tools.NewBashTool(...)`
- `tools.NewFetchTool(...)`
- `tools.NewWriteTool(...)`
- `agent.NewMcpTool(...)`

There is no Lua API that:

- declares a tool schema,
- registers a tool name/description,
- exposes that tool to the LLM,
- dispatches tool execution back into Lua.

## Why the current architecture does not allow it

The current runtime model separates concerns like this:

1. **Tool registration** happens in Go when the agent starts.
2. **Lua execution** is used as a customization layer over prompts and MCP traffic.
3. **Tool metadata** (`ToolInfo`) and execution (`Run`) are implemented by Go structs that satisfy the internal tool interface.

Because of that, Lua can modify requests and responses for MCP-backed tools, but it cannot contribute a new `BaseTool` implementation on its own.

## How Lua-defined tools could be implemented

A practical design would be:

### 1. Add a Lua tool registry in the Lua engine

Expose a function like:

```lua
pando_register_tool({
  name = "my_lua_tool",
  description = "Summarize project-local metadata",
  parameters = {
    path = { type = "string", description = "Path to inspect" }
  },
  required = { "path" }
})
```

The Lua engine would persist these definitions after loading the script.

### 2. Add a Go adapter implementing `tools.BaseTool`

Create a Go type like `luaToolAdapter` with:

- `Info() ToolInfo`
- `Run(ctx context.Context, call ToolCall) (ToolResponse, error)`

`Run(...)` would call back into Lua using a naming convention such as:

- `tool_my_lua_tool(ctx)`
- or a generic dispatcher like `pando_run_tool(name, ctx)`

### 3. Expose safe helper APIs to Lua tools

Lua tools would need limited, explicit capabilities, for example:

- read-only filesystem helpers,
- prompt/config helpers,
- optional outbound HTTP helper,
- optional permission-gated shell helper.

These helpers should be capability-scoped, not unrestricted.

### 4. Integrate Lua-defined tools into tool discovery

Where the agent currently aggregates Go tools, append Lua tool adapters after loading the script.

That would allow Lua tools to appear in:

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

- Keep Lua tools **opt-in** behind config, e.g. `Lua.EnableToolRegistration = true`.
- Require explicit JSON-schema-like parameter definitions.
- Enforce per-tool execution timeouts.
- Do not expose arbitrary shell or network primitives without permission checks.
- Consider a separate namespace like `lua_<name>` to avoid collisions with built-in tools.

## Suggested first milestone

Implement **read-only Lua tools** first:

- registration API,
- tool metadata exposure,
- Lua dispatcher,
- text-only output.

Then later add structured output and optional permission-gated side effects.
