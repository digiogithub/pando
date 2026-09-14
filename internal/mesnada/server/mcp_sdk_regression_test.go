package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
)

// headerInjectingRoundTripper attaches a fixed set of headers to every
// outgoing request. The go-sdk's StreamableClientTransport has no built-in
// "static headers" option (only an HTTPClient), so this is how a real client
// would carry a bearer token.
type headerInjectingRoundTripper struct {
	base   http.RoundTripper
	header http.Header
}

func (t headerInjectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	for k, values := range t.header {
		for _, v := range values {
			cloned.Header.Add(k, v)
		}
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

// sessionIDCapturingRoundTripper records the Mcp-Session-Id response header
// seen on every response, so the test can assert it is minted once and never
// changes mid-session.
type sessionIDCapturingRoundTripper struct {
	base   http.RoundTripper
	record *[]string
}

func (t sessionIDCapturingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err == nil && resp != nil {
		if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
			*t.record = append(*t.record, id)
		}
	}
	return resp, err
}

// echoRegressionTool is a minimal native tool exercised by tools/call below.
type echoRegressionTool struct{}

func (echoRegressionTool) Info() llmtools.ToolInfo {
	return llmtools.ToolInfo{
		Name:        "echo_regression",
		Description: "Echoes back its input value",
		Parameters: map[string]any{
			"value": map[string]any{"type": "string"},
		},
		Required: []string{"value"},
	}
}

func (echoRegressionTool) Run(_ context.Context, call llmtools.ToolCall) (llmtools.ToolResponse, error) {
	return llmtools.NewTextResponse(call.Input), nil
}

// TestMCPGoSDKClient_FullSessionOverBearerToken drives a real
// github.com/modelcontextprotocol/go-sdk v1.4.1 streamable client against
// this package's HTTP transport, with a bearer token configured, through
// initialize -> tools/list -> tools/call. It also pins two behaviours the
// SDK client depends on that PANDO-EP-0006's middleware must not disturb:
//   - Close() sends a DELETE /mcp whose HTTP status the client ignores.
//   - The Mcp-Session-Id is minted once and echoed unchanged on every later
//     response within the session (the SDK client errors on a mismatch).
//
// See TestMCPGoSDKClient_GetMCPReturns405 for the third pinned behaviour
// (standalone-SSE GET /mcp returning 405).
func TestMCPGoSDKClient_FullSessionOverBearerToken(t *testing.T) {
	const token = "regression-test-token"

	srv := New(Config{
		UseStdio: false,
		Addr:     "127.0.0.1:0",
		Token:    token,
		PandoTools: []llmtools.BaseTool{
			echoRegressionTool{},
		},
	})
	require.NotNil(t, srv.httpServer)

	testServer := httptest.NewServer(srv.httpServer.Handler)
	defer testServer.Close()

	var sessionIDs []string
	httpClient := &http.Client{
		Transport: sessionIDCapturingRoundTripper{
			record: &sessionIDs,
			base: headerInjectingRoundTripper{
				header: http.Header{"Authorization": []string{"Bearer " + token}},
			},
		},
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   testServer.URL + "/mcp",
		HTTPClient: httpClient,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "pando-regression-test", Version: "1.0.0"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "initialize must succeed with a matching bearer token")
	require.NotNil(t, session.InitializeResult())

	toolsResult, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	require.NotEmpty(t, toolsResult.Tools)
	found := false
	for _, tl := range toolsResult.Tools {
		if tl.Name == "echo_regression" {
			found = true
		}
	}
	require.True(t, found, "tools/list must include the registered echo_regression tool")

	callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo_regression",
		Arguments: map[string]any{"value": "hello"},
	})
	require.NoError(t, err)
	require.False(t, callResult.IsError)
	require.NotEmpty(t, callResult.Content)

	require.NoError(t, session.Close(),
		"Close()'s DELETE /mcp must not surface an error regardless of its HTTP status")

	// The session id is minted once (on the initialize response) and echoed
	// unchanged on every later response in the session.
	require.NotEmpty(t, sessionIDs)
	first := sessionIDs[0]
	require.NotEmpty(t, first)
	for i, id := range sessionIDs {
		require.Equal(t, first, id, "Mcp-Session-Id changed at response #%d: got %q, want %q", i, id, first)
	}
}

// TestMCPGoSDKClient_GetMCPReturns405 pins that a bare authenticated GET
// /mcp returns 405, which is what the go-sdk client's standalone-SSE
// negotiation relies on to treat "no standalone SSE stream" as expected
// rather than fatal (see handleMCP).
func TestMCPGoSDKClient_GetMCPReturns405(t *testing.T) {
	srv := New(Config{UseStdio: false, Addr: "127.0.0.1:0", Token: "regression-test-token"})
	require.NotNil(t, srv.httpServer)

	testServer := httptest.NewServer(srv.httpServer.Handler)
	defer testServer.Close()

	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer regression-test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
