# Lua Hooks for Prompt Templates

Pando's template system provides Lua hooks at every stage of prompt composition, allowing dynamic modification without replacing template files.

## Available Hooks

### hook_template_section

Called for each template section during composition. Allows modification or removal of individual sections.

**Data fields:**
- `section_name` (string): Template path, e.g., `"base/workflow"`, `"capabilities/remembrances"`
- `section_content` (string): Rendered section content
- `agent_name` (string): Current agent name
- `provider` (string): Current LLM provider

**Return:** Modified ctx table. Set `section_content` to modify; set to `""` to remove the section.

```lua
function hook_template_section(ctx)
    if ctx.section_name == "base/workflow" then
        ctx.section_content = ctx.section_content .. "\n\n## Project Rules\n- Run 'make lint' before any commit"
    end
    return ctx
end
```

### hook_capability_check

Called for each capability during detection. Allows overriding whether a capability is included.

**Data fields:**
- `capability` (string): `"remembrances"`, `"orchestration"`, `"web_search"`, `"code_indexing"`, `"lsp"`
- `available` (boolean): Detected availability
- `agent_name` (string): Current agent name

**Return:** Modified ctx table. Set `available` to override detection.

```lua
function hook_capability_check(ctx)
    -- Disable web search in offline environments
    if ctx.capability == "web_search" then
        ctx.available = false
    end
    return ctx
end
```

### hook_provider_select

Called during provider template selection. Allows overriding which provider template is used.

**Data fields:**
- `provider` (string): Detected provider name
- `model` (string): Model name
- `agent_name` (string): Current agent name

**Return:** Modified ctx table. Set `provider_template` (string) to override the template path (e.g., `"providers/anthropic"`).

```lua
function hook_provider_select(ctx)
    -- Use Anthropic template for all providers when planning
    if ctx.agent_name == "planner" then
        ctx.provider_template = "providers/anthropic"
    end
    return ctx
end
```

### hook_prompt_compose

Called after all sections are rendered but before final assembly. Allows reordering, adding, or removing sections.

**Data fields:**
- `sections` (table): Array of `{name, content}` pairs
- `agent_name` (string): Current agent name
- `provider` (string): Current LLM provider

**Return:** Modified ctx table with updated `sections`.

```lua
function hook_prompt_compose(ctx)
    -- Add a custom section at the end
    local instructions = pando_load_file(".pando/team-instructions.md")
    if instructions then
        table.insert(ctx.sections, {name = "team", content = instructions})
    end
    return ctx
end
```

### hook_system_prompt (existing)

Called on the final composed prompt. Last chance to modify the entire prompt.

**Data fields:**
- `system_prompt` (string): Complete composed prompt
- `agent_name` (string): Current agent name
- `provider` (string): Current provider

```lua
function hook_system_prompt(ctx)
    ctx.system_prompt = ctx.system_prompt .. "\n\nIMPORTANT: Always respond in Spanish."
    return ctx
end
```

## Lua Helper Functions

These functions are available inside all hook functions:

| Function | Description | Example |
|----------|-------------|---------|
| `pando_get_config(key)` | Get config value | `pando_get_config("working_dir")` |
| `pando_get_git_status()` | Get git info table | `pando_get_git_status().is_repo` |
| `pando_load_file(path)` | Read file content | `pando_load_file(".pando/rules.md")` |
| `pando_list_mcp_servers()` | List MCP server names | `pando_list_mcp_servers()` |
| `pando_list_tools()` | List available tools | `pando_list_tools()` |

**Config keys:** `"working_dir"`, `"data_dir"`, `"debug"`

**Git status table:** `{is_repo = bool, working_dir = string}`

## Hook Execution Order

During prompt composition, hooks fire in this order:

1. `hook_provider_select` — Choose provider template
2. `hook_template_section` — For each section (identity, provider, agent, base sections...)
3. `hook_capability_check` — For each capability (remembrances, orchestration, web_search...)
4. `hook_template_section` — For each enabled capability section
5. `hook_template_section` — For context sections (git, project, skills, mcp)
6. `hook_prompt_compose` — Reorder/modify all sections
7. `hook_system_prompt` — Final prompt modification

## Configuration

Enable Lua hooks in `.pando.toml`:

```toml
[Lua]
Enabled = true
ScriptPath = ".pando/hooks.lua"
Timeout = "5s"
StrictMode = false
HotReload = true
```

See [lua-hooks-example.lua](lua-hooks-example.lua) for a complete example script.
