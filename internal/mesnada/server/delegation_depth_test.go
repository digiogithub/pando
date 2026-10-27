package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/mesnada/depth"
	"github.com/stretchr/testify/require"
)

// An HTTP MCP client (a subagent) declares its delegation level in a header;
// the tool context carries it so spawn tools count one level deeper.
func TestHandleMCPPropagatesDelegationDepthHeader(t *testing.T) {
	seen := -1
	srv := New(Config{
		PandoTools: []llmtools.BaseTool{stubTool{
			info: llmtools.ToolInfo{Name: "probe_depth", Parameters: map[string]any{}},
			run: func(ctx context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
				if v, ok := depth.FromContext(ctx); ok {
					seen = v
				}
				return llmtools.NewTextResponse(fmt.Sprint(seen)), nil
			},
		}},
	})

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"probe_depth","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set(depth.HeaderName, "2")
	rec := httptest.NewRecorder()
	srv.handleMCP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 2, seen)
}
