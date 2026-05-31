-- MVP example: declare and execute a Lua-defined tool.
--
-- Current MVP contract:
-- 1. Register tools with pando_register_tool(...)
-- 2. Implement a global dispatcher called pando_run_tool(ctx)
-- 3. Return a table with one of: content, output, or error

pando_register_tool({
    name = "lua_echo",
    description = "Echoes the provided text back to the caller.",
    parameters = {
        text = {
            type = "string",
            description = "Text to echo back"
        }
    },
    required = { "text" }
})

pando_register_tool({
    name = "lua_repo_summary",
    description = "Returns a small repository summary using prompt helper functions.",
    parameters = {},
    required = {}
})

function pando_run_tool(ctx)
    local tool_name = ctx.tool_name
    local arguments = ctx.arguments or {}

    if tool_name == "lua_echo" then
        local text = arguments.text or ""
        return {
            content = "lua_echo: " .. tostring(text)
        }
    end

    if tool_name == "lua_repo_summary" then
        local git = pando_get_git_status()
        local tools = pando_list_tools() or {}
        local repo_path = "not-a-repo"
        if git and git.is_repo then
            repo_path = git.working_dir or "unknown"
        end
        return {
            content = "repo=" .. repo_path .. ", visible_tools=" .. tostring(#tools)
        }
    end

    return {
        error = "unknown lua tool: " .. tostring(tool_name)
    }
end
