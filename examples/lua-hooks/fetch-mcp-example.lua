-- Example for an MCP server named "fetch".
--
-- Important: this example applies only if you configured an MCP server whose
-- configured server name is exactly "fetch".
--
-- It does NOT currently intercept Pando's native built-in fetch tool from
-- internal/llm/tools/fetch.go.

rawset(_G, "fetch-input", function(ctx)
    if ctx.parameters == nil then
        return ctx
    end

    -- Force HTTPS when the upstream MCP fetch server is used.
    if type(ctx.parameters.url) == "string" then
        ctx.parameters.url = string.gsub(ctx.parameters.url, "^http://", "https://")
    end

    -- Provide a default output format if absent.
    if ctx.parameters.format == nil or ctx.parameters.format == "" then
        ctx.parameters.format = "markdown"
    end

    return ctx
end)

rawset(_G, "fetch-output", function(ctx)
    if ctx.result == nil then
        return ctx
    end

    if type(ctx.result.output) == "string" then
        ctx.result.output = "# MCP Fetch Result\n\n" .. ctx.result.output
    end

    return ctx
end)
