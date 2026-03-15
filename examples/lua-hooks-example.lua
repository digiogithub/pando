-- Pando Lua Hooks Example
-- Place this file path in your config under lua.script_path
--
-- All hook functions receive a context table (ctx) and must return a table
-- (or nil for informational hooks where no modification is needed).
-- If a hook returns nil or errors, Pando continues with original data unchanged.

-- =============================================================================
-- Hook 1: hook_system_prompt
-- Fired when building the system prompt for the agent.
-- ctx fields: system_prompt (string), agent_name (string), provider (string)
-- Return ctx with modified system_prompt to override the prompt.
-- =============================================================================
function hook_system_prompt(ctx)
    -- Append custom rules to the system prompt
    ctx.system_prompt = ctx.system_prompt .. "\n\n## Custom Rules\n- Always respond in English.\n- Prefer concise answers."
    return ctx
end

-- =============================================================================
-- Hook 2: hook_session_start
-- Fired when a new session is created.
-- ctx fields: session_id (string), title (string), created_at (string RFC3339)
-- Informational — return value is ignored.
-- =============================================================================
function hook_session_start(ctx)
    -- Example: log session creation (implement pando_log if needed)
    -- pando_log("New session started: " .. ctx.session_id .. " - " .. ctx.title)
    return nil
end

-- =============================================================================
-- Hook 3: hook_session_restore
-- Fired when an existing session is loaded/restored.
-- ctx fields: session_id (string), title (string), message_count (number)
-- Informational — return value is ignored.
-- =============================================================================
function hook_session_restore(ctx)
    -- Example: log session restore
    -- pando_log("Session restored: " .. ctx.session_id .. " (" .. ctx.message_count .. " messages)")
    return nil
end

-- =============================================================================
-- Hook 4: hook_conversation_start
-- Fired before building the messages list for each generation.
-- ctx fields: session_id (string), is_new_session (bool), message_count (number)
-- Return ctx with injected_context (string) to prepend context to user message.
-- =============================================================================
function hook_conversation_start(ctx)
    if ctx.is_new_session then
        -- Inject project context for new sessions only
        ctx.injected_context = "Project: Pando — Go AI assistant. Follow Go best practices and keep changes minimal."
    end
    return ctx
end

-- =============================================================================
-- Hook 5: hook_user_prompt
-- Fired before creating the user message in the database.
-- ctx fields: session_id (string), user_content (string)
-- Return ctx with modified_content (string) to override the user message.
-- =============================================================================
function hook_user_prompt(ctx)
    -- Example: strip accidental credential leaks (simplistic example)
    -- local sanitized = string.gsub(ctx.user_content, "sk%-[a-zA-Z0-9]+", "[REDACTED]")
    -- if sanitized ~= ctx.user_content then
    --     ctx.modified_content = sanitized
    -- end
    return ctx
end

-- =============================================================================
-- Hook 6: hook_agent_response_finish
-- Fired when the agent finishes generating a response (EventComplete).
-- ctx fields: session_id (string), finish_reason (string),
--             input_tokens (number), output_tokens (number)
-- Informational — return value is ignored.
-- =============================================================================
function hook_agent_response_finish(ctx)
    -- Example: alert on expensive sessions (>10k tokens)
    -- if ctx.output_tokens > 10000 then
    --     pando_log("Warning: large response for session " .. ctx.session_id ..
    --               " (" .. ctx.output_tokens .. " output tokens)")
    -- end
    return nil
end

-- =============================================================================
-- MCP Tool Filters (from Phase 1)
-- Format: <server-name>-input / <server-name>-output
-- =============================================================================

-- Example: filter inputs for a server named "myserver"
_G["myserver-input"] = function(ctx)
    -- ctx.parameters contains the tool call arguments
    -- Modify and return them to override
    return ctx.parameters
end

-- Example: filter outputs for "myserver"
_G["myserver-output"] = function(ctx)
    -- ctx.result contains the tool output data
    -- Modify and return to override
    return ctx.result
end

-- Global fallback input filter (applied when no server-specific filter exists)
-- function global-input(ctx)
--     return ctx.parameters
-- end
