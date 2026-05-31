# Lua hook examples for Pando

This folder contains example Lua scripts that show the current customization surface exposed by Pando's Lua engine.

## What Pando supports today

Pando currently supports two Lua customization mechanisms:

1. **Lifecycle hooks** via `hook_<name>(ctx)` functions.
2. **MCP input/output filters** via `<server-name>-input(ctx)` and `<server-name>-output(ctx)` names, with `global-input(ctx)` and `global-output(ctx)` fallbacks.

It also exposes helper functions inside Lua prompt hooks:

- `pando_get_config(key)`
- `pando_get_git_status()`
- `pando_load_file(path)`
- `pando_list_mcp_servers()`
- `pando_list_tools()`

## Important limitation

At the time of writing, Pando **does not create new tools dynamically from Lua**. Lua can:

- modify prompt composition,
- intercept MCP server tool inputs,
- intercept MCP server tool outputs,
- react to session and conversation lifecycle events.

But Lua **cannot register a new first-class tool** in the native tool registry.

See `tool-creation-analysis.md` for implementation notes on how this could be added in the future.

## Files in this folder

- `hooks.lua`: complete example combining lifecycle hooks, prompt hooks, and MCP filters.
- `prompt-hooks.lua`: focused examples for system prompt and template customization.
- `mcp-filters.lua`: focused examples for MCP input/output filtering.
- `fetch-mcp-example.lua`: example of filtering an MCP server named `fetch`.
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
