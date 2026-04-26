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
			for i := 1; i <= 350; i++ {
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
