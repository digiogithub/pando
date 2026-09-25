-- +goose Up
-- +goose StatementBegin

-- mcp_server_fingerprints: one row per MCP server whose catalog is stored in
-- mcp_tool_registry, recording a hash of the server's raw configuration at the
-- time the catalog was discovered (PANDO-US-0068). On startup the gateway
-- skips connecting to a server whose catalog rows exist and whose fingerprint
-- still matches: every new connection to Xcode's mcpbridge raises an "Allow
-- ... to access Xcode?" prompt, and Xcode relaunches `pando acp` often.
CREATE TABLE IF NOT EXISTS mcp_server_fingerprints (
    server_name TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS mcp_server_fingerprints;

-- +goose StatementEnd
