# Plan: JSON→TOON Conversion for Tool Responses

## Objective
Convert all JSON responses from MCP tools (direct, gateway, favorites) and internal tools to TOON format before returning them to the AI model. TOON (Token-Oriented Object Notation) is more token-efficient than JSON for LLM prompts.

## Architecture Decision
**Single interception point** in the agent's tool result processing loop (`internal/llm/agent/agent.go:streamAndHandleEvents()` ~line 513). This covers ALL tools without modifying each one individually.

## Phases

### Phase 1 - Foundation (fact: toon_plan_fase1_foundation)
- Add `github.com/toon-format/toon-go` dependency
- Create `internal/toonconv/toonconv.go` with `ConvertIfJSON(content string) string`
- Uses `json.Valid()` for detection, `toon.MarshalString()` for conversion
- Thread-safe, stateless function

### Phase 2 - Agent Integration (fact: toon_plan_fase2_agent_integration)
- Modify `agent.go:streamAndHandleEvents()` at tool result assignment
- Apply `toonconv.ConvertIfJSON()` on `toolResult.Content` before creating `message.ToolResult`
- Covers: internal tools, MCP direct, MCP gateway proxy, gateway favorites, remembrances, mesnada

### Phase 3 - Configuration (fact: toon_plan_fase3_configuration)
- Add `ToonConversion bool` to config (default: true)
- Agent checks config before applying conversion

### Phase 4 - Tests (fact: toon_plan_fase4_tests)
- Unit tests for toonconv package (valid JSON, invalid JSON, empty, arrays, nested, primitives)
- Integration verification

## Key Files
- `internal/toonconv/toonconv.go` (NEW)
- `internal/toonconv/toonconv_test.go` (NEW)
- `internal/llm/agent/agent.go` (MODIFY - line ~513)
- `internal/config/config.go` (MODIFY - add ToonConversion field)
- `go.mod` (MODIFY - add toon-go dependency)

## Reference
- Library: `github.com/toon-format/toon-go` (same as remembrances-mcp)
- TOON spec: https://toonformat.dev/guide/format-overview.html
- Remembrances usage: `pkg/mcp_tools/toon_utils.go` → `MarshalTOON()` function