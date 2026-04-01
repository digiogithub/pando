package acp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/digiogithub/pando/internal/message"
)

// mockAgentService is a test double for AgentService.
type mockAgentService struct {
	runCalled        bool
	cancelCalled     bool
	runErr           error
	modelOverride    string
	modelOverrideErr error
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

func (m *mockAgentService) CurrentModelID() string {
	return "test-model"
}

func (m *mockAgentService) AvailableModels() []ACPModelInfo {
	return []ACPModelInfo{
		{ID: "test-model", Name: "Test Model"},
	}
}

func (m *mockAgentService) SetModelOverride(modelID string) error {
	m.modelOverride = modelID
	return m.modelOverrideErr
}

// mockSessionService is a test double for SessionService.
type mockSessionService struct {
	sessions map[string]ACPSessionInfo
	created  []string
	counter  int
}

func newMockSessionService() *mockSessionService {
	return &mockSessionService{
		sessions: make(map[string]ACPSessionInfo),
	}
}

func (m *mockSessionService) CreateSession(ctx context.Context, title string) (string, error) {
	m.counter++
	id := fmt.Sprintf("pando-session-%d", m.counter)
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

type mockPermissionService struct {
	autoApproved []string
	removed      []string
	registered   []string
	unregistered []string
	handlers     map[string]func(sessionID, toolName, description string) bool
}

func newMockPermissionService() *mockPermissionService {
	return &mockPermissionService{
		handlers: make(map[string]func(sessionID, toolName, description string) bool),
	}
}

func (m *mockPermissionService) AutoApproveSession(sessionID string) {
	m.autoApproved = append(m.autoApproved, sessionID)
}

func (m *mockPermissionService) RemoveAutoApproveSession(sessionID string) {
	m.removed = append(m.removed, sessionID)
}

func (m *mockPermissionService) RegisterSessionHandler(sessionID string, handler func(sessionID, toolName, description string) bool) {
	m.registered = append(m.registered, sessionID)
	m.handlers[sessionID] = handler
}

func (m *mockPermissionService) UnregisterSessionHandler(sessionID string) {
	m.unregistered = append(m.unregistered, sessionID)
	delete(m.handlers, sessionID)
}

func newTestPandoAgent() *PandoACPAgent {
	agent := &mockAgentService{}
	sessions := newMockSessionService()
	return NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), agent, sessions, nil)
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

	if !strings.HasPrefix(string(resp.SessionId), "pando-session-") {
		t.Errorf("Expected ACP session ID to be synchronized with Pando session ID, got %q", resp.SessionId)
	}

	// Session should now be registered
	agent.sessionsMu.RLock()
	_, exists := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()
	if !exists {
		t.Errorf("Session %s not found in agent sessions map", resp.SessionId)
	}
}

// TestPandoACPAgent_SetConnection_SynchronizesExistingSessions verifies that
// existing sessions receive updated agent connection references.
func TestPandoACPAgent_SetConnection_SynchronizesExistingSessions(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	agent.sessionsMu.RLock()
	sess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()

	if sess.HasAgentConnection() {
		t.Fatal("expected session to start without agent connection")
	}

	agent.SetConnection(&acpsdk.AgentSideConnection{})

	if !sess.HasAgentConnection() {
		t.Fatal("expected session connection to be synchronized after SetConnection")
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

// TestPandoACPAgent_LoadSession_SynchronizesExistingState verifies LoadSession
// does not replace an already registered ACP session and keeps in-memory mode/model.
func TestPandoACPAgent_LoadSession_SynchronizesExistingState(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	sessID := "sync-existing-session"
	agent.sessionService.(*mockSessionService).sessions[sessID] = ACPSessionInfo{ID: sessID, Title: "Sync"}

	_, err := agent.LoadSession(ctx, acpsdk.LoadSessionRequest{SessionId: acpsdk.SessionId(sessID), Cwd: "/tmp/a"})
	if err != nil {
		t.Fatalf("initial LoadSession failed: %v", err)
	}

	agent.sessionsMu.RLock()
	sess := agent.sessions[acpsdk.SessionId(sessID)]
	agent.sessionsMu.RUnlock()

	sess.SetMode("agent")
	sess.SetModel("test-model")

	_, err = agent.LoadSession(ctx, acpsdk.LoadSessionRequest{SessionId: acpsdk.SessionId(sessID), Cwd: "/tmp/b"})
	if err != nil {
		t.Fatalf("second LoadSession failed: %v", err)
	}

	agent.sessionsMu.RLock()
	reloaded := agent.sessions[acpsdk.SessionId(sessID)]
	agent.sessionsMu.RUnlock()

	if reloaded.Mode() != "agent" {
		t.Errorf("expected mode to be preserved, got %q", reloaded.Mode())
	}
	if reloaded.Model() != "test-model" {
		t.Errorf("expected model to be preserved, got %q", reloaded.Model())
	}
	if reloaded.WorkDir != "/tmp/b" {
		t.Errorf("expected workdir to be synchronized to latest value, got %q", reloaded.WorkDir)
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
		ModeId:    "ask",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()

	if acpSess.Mode() != "ask" {
		t.Errorf("Expected mode 'ask', got %q", acpSess.Mode())
	}
}

func TestPandoACPAgent_SetSessionMode_LogsNextPromptApplication(t *testing.T) {
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	mockAgent := &mockAgentService{}
	sessions := newMockSessionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", logger, mockAgent, sessions, nil)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: resp.SessionId,
		ModeId:    "ask",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	if !strings.Contains(logs.String(), "mode will take effect on next prompt") {
		t.Fatalf("expected SetSessionMode log to mention next prompt, got logs:\n%s", logs.String())
	}
}

func TestPandoACPAgent_Prompt_AgentModeAutoApprovesSession(t *testing.T) {
	mockAgent := &mockAgentService{}
	sessions := newMockSessionService()
	permSvc := newMockPermissionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, sessions, permSvc)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("hello")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if len(permSvc.autoApproved) != 1 || permSvc.autoApproved[0] != string(resp.SessionId) {
		t.Fatalf("expected session %s to be auto-approved once, got %+v", resp.SessionId, permSvc.autoApproved)
	}
	if len(permSvc.registered) != 0 {
		t.Fatalf("did not expect ask-mode handler registration, got %+v", permSvc.registered)
	}
}

func TestPandoACPAgent_Prompt_AskModeRegistersAndUnregistersHandler(t *testing.T) {
	mockAgent := &mockAgentService{}
	sessions := newMockSessionService()
	permSvc := newMockPermissionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, sessions, permSvc)
	agent.SetConnection(&acpsdk.AgentSideConnection{})
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: resp.SessionId,
		ModeId:    "ask",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	_, err = agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("hello")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if len(permSvc.removed) != 1 || permSvc.removed[0] != string(resp.SessionId) {
		t.Fatalf("expected auto-approve removal for session %s, got %+v", resp.SessionId, permSvc.removed)
	}
	if len(permSvc.registered) != 1 || permSvc.registered[0] != string(resp.SessionId) {
		t.Fatalf("expected handler registration for session %s, got %+v", resp.SessionId, permSvc.registered)
	}
	if len(permSvc.unregistered) != 1 || permSvc.unregistered[0] != string(resp.SessionId) {
		t.Fatalf("expected handler unregistration for session %s, got %+v", resp.SessionId, permSvc.unregistered)
	}
}

func TestPandoACPAgent_Prompt_AskModeWithoutConnectionLogsWarning(t *testing.T) {
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	mockAgent := &mockAgentService{}
	sessions := newMockSessionService()
	permSvc := newMockPermissionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", logger, mockAgent, sessions, permSvc)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: resp.SessionId,
		ModeId:    "ask",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	_, err = agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("hello")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if len(permSvc.registered) != 0 {
		t.Fatalf("did not expect handler registration without ACP connection, got %+v", permSvc.registered)
	}
	if !strings.Contains(logs.String(), "no ACP connection is available") {
		t.Fatalf("expected ask-mode warning about missing ACP connection, got logs:\n%s", logs.String())
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
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, sessions, nil)

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

// TestPandoACPAgent_ListSessions verifies historical session listing from the service.
func TestPandoACPAgent_ListSessions(t *testing.T) {
	sessions := newMockSessionService()
	sessions.sessions["s1"] = ACPSessionInfo{ID: "s1", Title: "First"}
	sessions.sessions["s2"] = ACPSessionInfo{ID: "s2", Title: "Second"}
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), &mockAgentService{}, sessions, nil)

	ctx := context.Background()
	list, err := agent.ListSessions(ctx)
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
