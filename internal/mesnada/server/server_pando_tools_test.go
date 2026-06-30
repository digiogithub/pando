package server

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/stretchr/testify/require"
)

type stubTool struct {
	info llmtools.ToolInfo
	run  func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error)
}

const cacheTriggerLineCount = 350

func (t stubTool) Info() llmtools.ToolInfo { return t.info }

func (t stubTool) Run(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
	return t.run(ctx, call)
}

func TestGetToolDefinitionsFromPandoTools(t *testing.T) {
	srv := New(Config{
		PandoTools: []llmtools.BaseTool{
			stubTool{
				info: llmtools.ToolInfo{
					Name:        "echo_context",
					Description: "Echoes session context",
					Parameters: map[string]any{
						"value": map[string]any{"type": "string"},
					},
					Required: []string{"value"},
				},
				run: func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
					return llmtools.NewTextResponse(call.Input), nil
				},
			},
		},
	})

	definitions := srv.getToolDefinitions()
	require.Len(t, definitions, 1)
	require.Equal(t, "echo_context", definitions[0].Name)
	require.Equal(t, []string{"value"}, definitions[0].InputSchema["required"])
}

func TestHandleToolsCallSupportsCacheReadAcrossSession(t *testing.T) {
	largeTool := stubTool{
		info: llmtools.ToolInfo{
			Name:        "large_output",
			Description: "Returns a large body",
			Parameters:  map[string]any{},
		},
		run: func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
			var sb strings.Builder
			for i := 1; i <= cacheTriggerLineCount; i++ {
				sb.WriteString("line ")
				sb.WriteString(strings.Repeat("x", 50))
				sb.WriteString("\n")
			}
			return llmtools.NewTextResponse(sb.String()), nil
		},
	}

	srv := New(Config{
		PandoTools: []llmtools.BaseTool{
			largeTool,
			llmtools.NewCacheReadTool(),
		},
		UseStdio: true,
	})
	session := srv.getOrCreateSession("session-1")

	firstReq := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "req-1",
		Method:  "tools/call",
		Params:  mustJSON(t, map[string]any{"name": "large_output", "arguments": map[string]any{}}),
	}
	firstResp := srv.handleToolsCall(context.Background(), session, firstReq)
	require.Nil(t, firstResp.Error)

	firstResult, ok := firstResp.Result.(map[string]interface{})
	require.True(t, ok)
	content := firstResult["content"].([]map[string]interface{})[0]["text"].(string)
	require.Contains(t, content, "[Response cached:")

	cacheID := extractCacheID(t, content)
	secondReq := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "req-2",
		Method:  "tools/call",
		Params: mustJSON(t, map[string]any{
			"name": "cache_read",
			"arguments": map[string]any{
				"cache_id": cacheID,
				"offset":   200,
			},
		}),
	}
	secondResp := srv.handleToolsCall(context.Background(), session, secondReq)
	require.Nil(t, secondResp.Error)

	secondResult, ok := secondResp.Result.(map[string]interface{})
	require.True(t, ok)
	page := secondResult["content"].([]map[string]interface{})[0]["text"].(string)
	require.Contains(t, page, "[Cache page:")
	require.Contains(t, page, "201|")
}

func TestHandleRequestNotificationsGetNoResponse(t *testing.T) {
	srv := New(Config{UseStdio: true})
	session := srv.getOrCreateSession("notif-sess")

	// A notification carries no "id" and must never receive a response, even
	// for the (previously mismatched) initialized notification. Strict MCP
	// clients abort the handshake if the server replies to it.
	for _, method := range []string{"notifications/initialized", "notifications/cancelled", "notifications/unknown"} {
		req := &JSONRPCRequest{JSONRPC: "2.0", Method: method}
		resp := srv.handleRequest(context.Background(), session, req)
		require.Nil(t, resp, "notification %q must not produce a response", method)
	}

	// A request (with an id) for an unknown method still gets a Method-not-found error.
	req := &JSONRPCRequest{JSONRPC: "2.0", ID: 7, Method: "does/not/exist"}
	resp := srv.handleRequest(context.Background(), session, req)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32601, resp.Error.Code)
}

func TestNegotiateProtocolVersion(t *testing.T) {
	require.Equal(t, "2025-06-18", negotiateProtocolVersion(mustJSON(t, map[string]any{"protocolVersion": "2025-06-18"})))
	require.Equal(t, "2025-03-26", negotiateProtocolVersion(mustJSON(t, map[string]any{"protocolVersion": "2025-03-26"})))
	// Unknown/blank requested versions fall back to the server default.
	require.Equal(t, mcpVersion, negotiateProtocolVersion(mustJSON(t, map[string]any{"protocolVersion": "1999-01-01"})))
	require.Equal(t, mcpVersion, negotiateProtocolVersion(nil))
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func extractCacheID(t *testing.T, content string) string {
	t.Helper()
	match := regexp.MustCompile(`cache_id: "([^"]+)"`).FindStringSubmatch(content)
	require.Len(t, match, 2)
	return match[1]
}
