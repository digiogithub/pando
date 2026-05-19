package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeJSONInputRepairsCommonLLMJSONIssues(t *testing.T) {
	input := "```json\n{query: 'search tools', max_results: 5,}\n```"

	normalized, err := NormalizeJSONInput(input)
	require.NoError(t, err)
	require.JSONEq(t, `{"query":"search tools","max_results":5}`, normalized)
}

func TestDecodeToolInputRepairsNestedParameters(t *testing.T) {
	var req struct {
		ToolName   string                 `json:"tool_name"`
		Parameters map[string]interface{} `json:"parameters"`
	}

	err := DecodeToolInput(`{tool_name:'demo', parameters:{path:'/tmp/test', recursive:true,},}`, &req)
	require.NoError(t, err)
	require.Equal(t, "demo", req.ToolName)
	require.Equal(t, "/tmp/test", req.Parameters["path"])
	require.Equal(t, true, req.Parameters["recursive"])
}

func TestMustNormalizeToolCallInputDefaultsEmptyInput(t *testing.T) {
	call, err := MustNormalizeToolCallInput(ToolCall{Input: "   "})
	require.NoError(t, err)
	require.Equal(t, "{}", call.Input)
}
