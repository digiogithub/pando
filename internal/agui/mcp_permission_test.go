package agui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// PANDO-US-0031: an MCP tool called from an AG-UI run must go through the
// adapter's OWN permission service.
//
// The whole point of the defect was that GetMcpTools cached permission-bound
// tools, so these tests exercise the real thing end to end: a real MCP server
// (in process, over the streamable-HTTP transport the configuration uses), the
// real internal/mcpclient connection, the real agent.GetMcpTools, the real
// permission service the Runtime owns, the real HITL handler and the real
// run/suspend/resume lifecycle. Only the model is faked -- exactly as the
// PANDO-US-0010 fixture agent fakes it, and for the same reason: no
// credentials, no network, deterministic.

const fixtureMCPServerName = "fixture"

// startFixtureMCPServer runs a one-tool MCP server in this process and returns
// the streamable-HTTP endpoint Pando's MCP client can connect to.
func startFixtureMCPServer(t *testing.T) string {
	t.Helper()
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: fixtureMCPServerName, Version: "v1"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "ping", Description: "Answers pong"},
		func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}},
			}, nil, nil
		})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts.URL
}

// useFixtureMCPConfig points the process configuration at the fixture server
// with the gateway off -- the configuration that keeps "<server>_<tool>" names
// so the [AGUI] Tools allow-list can see them, and the one the defect was
// reported against.
func useFixtureMCPConfig(t *testing.T, endpoint string) {
	t.Helper()
	prev := config.Get()
	config.SetForTests(&config.Config{
		WorkingDir: t.TempDir(),
		MCPServers: map[string]config.MCPServer{
			fixtureMCPServerName: {Type: config.MCPStreamableHTTP, URL: endpoint},
		},
	})
	agent.ResetMcpToolsCache()
	t.Cleanup(func() {
		agent.ResetMcpToolsCache()
		config.SetForTests(prev)
	})
}

// mcpRunnerAgentService is a fake model that calls one real MCP tool, obtained
// the way the agent pool obtains it (agent.GetMcpTools with the adapter's own
// permission service), and reports the call and its result as agent events.
type mcpRunnerAgentService struct {
	*fakeAgentService
	perms    permission.Service
	toolName string
}

func (f *mcpRunnerAgentService) Run(ctx context.Context, sessionID, _ string, _ ...message.Attachment) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent, 4)
	go func() {
		defer close(ch)
		toolCtx := context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
		toolCtx = context.WithValue(toolCtx, tools.MessageIDContextKey, "msg-1")

		var target tools.BaseTool
		for _, candidate := range agent.GetMcpTools(toolCtx, f.perms) {
			if candidate.Info().Name == f.toolName {
				target = candidate
				break
			}
		}
		if target == nil {
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeError, Error: fmt.Errorf("MCP tool %q was not discovered", f.toolName)}
			return
		}

		call := tools.ToolCall{ID: "call-1", Name: f.toolName, Input: "{}"}
		ch <- agent.AgentEvent{
			Type:     agent.AgentEventTypeToolCall,
			ToolCall: &message.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input, Finished: true},
		}

		resp, err := target.Run(toolCtx, call)
		if err != nil {
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeError, Error: err}
			return
		}
		// A real agent abandons the turn when its context is gone rather than
		// reporting the tool's fail-closed denial as a result.
		if ctx.Err() != nil {
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeError, Error: ctx.Err()}
			return
		}
		ch <- agent.AgentEvent{
			Type: agent.AgentEventTypeToolResult,
			ToolResult: &message.ToolResult{
				ToolCallID: call.ID, Name: call.Name, Content: resp.Content, IsError: resp.IsError,
			},
		}
		ch <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	}()
	return ch, nil
}

// newMCPRuntime builds a Runtime whose pooled "coder" agent calls the fixture
// MCP tool through the adapter's own permission service.
func newMCPRuntime(t *testing.T, cfg Config) *Runtime {
	t.Helper()
	endpoint := startFixtureMCPServer(t)
	useFixtureMCPConfig(t, endpoint)

	db := newThreadDB(t)
	r, _, _ := newThreadTestRuntime(t, db)
	r.cfg = cfg
	r.cfg.AgentPoolSize = defaultPoolSize
	r.cfg.AgentPoolTTL = defaultPoolTTL
	ctx, cancel := context.WithCancel(context.Background())
	r.baseCtx, r.cancel = ctx, cancel
	t.Cleanup(cancel)
	r.pool = newAgentPool(r.deps, r.cfg, r.perms, nil, r.pending)
	r.pool.entries["coder"] = &poolEntry{svc: &mcpRunnerAgentService{
		fakeAgentService: newFakeAgentService(),
		perms:            r.perms,
		toolName:         fixtureMCPServerName + "_ping",
	}, lastUsed: time.Now()}
	return r
}

func mcpTestConfig() Config {
	cfg := testConfig()
	cfg.HumanInTheLoop = true
	return cfg
}

// newResumeRequest answers a suspended run's synthetic permission call.
func newResumeRequest(threadID, runID, callID, content string) *http.Request {
	body := fmt.Sprintf(
		`{"threadId":%q,"runId":%q,"messages":[{"id":"m1","role":"user","content":"go"},{"id":"m2","role":"tool","toolCallId":%q,"content":%q}]}`,
		threadID, runID, callID, content)
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("agent", "coder")
	return req
}

// permissionCallID digs the synthetic permission tool call's id out of an SSE body.
func permissionCallID(t *testing.T, body string) string {
	t.Helper()
	marker := `"toolCallName":"` + permissionToolName + `"`
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("no %s tool call in the stream: %s", permissionToolName, body)
	}
	frameStart := strings.LastIndex(body[:idx], "data: ")
	frame := body[frameStart:]
	key := `"toolCallId":"`
	start := strings.Index(frame, key)
	if start < 0 {
		t.Fatalf("the permission tool call carries no id: %s", frame)
	}
	rest := frame[start+len(key):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("malformed tool call id: %s", frame)
	}
	return rest[:end]
}

// TestMCPToolAsksTheAdapterForApprovalAndResumes is the PANDO-US-0031
// acceptance criterion: with the gateway off and AutoApprove false, a run
// calling "<server>_<tool>" emits pando_permission_request, and a trailing
// {"approved":true} tool message lets it finish with TOOL_CALL_RESULT and
// RUN_FINISHED{outcome:"success"}. Before the fix the call blocked forever on
// the TUI's permission service, which the startup cache had baked in.
func TestMCPToolAsksTheAdapterForApprovalAndResumes(t *testing.T) {
	r := newMCPRuntime(t, mcpTestConfig())

	rec := httptest.NewRecorder()
	r.handleRun(rec, newRunRequest("t-mcp", "run-1", "call the fixture tool"))
	body := rec.Body.String()

	if !strings.Contains(body, permissionToolName) {
		t.Fatalf("the MCP tool never asked the adapter for approval: %s", body)
	}
	if !strings.Contains(body, `"outcome":"interrupt"`) {
		t.Fatalf("the run did not suspend on the approval: %s", body)
	}
	callID := permissionCallID(t, body)

	resume := httptest.NewRecorder()
	r.handleRun(resume, newResumeRequest("t-mcp", "run-2", callID, `{"approved":true}`))
	resumed := resume.Body.String()

	if !strings.Contains(resumed, string(EventToolCallResult)) {
		t.Fatalf("the approved MCP call produced no TOOL_CALL_RESULT: %s", resumed)
	}
	if !strings.Contains(resumed, "pong") {
		t.Fatalf("the MCP server's answer never reached the client: %s", resumed)
	}
	if !strings.Contains(resumed, `"outcome":"success"`) {
		t.Fatalf("the resumed run did not finish successfully: %s", resumed)
	}
}

// TestMCPToolWithAutoApproveNeverInterrupts is the AutoApprove half of the
// story: the same run completes in one request, with no prompt at all. This is
// the case that used to hang identically to the others -- the tell that the
// adapter's service was not the one being asked.
func TestMCPToolWithAutoApproveNeverInterrupts(t *testing.T) {
	cfg := mcpTestConfig()
	cfg.AutoApprove = true
	r := newMCPRuntime(t, cfg)

	rec := httptest.NewRecorder()
	r.handleRun(rec, newRunRequest("t-mcp-auto", "run-1", "call the fixture tool"))
	body := rec.Body.String()

	if strings.Contains(body, permissionToolName) {
		t.Fatalf("auto-approve must not prompt the client: %s", body)
	}
	if !strings.Contains(body, string(EventToolCallResult)) || !strings.Contains(body, "pong") {
		t.Fatalf("the auto-approved MCP call produced no result: %s", body)
	}
	if !strings.Contains(body, `"outcome":"success"`) {
		t.Fatalf("the run did not finish successfully: %s", body)
	}
}

// TestMCPToolPermissionFailsClosedWhenTheRunEnds is the fail-closed criterion:
// nobody answers the prompt and the run's context ends, so the run must end
// with RUN_ERROR rather than stranding the tool goroutine (and, with
// MaxConcurrentRuns, the whole adapter) forever. The deadlines here are short
// on purpose: a regression fails fast instead of hanging CI.
func TestMCPToolPermissionFailsClosedWhenTheRunEnds(t *testing.T) {
	r := newMCPRuntime(t, mcpTestConfig())

	rec := httptest.NewRecorder()
	r.handleRun(rec, newRunRequest("t-mcp-timeout", "run-1", "call the fixture tool"))
	if !strings.Contains(rec.Body.String(), permissionToolName) {
		t.Fatalf("the MCP tool never asked for approval: %s", rec.Body.String())
	}

	run, ok := r.runs.get("t-mcp-timeout")
	if !ok {
		t.Fatal("the suspended run should still be registered")
	}

	// Nobody answers; the adapter goes away underneath the run.
	r.cancel()

	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the abandoned permission request hung the run instead of failing closed")
	}

	events, _ := run.buffer.snapshot()
	var sawError bool
	for _, ev := range events {
		if ev.EventType() == EventRunError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("the abandoned run did not end with RUN_ERROR: %v", events)
	}
}

// TestAGUIAllowListStillFiltersDirectMCPToolNames keeps the PANDO-US-0011
// guarantee alive across this change: the tools GetMcpTools now builds per
// caller still carry their "<server>_<tool>" names, so the [AGUI] Tools
// allow-list can still see and filter them.
func TestAGUIAllowListStillFiltersDirectMCPToolNames(t *testing.T) {
	endpoint := startFixtureMCPServer(t)
	useFixtureMCPConfig(t, endpoint)

	mcpTools := agent.GetMcpTools(context.Background(), permission.NewPermissionService())
	if len(mcpTools) == 0 {
		t.Fatal("the fixture MCP server advertised no tools")
	}
	if mcpTools[0].Info().Name != fixtureMCPServerName+"_ping" {
		t.Fatalf("the direct registration lost its <server>_<tool> name: %q", mcpTools[0].Info().Name)
	}

	kept := filterAGUITools(mcpTools, []string{fixtureMCPServerName + "_*"}, nil, true)
	if len(kept) != len(mcpTools) {
		t.Fatalf("the allow-list dropped a matching MCP tool: %d of %d kept", len(kept), len(mcpTools))
	}
	if dropped := filterAGUITools(mcpTools, []string{"something_else"}, nil, true); len(dropped) != 0 {
		t.Fatalf("a non-matching allow-list must drop every MCP tool, kept %d", len(dropped))
	}
}
