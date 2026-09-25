package acp

import (
	"context"
	"encoding/json"
	"log"
	"reflect"
	"testing"
	"time"

	acpsdk "github.com/madeindigio/acp-go-sdk"

	"github.com/digiogithub/pando/internal/config"
)

func TestConvertACPMCPServers(t *testing.T) {
	// Decoded from the wire so the SDK's union discrimination is exercised too.
	raw := `[
		{"name":"xcode-tools","command":"/usr/bin/mcpbridge","args":["--x"],"env":[{"name":"MCP_XCODE_PID","value":"18175"}]},
		{"type":"http","name":"remote","url":"https://h.example/mcp","headers":[{"name":"Authorization","value":"Bearer t"}]},
		{"type":"sse","name":"events","url":"https://s.example/sse","headers":[]}
	]`
	var servers []acpsdk.McpServer
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := ConvertACPMCPServers(servers)
	want := []SessionMCPServer{
		{Name: "xcode-tools", Type: SessionMCPStdio, Command: "/usr/bin/mcpbridge", Args: []string{"--x"}, Env: map[string]string{"MCP_XCODE_PID": "18175"}},
		{Name: "remote", Type: SessionMCPHTTP, URL: "https://h.example/mcp", Headers: map[string]string{"Authorization": "Bearer t"}},
		{Name: "events", Type: SessionMCPSSE, URL: "https://s.example/sse", Headers: map[string]string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConvertACPMCPServers:\n got %#v\nwant %#v", got, want)
	}

	configs := SessionMCPServerConfigs(got)
	if c := configs["xcode-tools"]; c.Type != config.MCPStdio || c.Command != "/usr/bin/mcpbridge" || !reflect.DeepEqual(c.Env, []string{"MCP_XCODE_PID=18175"}) {
		t.Fatalf("stdio config = %#v", c)
	}
	if c := configs["remote"]; c.Type != config.MCPStreamableHTTP || c.URL != "https://h.example/mcp" || c.Headers["Authorization"] != "Bearer t" {
		t.Fatalf("http config = %#v", c)
	}
	if c := configs["events"]; c.Type != config.MCPSse || c.URL != "https://s.example/sse" {
		t.Fatalf("sse config = %#v", c)
	}
}

func TestPandoACPAgent_AdvertisesHTTPAndSSEMCP(t *testing.T) {
	caps := newTestPandoAgent().GetCapabilities().McpCapabilities
	if !caps.Http || !caps.Sse {
		t.Fatalf("McpCapabilities = %+v, want http and sse", caps)
	}
}

func TestPandoACPAgent_NewSessionAttachesMCPAndPromptWaits(t *testing.T) {
	release := make(chan struct{})
	mockAgent := &mockAgentService{}
	mockAgent.mcpAttachFn = func(ctx context.Context, sessionID string, servers []SessionMCPServer) error {
		<-release // discovery still in progress
		return nil
	}
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, newMockSessionService(), nil)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd: "/tmp",
		McpServers: []acpsdk.McpServer{{Stdio: &acpsdk.McpServerStdio{
			Name: "xcode-tools", Command: "/usr/bin/mcpbridge", Env: []acpsdk.EnvVariable{{Name: "MCP_XCODE_PID", Value: "1"}},
		}}},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	sid := string(resp.SessionId)

	// session/new must not block on discovery.
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(release)
	}()

	if _, err := agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("hello")},
	}); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	mockAgent.mcpMu.Lock()
	trace := append([]string(nil), mockAgent.mcpTrace...)
	attached := mockAgent.mcpAttached[sid]
	mockAgent.mcpMu.Unlock()
	if want := []string{"attach:" + sid, "run:" + sid}; !reflect.DeepEqual(trace, want) {
		t.Fatalf("trace = %v, want %v (prompt must wait for the MCP attach)", trace, want)
	}
	if len(attached) != 1 || attached[0].Name != "xcode-tools" || attached[0].Env["MCP_XCODE_PID"] != "1" {
		t.Fatalf("attached servers = %#v", attached)
	}

	if _, err := agent.CloseSession(ctx, acpsdk.CloseSessionRequest{SessionId: resp.SessionId}); err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}
	mockAgent.mcpMu.Lock()
	detached := append([]string(nil), mockAgent.mcpDetached...)
	mockAgent.mcpMu.Unlock()
	if !reflect.DeepEqual(detached, []string{sid}) {
		t.Fatalf("detached = %v, want [%s]", detached, sid)
	}
}

func TestPandoACPAgent_PromptMCPWaitIsBounded(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	mockAgent := &mockAgentService{}
	mockAgent.mcpAttachFn = func(context.Context, string, []SessionMCPServer) error {
		<-block
		return nil
	}
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, newMockSessionService(), nil)
	resp, err := agent.NewSession(context.Background(), acpsdk.NewSessionRequest{
		Cwd:        "/tmp",
		McpServers: []acpsdk.McpServer{{Stdio: &acpsdk.McpServerStdio{Name: "slow", Command: "x"}}},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	acpSession, _ := agent.getSession(resp.SessionId)

	start := time.Now()
	agent.waitSessionMCPReady(context.Background(), acpSession, 50*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("wait not bounded: %v", elapsed)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	agent.waitSessionMCPReady(cancelled, acpSession, time.Hour)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("wait ignored ctx cancel: %v", elapsed)
	}
}

// TestSessionMCPServer_ToConfigStdioIsSandboxExempt is part of PANDO-US-0067:
// stdio servers handed over by an ACP client (Xcode, Zed, VS Code, ...) are
// trusted and commonly need IDE IPC the host sandbox would hide or block, so
// ToConfig must mark them NoSandbox regardless of the global sandbox policy.
func TestSessionMCPServer_ToConfigStdioIsSandboxExempt(t *testing.T) {
	s := SessionMCPServer{
		Name:    "xcode-tools",
		Type:    SessionMCPStdio,
		Command: "/usr/bin/mcpbridge",
		Args:    []string{"--x"},
		Env:     map[string]string{"MCP_XCODE_PID": "1"},
	}
	cfg := s.ToConfig()
	if cfg.Type != config.MCPStdio {
		t.Fatalf("Type = %v, want stdio", cfg.Type)
	}
	if !cfg.NoSandbox {
		t.Fatal("stdio servers handed over by the ACP client must be sandbox-exempt (NoSandbox=true)")
	}
	if !cfg.SandboxExempt() {
		t.Fatal("SandboxExempt() must be true for an ACP-provided stdio server")
	}
}

// TestSessionMCPServer_ToConfigHTTPAndSSENoSandboxUnset checks that the
// NoSandbox marking is specific to the stdio (local process) branch: HTTP and
// SSE servers are network clients, not local processes the host sandbox would
// ever wrap, so ToConfig leaves NoSandbox at its zero value for them.
func TestSessionMCPServer_ToConfigHTTPAndSSENoSandboxUnset(t *testing.T) {
	for _, s := range []SessionMCPServer{
		{Name: "remote", Type: SessionMCPHTTP, URL: "https://h.example/mcp"},
		{Name: "events", Type: SessionMCPSSE, URL: "https://s.example/sse"},
	} {
		cfg := s.ToConfig()
		if cfg.NoSandbox {
			t.Fatalf("%s: NoSandbox = true, want false (not a local process)", s.Type)
		}
	}
}
