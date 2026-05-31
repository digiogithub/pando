-- Complete Lua customization example for Pando.
--
-- This script shows the current hook/filter surface supported by the runtime:
-- - lifecycle hooks
-- - prompt composition hooks
-- - MCP input/output filters
--
-- Note: Lua currently customizes existing behavior; it does not register new
-- first-class Pando tools by itself.

local function append_line(text, line)
    if text == nil or text == "" then
        return line
    end
    return text .. "\n" .. line
end

function hook_global(ctx)
    -- Fallback hook called when a specific hook is not defined.
    -- Keep it conservative to avoid surprising behavior.
    return ctx
end

function hook_session_start(ctx)
    -- Available fields typically include:
    -- session_id, title, created_at
    return ctx
end

function hook_session_end(ctx)
    -- Example: attach a lightweight marker for observability.
    ctx.session_closed_by_lua = true
    return ctx
end

function hook_conversation_start(ctx)
    local git = pando_get_git_status()
    if git and git.is_repo then
        ctx.injected_context = "Repository detected at: " .. (git.working_dir or "unknown")
    end
    return ctx
end

function hook_user_prompt(ctx)
    -- Example: normalize a team shortcut before the model sees it.
    if ctx.user_content and string.find(ctx.user_content, "@brief") then
        ctx.modified_content = string.gsub(ctx.user_content, "@brief",
            "Please keep the answer concise and action-oriented.")
    end
    return ctx
end

function hook_agent_response_finish(ctx)
    -- Available fields include session_id, finish_reason, input_tokens, output_tokens.
    return ctx
end

function hook_template_section(ctx)
    -- Add a local policy section into workflow-related template sections.
    if ctx.section_name == "base/workflow" then
        local team_rules = pando_load_file(".pando/team-rules.md")
        if team_rules and team_rules ~= "" then
            ctx.section_content = ctx.section_content .. "\n\n## Team Rules\n" .. team_rules
        end
    end

    -- Remove sections by setting empty content.
    if ctx.section_name == "capabilities/web_search" then
        local offline = pando_get_config("debug")
        if offline == false then
            -- Keep as-is in normal mode; this line just documents the pattern.
        end
    end

    return ctx
end

function hook_capability_check(ctx)
    -- Example: disable web search for a specific agent profile.
    if ctx.agent_name == "reviewer" and ctx.capability == "web_search" then
        ctx.available = false
    end
    return ctx
end

function hook_provider_select(ctx)
    -- Example: route a planner-style agent to a specific provider template.
    if ctx.agent_name == "planner" then
        ctx.provider_template = "providers/anthropic"
    end
    return ctx
end

function hook_prompt_compose(ctx)
    local servers = pando_list_mcp_servers()
    if servers and #servers > 0 then
        local summary = "## Connected MCP Servers\n"
        for i = 1, #servers do
            summary = append_line(summary, "- " .. tostring(servers[i]))
        end
        table.insert(ctx.sections, { name = "lua/mcp-summary", content = summary })
    end
    return ctx
end

function hook_system_prompt(ctx)
    local tools = pando_list_tools()
    local tool_count = 0
    if tools then
        tool_count = #tools
    end

    ctx.system_prompt = ctx.system_prompt
        .. "\n\n## Lua Runtime Notes"
        .. "\n- Lua hooks are enabled for this session."
        .. "\n- Visible tool count at prompt-build time: " .. tostring(tool_count)
        .. "\n- Prefer existing project conventions over generic solutions."

    return ctx
end

rawset(_G, "global-input", function(ctx)
    -- Fallback MCP input filter.
    -- Runs for any MCP server without a more specific <server>-input function.
    if ctx.parameters == nil then
        return ctx
    end

    -- Example: annotate requests in a non-invasive way.
    ctx.parameters._lua_filtered = true
    return ctx
end)

rawset(_G, "global-output", function(ctx)
    -- Fallback MCP output filter.
    if ctx.result == nil then
        return ctx
    end

    if ctx.result.output and type(ctx.result.output) == "string" then
        ctx.result.output = ctx.result.output
    end
    return ctx
end)

rawset(_G, "fetch-input", function(ctx)
    -- This function is only used when there is an MCP server configured with the
    -- server name "fetch". It does NOT intercept Pando's built-in fetch tool.
    if ctx.parameters and ctx.parameters.url then
        if string.find(ctx.parameters.url, "http://") == 1 then
            ctx.parameters.url = string.gsub(ctx.parameters.url, "^http://", "https://")
        end
    end
    return ctx
end)

rawset(_G, "fetch-output", function(ctx)
    if ctx.result and ctx.result.output and type(ctx.result.output) == "string" then
        ctx.result.output = "[Filtered by Lua MCP output hook]\n" .. ctx.result.output
    end
    return ctx
end)
