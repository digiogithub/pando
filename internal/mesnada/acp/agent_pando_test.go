package acp

import (
	"context"
	"errors"
	"log"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/digiogithub/pando/internal/message"
)

// mockAgentService is a test double for AgentService.
type mockAgentService struct {
	runCalled    bool
	cancelCalled bool
	runErr       error
}

func (m *mockAgentService) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	m.runCalled = true
	if m.runErr != nil {
		return nil, m.runErr
	}
	ch := make(chan AgentEvent)
	close(ch)
	return ch, nil
}

func (m *mockAgentService) Cancel(sessionID string) {
	m.cancelCalled = true
}

// mockSessionService is a test double for SessionService.
type mockSessionService struct {
	sessions map[string]ACPSessionInfo
	created  []string
}

func newMockSessionService() *mockSessionService {
	return &mockSessionService{
		sessions: make(map[string]ACPSessionInfo),
	}
}

func (m *mockSessionService) CreateSession(ctx context.Context, title string) (string, error) {
	id := "pando-" + title
	m.sessions[id] = ACPSessionInfo{ID: id, Title: title}
	m.created = append(m.created, id)
	return id, nil
}

func (m *mockSessionService) GetSession(ctx context.Context, id string) (ACPSessionInfo, error) {
	s, ok := m.sessions[id]
	if !ok {
		return ACPSessionInfo{}, errors.New("session not found")
	}
	return s, nil
}

func (m *mockSessionService) ListSessions(ctx context.Context) ([]ACPSessionInfo, error) {
	result := make([]ACPSessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result, nil
}

func newTestPandoAgent() *PandoACPAgent {
	agent := &mockAgentService{}
	sessions := newMockSessionService()
	return NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), agent, sessions)
}

// TestPandoACPAgent_Initialize verifies the initialization response.
func TestPandoACPAgent_Initialize(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: 1,
		ClientInfo:      &acpsdk.Implementation{Name: "test-client", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if resp.AgentInfo == nil || resp.AgentInfo.Name != "pando" {
		t.Errorf("Expected agent name 'pando', got %v", resp.AgentInfo)
	}
	if !resp.AgentCapabilities.LoadSession {
		t.Error("Expected LoadSession capability to be true")
	}
}

// TestPandoACPAgent_NewSession verifies session creation.
func TestPandoACPAgent_NewSession(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	if resp.SessionId == "" {
		t.Error("Expected non-empty session ID")
	}

	// Session should now be registered
	agent.sessionsMu.RLock()
	_, exists := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()
	if !exists {
		t.Errorf("Session %s not found in agent sessions map", resp.SessionId)
	}
}

// TestPandoACPAgent_LoadSession_Found verifies loading an existing session.
func TestPandoACPAgent_LoadSession_Found(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	// Register a session in the mock service
	sessID := "existing-session-1"
	agent.sessionService.(*mockSessionService).sessions[sessID] = ACPSessionInfo{
		ID:    sessID,
		Title: "Test Session",
	}

	resp, err := agent.LoadSession(ctx, acpsdk.LoadSessionRequest{
		SessionId: acpsdk.SessionId(sessID),
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	_ = resp

	// ACP session should be registered
	agent.sessionsMu.RLock()
	_, exists := agent.sessions[acpsdk.SessionId(sessID)]
	agent.sessionsMu.RUnlock()
	if !exists {
		t.Errorf("Expected ACP session %s to be registered after LoadSession", sessID)
	}
}

// TestPandoACPAgent_LoadSession_NotFound verifies error when session doesn't exist.
func TestPandoACPAgent_LoadSession_NotFound(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	_, err := agent.LoadSession(ctx, acpsdk.LoadSessionRequest{
		SessionId: acpsdk.SessionId("nonexistent-session"),
		Cwd:       "/tmp",
	})
	if err == nil {
		t.Error("Expected error for non-existent session, got nil")
	}
}

// TestPandoACPAgent_LoadSession_CustomCwd verifies that Cwd override is applied.
func TestPandoACPAgent_LoadSession_CustomCwd(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	sessID := "session-cwd-test"
	agent.sessionService.(*mockSessionService).sessions[sessID] = ACPSessionInfo{
		ID:    sessID,
		Title: "CWD Test",
	}

	customCwd := "/custom/work/dir"
	_, err := agent.LoadSession(ctx, acpsdk.LoadSessionRequest{
		SessionId: acpsdk.SessionId(sessID),
		Cwd:       customCwd,
	})
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess, exists := agent.sessions[acpsdk.SessionId(sessID)]
	agent.sessionsMu.RUnlock()
	if !exists {
		t.Fatal("Session not registered")
	}
	if acpSess.WorkDir != customCwd {
		t.Errorf("Expected WorkDir %q, got %q", customCwd, acpSess.WorkDir)
	}
}

// TestPandoACPAgent_LoadSession_DefaultCwd verifies fallback to agent workdir.
func TestPandoACPAgent_LoadSession_DefaultCwd(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	sessID := "session-default-cwd"
	agent.sessionService.(*mockSessionService).sessions[sessID] = ACPSessionInfo{
		ID:    sessID,
		Title: "Default CWD",
	}

	_, err := agent.LoadSession(ctx, acpsdk.LoadSessionRequest{
		SessionId: acpsdk.SessionId(sessID),
		Cwd:       "", // empty → use agent default
	})
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[acpsdk.SessionId(sessID)]
	agent.sessionsMu.RUnlock()
	if acpSess.WorkDir != "/tmp" {
		t.Errorf("Expected default WorkDir /tmp, got %q", acpSess.WorkDir)
	}
}

// TestPandoACPAgent_SetSessionMode verifies mode updates.
func TestPandoACPAgent_SetSessionMode(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	// Create session first
	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: resp.SessionId,
		ModeId:    "auto",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()

	if acpSess.Mode() != "auto" {
		t.Errorf("Expected mode 'auto', got %q", acpSess.Mode())
	}
}

// TestPandoACPAgent_SetSessionModel verifies model updates.
func TestPandoACPAgent_SetSessionModel(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.SetSessionModel(ctx, acpsdk.SetSessionModelRequest{
		SessionId: resp.SessionId,
		ModelId:   "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("SetSessionModel failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()

	if acpSess.Model() != "claude-sonnet-4-6" {
		t.Errorf("Expected model 'claude-sonnet-4-6', got %q", acpSess.Model())
	}
}

// TestPandoACPAgent_Cancel_Existing verifies cancellation of a known session.
func TestPandoACPAgent_Cancel_Existing(t *testing.T) {
	mockAgent := &mockAgentService{}
	sessions := newMockSessionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, sessions)

	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	err = agent.Cancel(ctx, acpsdk.CancelNotification{SessionId: resp.SessionId})
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	if !mockAgent.cancelCalled {
		t.Error("Expected Cancel to be called on the agent service")
	}
}

// TestPandoACPAgent_Cancel_Unknown verifies cancelling a non-existent session is a no-op.
func TestPandoACPAgent_Cancel_Unknown(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	err := agent.Cancel(ctx, acpsdk.CancelNotification{SessionId: "nonexistent"})
	if err != nil {
		t.Fatalf("Cancel on unknown session should not error, got: %v", err)
	}
}

// TestPandoACPAgent_SessionService_ListSessions verifies the session service adapter.
func TestPandoACPAgent_SessionService_ListSessions(t *testing.T) {
	sessions := newMockSessionService()
	sessions.sessions["s1"] = ACPSessionInfo{ID: "s1", Title: "First"}
	sessions.sessions["s2"] = ACPSessionInfo{ID: "s2", Title: "Second"}

	ctx := context.Background()
	list, err := sessions.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(list))
	}
}

// TestPandoACPAgent_GetVersion verifies version is returned correctly.
func TestPandoACPAgent_GetVersion(t *testing.T) {
	agent := newTestPandoAgent()
	if agent.GetVersion() != "1.0.0-test" {
		t.Errorf("Expected version '1.0.0-test', got %q", agent.GetVersion())
	}
}

// TestPandoACPAgent_GetCapabilities verifies LoadSession capability is advertised.
func TestPandoACPAgent_GetCapabilities(t *testing.T) {
	agent := newTestPandoAgent()
	caps := agent.GetCapabilities()
	if !caps.LoadSession {
		t.Error("PandoACPAgent should advertise LoadSession: true")
	}
}
