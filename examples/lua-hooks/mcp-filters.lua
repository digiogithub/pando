-- Focused examples for MCP server tool filtering.
--
-- These filters apply only to tools executed through configured MCP servers.
-- They do not automatically wrap Pando's built-in Go tools.

rawset(_G, "global-input", function(ctx)
    -- ctx fields include:
    -- server_name, tool_name, parameters, request_id, timestamp, filter_type
    if ctx.parameters == nil then
        return ctx
    end

    -- Example: trim obvious surrounding whitespace from every string argument.
    for key, value in pairs(ctx.parameters) do
        if type(value) == "string" then
            ctx.parameters[key] = string.gsub(value, "^%s*(.-)%s*$", "%1")
        end
    end

    return ctx
end)

rawset(_G, "global-output", function(ctx)
    -- ctx fields include:
    -- server_name, tool_name, result, request_id, timestamp, duration, filter_type
    if ctx.result == nil then
        return ctx
    end

    if type(ctx.result.output) == "string" and #ctx.result.output > 0 then
        ctx.result.output = ctx.result.output
    end

    return ctx
end)

rawset(_G, "github-docs-input", function(ctx)
    -- Matches an MCP server configured as "github_docs" or "github.docs".
    if ctx.parameters and ctx.parameters.query then
        ctx.parameters.query = ctx.parameters.query .. " language:go"
    end
    return ctx
end)

rawset(_G, "github-docs-output", function(ctx)
    if ctx.result and type(ctx.result.output) == "string" then
        ctx.result.output = "[github-docs filtered output]\n" .. ctx.result.output
    end
    return ctx
end)
