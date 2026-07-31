-- Figma MCP Lua filters for Pando
--
-- Pando's Lua engine looks for functions named `<server>-input` and
-- `<server>-output` (server name with _ and . replaced by -). For a
-- server named "figma" in TOML, the functions are `figma-input` and
-- `figma-output`.
--
-- These filters run before/after every Figma MCP tool call. Use them to:
--   - validate or sanitize file_key / node_id parameters
--   - truncate large responses before they consume context
--   - log tool usage for auditing
--   - inject default parameters (e.g. geometric_bounds)

-- ── Input filter ─────────────────────────────────────────────────────────
-- Called before each Figma tool invocation. `ctx` is a Lua table with:
--   server_name, tool_name, parameters, request_id, session_id, timestamp
--
-- Return a table with:
--   modified = true/false
--   data     = the (possibly modified) parameters table
--   logs     = array of log strings (optional)

function figma-input(ctx)
    local params = ctx.parameters or {}
    local logs = {}

    -- Ensure file_key is a plain string (Figma URLs include the key after
    -- /design/; some LLMs paste the full URL).
    if params.file_key and type(params.file_key) == "string" then
        local original = params.file_key
        -- Extract the UUID-like key from a Figma URL if present.
        local key = original:match("[%a%d]+%-[%a%d]+%-[%a%d]+%-[%a%d]+%-[%a%d]+")
        if key and key ~= original then
            params.file_key = key
            table.insert(logs, "figma-input: extracted file_key " .. key .. " from URL")
        end
    end

    -- Default depth for get_figma_data if not specified.
    if ctx.tool_name == "get_figma_data" and params.depth == nil then
        params.depth = 2
        table.insert(logs, "figma-input: defaulting depth=2 for get_figma_data")
    end

    return {
        modified = #logs > 0,
        data = params,
        logs = logs,
    }
end

-- ── Output filter ────────────────────────────────────────────────────────
-- Called after each Figma tool returns. `ctx` has the same fields plus
-- `result` (the tool output) and `duration` (ms).

function figma-output(ctx)
    local result = ctx.result or {}
    local logs = {}

    -- Truncate image data URIs in the result to avoid flooding the context.
    -- Figma's download_figma_images returns base64 images that can be large.
    if ctx.tool_name == "download_figma_images" and result.content then
        for _, item in ipairs(result.content) do
            if type(item) == "table" and item.text and #item.text > 10000 then
                local original_len = #item.text
                item.text = item.text:sub(1, 200) .. "...[truncated, " .. original_len .. " chars]"
                table.insert(logs, "figma-output: truncated image data from " .. original_len .. " chars")
            end
        end
    end

    -- Log tool execution time for slow calls.
    if ctx.duration and ctx.duration > 3000 then
        table.insert(logs, string.format("figma-output: slow call %s took %dms", ctx.tool_name, ctx.duration))
    end

    return {
        modified = #logs > 0,
        data = result,
        logs = logs,
    }
end

-- ── Global fallback (optional) ───────────────────────────────────────────
-- If a server has no specific filter, Pando falls back to `global-input` /
-- `global-output`. Uncomment to apply to all MCP servers.

-- function global-input(ctx)
--     return { modified = false, data = ctx.parameters or {}, logs = {} }
-- end
--
-- function global-output(ctx)
--     return { modified = false, data = ctx.result or {}, logs = {} }
-- end
