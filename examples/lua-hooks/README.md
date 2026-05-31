# Lua hook examples for Pando

This folder contains example Lua scripts that show the current customization surface exposed by Pando's Lua engine.

## What Pando supports today

Pando currently supports three Lua customization mechanisms:

1. **Lifecycle hooks** via `hook_<name>(ctx)` functions.
2. **MCP input/output filters** via `<server-name>-input(ctx)` and `<server-name>-output(ctx)` names, with `global-input(ctx)` and `global-output(ctx)` fallbacks.
3. **MVP Lua tools** via `pando_register_tool({...})` plus a `pando_run_tool(ctx)` dispatcher.

It also exposes helper functions inside Lua prompt hooks:

- `pando_get_config(key)`
- `pando_get_git_status()`
- `pando_load_file(path)`
- `pando_list_mcp_servers()`
- `pando_list_tools()`

## Important limitation

At the time of writing, Pando supports an **MVP** for Lua-defined tools. Lua can:

- modify prompt composition,
- intercept MCP server tool inputs,
- intercept MCP server tool outputs,
- react to session and conversation lifecycle events,
- register simple text-returning tools through `pando_register_tool({...})`.

Current MVP limits for Lua tools:

- tool names must start with `lua_`,
- execution is routed through a single `pando_run_tool(ctx)` dispatcher,
- tools currently return text or error through a Lua table,
- advanced capabilities like direct shell/network helper APIs are not part of this MVP.

See `tool-creation-analysis.md` for the broader design beyond the MVP.

## Files in this folder

- `hooks.lua`: complete example combining lifecycle hooks, prompt hooks, and MCP filters.
- `prompt-hooks.lua`: focused examples for system prompt and template customization.
- `mcp-filters.lua`: focused examples for MCP input/output filtering.
- `fetch-mcp-example.lua`: example of filtering an MCP server named `fetch`.
- `lua-tools-mvp.lua`: MVP example of declaring Lua-defined tools with `pando_register_tool`.
- `tool-creation-analysis.md`: analysis of current support and an implementation proposal for Lua-defined tools.

## Example config

```toml
[Lua]
Enabled = true
ScriptPath = "examples/lua-hooks/hooks.lua"
Timeout = "5s"
StrictMode = false
HotReload = true
```

## Naming rules

### Lifecycle hooks

Use functions named like:

- `hook_system_prompt`
- `hook_template_section`
- `hook_provider_select`
- `hook_prompt_compose`
- `hook_session_start`
- `hook_session_end`
- `hook_conversation_start`
- `hook_user_prompt`
- `hook_agent_response_finish`
- `hook_global`

### MCP filters

Pando resolves MCP filter names like:

- `<server-name>-input`
- `<server-name>-output`
- `global-input`
- `global-output`

Because `-` is not a valid identifier character in Lua, do not write `function fetch-output(ctx)`.

The most parser-friendly way is to assign a function with `rawset`:

```lua
rawset(_G, "github-docs-input", function(ctx)
    return ctx
end)
```

Pando normalizes server names by replacing `_` and `.` with `-` before resolving the Lua global name.

So an MCP server configured as `github_docs` is matched by the Lua global `"github-docs-input"`.

### MVP Lua tools

Register tools with:

```lua
pando_register_tool({
    name = "lua_echo",
    description = "Echoes text",
    parameters = {
        text = { type = "string", description = "Text to echo" }
    },
    required = { "text" }
})
```

And dispatch execution with:

```lua
function pando_run_tool(ctx)
    if ctx.tool_name == "lua_echo" then
        return { content = tostring((ctx.arguments or {}).text or "") }
    end
    return { error = "unknown tool" }
end
```
