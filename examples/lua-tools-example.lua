-- Pando Lua Tools Example
-- Define custom tools in the pando_tools table.
-- Each tool has: description, parameters, and a run function.
-- Tools are exposed to the LLM with the prefix "lua_" (e.g. lua_git_log).
--
-- To use tools that call shell commands (io.popen), enable the io module:
--   [lua]
--   tools_enabled    = true
--   allowed_modules  = ["io"]
--
-- Place this file path in your config:
--   [lua]
--   script_path = "/path/to/lua-tools-example.lua"

pando_tools = {

    -- Tool: git-log
    -- Returns the last N git commit log entries for the current repository.
    ["git-log"] = {
        description = "Get the last N git commit log entries for the current repository",
        parameters = {
            count = {
                type        = "integer",
                description = "Number of log entries to show (default: 10)",
                required    = false,
            },
        },
        run = function(params)
            local n = params.count or 10
            local handle = io.popen("git log --oneline -" .. tostring(n) .. " 2>&1")
            if not handle then
                return nil, "failed to run git log"
            end
            local result = handle:read("*a")
            handle:close()
            return result
        end,
    },

    -- Tool: count-go-lines
    -- Counts total lines of Go source code in the current directory tree,
    -- excluding the vendor directory.
    ["count-go-lines"] = {
        description = "Count total lines of Go code in the current directory (excluding vendor/)",
        parameters  = {},
        run = function(params)
            local handle = io.popen(
                "find . -name '*.go' -not -path '*/vendor/*' | xargs wc -l 2>/dev/null | tail -1"
            )
            if not handle then
                return nil, "failed to count lines"
            end
            local result = handle:read("*a")
            handle:close()
            local count = result:match("%s*(%d+)")
            return "Go lines of code: " .. (count or "unknown")
        end,
    },

    -- Tool: get-env
    -- Reads an environment variable value (read-only, safe).
    ["get-env"] = {
        description = "Get the value of an environment variable",
        parameters = {
            name = {
                type        = "string",
                description = "Environment variable name",
                required    = true,
            },
        },
        run = function(params)
            if not params.name or params.name == "" then
                return nil, "name parameter is required"
            end
            local value = os.getenv(params.name)
            if value == nil then
                return "Environment variable '" .. params.name .. "' is not set"
            end
            return params.name .. "=" .. value
        end,
    },

}

-- You can combine tools with hooks in the same script.
-- See examples/lua-hooks-example.lua for lifecycle hook examples.
