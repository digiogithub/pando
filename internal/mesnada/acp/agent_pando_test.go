package acp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	llmmodels "github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// syncBuffer is a goroutine-safe bytes.Buffer wrapper for tests whose logger is
// written by an async agent goroutine (e.g. sendAvailableCommandsUpdate) while
// the test reads it. A bare bytes.Buffer is not safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestToDisplayPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		cwd  string
		want string
	}{
		{name: "relative to cwd", path: "/workspace/project/file.go", cwd: "/workspace/project", want: "file.go"},
		{name: "nested relative to cwd", path: "/workspace/project/internal/acp.go", cwd: "/workspace/project", want: "internal/acp.go"},
		{name: "outside cwd keeps absolute", path: "/other/place/file.go", cwd: "/workspace/project", want: "/other/place/file.go"},
		{name: "empty cwd keeps path", path: "/workspace/project/file.go", cwd: "", want: "/workspace/project/file.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toDisplayPath(tt.path, tt.cwd); got != tt.want {
				t.Fatalf("toDisplayPath(%q, %q) = %q, want %q", tt.path, tt.cwd, got, tt.want)
			}
		})
	}
}

func TestToolDisplayTitle(t *testing.T) {
	cwd := "/workspace/project"

	readInput := map[string]interface{}{"file_path": "/workspace/project/file.go", "offset": float64(10), "limit": float64(5)}
	if got := toolDisplayTitle("read", readInput, cwd); got != "Read file.go (10 - 14)" {
		t.Fatalf("unexpected read title: %q", got)
	}

	writeInput := map[string]interface{}{"file_path": "/workspace/project/internal/out.go"}
	if got := toolDisplayTitle("write", writeInput, cwd); got != "Write internal/out.go" {
		t.Fatalf("unexpected write title: %q", got)
	}

	editInput := map[string]interface{}{"path": "/workspace/project/internal/edit.go"}
	if got := toolDisplayTitle("edit", editInput, cwd); got != "Edit internal/edit.go" {
		t.Fatalf("unexpected edit title: %q", got)
	}

	bashInput := map[string]interface{}{"command": "go test ./internal/api"}
	if got := toolDisplayTitle("bash", bashInput, cwd); got != "go test ./internal/api" {
		t.Fatalf("unexpected bash title: %q", got)
	}

	grepInput := map[string]interface{}{
		"pattern":     "toolInfo",
		"path":        "/workspace/project/internal",
		"output_mode": "count",
		"head_limit":  float64(20),
		"type":        "go",
		"multiline":   true,
	}
	if got := toolDisplayTitle("grep", grepInput, cwd); got != "grep -c | head -20 --type=go -P \"toolInfo\" internal" {
		t.Fatalf("unexpected grep title: %q", got)
	}

	todoInput := map[string]interface{}{"todos": []interface{}{
		map[string]interface{}{"content": "first task"},
		map[string]interface{}{"content": "second task"},
	}}
	if got := toolDisplayTitle("TodoWrite", todoInput, cwd); got != "Update TODOs: first task, second task" {
		t.Fatalf("unexpected todo title: %q", got)
	}
}

func TestMessageChunkHelpersSetMessageID(t *testing.T) {
	userUpdate := updateUserMessageTextWithID("hello", "user-msg-1")
	if userUpdate.UserMessageChunk == nil || userUpdate.UserMessageChunk.MessageId == nil || *userUpdate.UserMessageChunk.MessageId != "user-msg-1" {
		t.Fatalf("expected user messageId to be set, got %#v", userUpdate.UserMessageChunk)
	}

	agentUpdate := updateAgentMessageTextWithID("reply", "agent-msg-1")
	if agentUpdate.AgentMessageChunk == nil || agentUpdate.AgentMessageChunk.MessageId == nil || *agentUpdate.AgentMessageChunk.MessageId != "agent-msg-1" {
		t.Fatalf("expected agent messageId to be set, got %#v", agentUpdate.AgentMessageChunk)
	}

	thoughtUpdate := updateAgentThoughtTextWithID("thinking", "agent-msg-1")
	if thoughtUpdate.AgentThoughtChunk == nil || thoughtUpdate.AgentThoughtChunk.MessageId == nil || *thoughtUpdate.AgentThoughtChunk.MessageId != "agent-msg-1" {
		t.Fatalf("expected thought messageId to be set, got %#v", thoughtUpdate.AgentThoughtChunk)
	}
}

func TestGroupedThinkingStateFlushesOnCharThreshold(t *testing.T) {
	var state groupedThinkingState
	now := time.Now()

	state.append(strings.Repeat("a", acpGroupedThinkingFlushChars-1), now)
	if text, reason := state.readyText(now.Add(10*time.Millisecond), false); text != "" || reason != "" {
		t.Fatalf("expected no flush below char threshold, got %q", text)
	}

	state.append("b", now.Add(20*time.Millisecond))
	if text, reason := state.readyText(now.Add(30*time.Millisecond), false); len(text) != acpGroupedThinkingFlushChars || reason != "size" {
		t.Fatalf("expected grouped flush at char threshold, got length %d", len(text))
	}
}

func TestGroupedThinkingStateFlushesOnTimeThreshold(t *testing.T) {
	var state groupedThinkingState
	now := time.Now()

	state.append("thinking", now)
	if text, reason := state.readyText(now.Add(acpGroupedThinkingFlushInterval-time.Millisecond), false); text != "" || reason != "" {
		t.Fatalf("expected no flush before time threshold, got %q", text)
	}
	if text, reason := state.readyText(now.Add(acpGroupedThinkingFlushInterval), false); text != "thinking" || reason != "time" {
		t.Fatalf("expected time-based flush, got %q", text)
	}
}

func TestGroupedThinkingStateMarkFlushedResetsPending(t *testing.T) {
	var state groupedThinkingState
	now := time.Now()

	state.append("first", now)
	if text, reason := state.readyText(now, true); text != "first" || reason != "forced" {
		t.Fatalf("expected forced flush text, got %q", text)
	}

	flushedAt := now.Add(25 * time.Millisecond)
	state.markFlushed(flushedAt)
	if text, reason := state.readyText(flushedAt, true); text != "" || reason != "" {
		t.Fatalf("expected pending buffer to reset after flush, got %q", text)
	}

	state.append("next", flushedAt.Add(10*time.Millisecond))
	if text, reason := state.readyText(flushedAt.Add(20*time.Millisecond), false); text != "" || reason != "" {
		t.Fatalf("expected timer to restart after flush, got %q", text)
	}
}

func TestProcessPromptWithAgentEmitsUserMessageChunk(t *testing.T) {
	agent := newTestPandoAgentWithModels(string(llmmodels.Claude46Sonnet), ACPModelInfo{
		ID:   string(llmmodels.Claude46Sonnet),
		Name: "Claude Sonnet 4.6",
	})
	acpSession := NewACPServerSession(acpsdk.SessionId("session-1"), "/tmp", nil, "pando-session-1")
	acpSession.SetThinkingMode(thinkingModeDisabled)

	stopReason, err := agent.processPromptWithAgent(context.Background(), acpSession, "hello world")
	if err != nil {
		t.Fatalf("processPromptWithAgent failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("unexpected stop reason: %q", stopReason)
	}

	update := updateUserMessageTextWithID("hello world", "pando-session-1-user")
	if update.UserMessageChunk == nil || update.UserMessageChunk.MessageId == nil {
		t.Fatal("expected helper-generated user chunk to include messageId")
	}
	if got := *update.UserMessageChunk.MessageId; got != "pando-session-1-user" {
		t.Fatalf("unexpected user messageId: %q", got)
	}

	mockSvc := agent.agentService.(*mockAgentService)
	if got := mockSvc.sessionOverrides[acpSession.PandoSessionID()]; got.ReasoningEffort != "" || got.ThinkingMode != thinkingModeDisabled {
		t.Fatalf("expected normalized session overrides to be forwarded before Run, got %#v", got)
	}
}

func TestProcessAgentResponseUsesMessageIDForAssistantChunks(t *testing.T) {
	agent := newTestPandoAgent()
	acpSession := NewACPServerSession(acpsdk.SessionId("session-1"), "/tmp", nil, "pando-session-1")
	msg := message.Message{
		ID: "assistant-msg-1",
		Parts: []message.ContentPart{
			message.TextContent{Text: "hello"},
			message.ReasoningContent{Thinking: "thinking"},
		},
	}

	if err := agent.processAgentResponse(acpSession, msg, false, false); err != nil {
		t.Fatalf("processAgentResponse failed: %v", err)
	}

	messageUpdate := updateAgentMessageTextWithID("hello", msg.ID)
	if messageUpdate.AgentMessageChunk == nil || messageUpdate.AgentMessageChunk.MessageId == nil || *messageUpdate.AgentMessageChunk.MessageId != msg.ID {
		t.Fatalf("expected assistant message update to include messageId, got %#v", messageUpdate.AgentMessageChunk)
	}

	thoughtUpdate := updateAgentThoughtTextWithID("thinking", msg.ID)
	if thoughtUpdate.AgentThoughtChunk == nil || thoughtUpdate.AgentThoughtChunk.MessageId == nil || *thoughtUpdate.AgentThoughtChunk.MessageId != msg.ID {
		t.Fatalf("expected assistant thought update to include messageId, got %#v", thoughtUpdate.AgentThoughtChunk)
	}
}

func TestSendAgentTextUsesStableSlashCommandMessageID(t *testing.T) {
	acpSession := NewACPServerSession(acpsdk.SessionId("session-1"), "/tmp", nil, "pando-session-1")
	agent := newTestPandoAgent()

	if err := agent.sendAgentText(acpSession, "Goal mode activated."); err != nil {
		t.Fatalf("sendAgentText failed: %v", err)
	}

	update := updateAgentMessageTextWithID("Goal mode activated.", "pando-session-1-slash-command")
	if update.AgentMessageChunk == nil || update.AgentMessageChunk.MessageId == nil {
		t.Fatal("expected slash command assistant message to include messageId")
	}
	if got := *update.AgentMessageChunk.MessageId; got != "pando-session-1-slash-command" {
		t.Fatalf("unexpected slash command messageId: %q", got)
	}
}

// TestStartManualSummaryStreamsUntilDone verifies that /compact blocks on the real
// (asynchronous) summary completion instead of returning immediately: the channel
// stays open, forwards summarize progress, and only closes once the summary is done.
func TestStartManualSummaryStreamsUntilDone(t *testing.T) {
	agent := newTestPandoAgent()
	mockAgent := agent.agentService.(*mockAgentService)

	events, err := agent.startManualSummary(context.Background(), "pando-session-1")
	if err != nil {
		t.Fatalf("startManualSummary failed: %v", err)
	}
	if !mockAgent.summarizeCalled {
		t.Fatal("expected summarize to be invoked")
	}

	var progressEvents int
	for event := range events {
		if event.Type == AgentEventTypeSummarize && event.Progress != "" {
			progressEvents++
		}
	}
	if progressEvents == 0 {
		t.Fatal("expected summarize progress events streamed until completion, got none")
	}
}

func TestMapToolKind(t *testing.T) {
	if got := mapToolKind("TodoWrite"); got != acpsdk.ToolKindThink {
		t.Fatalf("TodoWrite kind = %q, want %q", got, acpsdk.ToolKindThink)
	}
	if got := mapToolKind("ExitPlanMode"); got != acpsdk.ToolKindSwitchMode {
		t.Fatalf("ExitPlanMode kind = %q, want %q", got, acpsdk.ToolKindSwitchMode)
	}
}

func TestHandleCopilotUsageRPC(t *testing.T) {
	agent := newTestPandoAgent()
	mockSvc := agent.agentService.(*mockAgentService)
	var out bytes.Buffer

	handleCopilotUsageRPC(jsonRPCMsg{ID: json.RawMessage("1")}, &out, agent, log.Default())

	var resp struct {
		Result usageOpenResult `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Result.Opened || resp.Result.URL != "https://github.com/settings/copilot/features" {
		t.Fatalf("unexpected result: %+v", resp.Result)
	}
	if mockSvc.copilotUsageErr != nil {
		t.Fatalf("unexpected mock error: %v", mockSvc.copilotUsageErr)
	}
}

func TestHandleClaudeUsageRPCError(t *testing.T) {
	mockSvc := &mockAgentService{claudeUsageErr: errors.New("oauth required")}
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockSvc, newMockSessionService(), nil)
	var out bytes.Buffer

	handleClaudeUsageRPC(jsonRPCMsg{ID: json.RawMessage("1")}, &out, agent, log.Default())

	var resp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error.Code != -32602 || resp.Error.Message != "oauth required" {
		t.Fatalf("unexpected error response: %+v", resp.Error)
	}
}

func TestPandoACPAgent_HandleExtensionMethod(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()
	newResp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	t.Run("set persona", func(t *testing.T) {
		params, err := json.Marshal(map[string]string{
			"sessionId": string(newResp.SessionId),
			"name":      "assistant",
		})
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}

		resp, err := agent.HandleExtensionMethod(ctx, "_pando.setPersona", params)
		if err != nil {
			t.Fatalf("HandleExtensionMethod returned error: %v", err)
		}

		result, ok := resp.(personaGetResult)
		if !ok {
			t.Fatalf("unexpected response type %T", resp)
		}
		if result.Active != "assistant" {
			t.Fatalf("unexpected active persona %q", result.Active)
		}

		agent.sessionsMu.RLock()
		persona := agent.sessions[newResp.SessionId].Persona()
		agent.sessionsMu.RUnlock()
		if persona != "assistant" {
			t.Fatalf("session persona = %q, want %q", persona, "assistant")
		}
	})

	t.Run("unknown method", func(t *testing.T) {
		_, err := agent.HandleExtensionMethod(ctx, "_pando.unknown", nil)
		if err == nil || !strings.Contains(err.Error(), "unknown extension method") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing session id", func(t *testing.T) {
		params, err := json.Marshal(map[string]string{"name": "assistant"})
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}

		_, err = agent.HandleExtensionMethod(ctx, "_pando.setPersona", params)
		if err == nil || !strings.Contains(err.Error(), "sessionId is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// mockAgentService is a test double for AgentService.
type mockAgentService struct {
	runCalled          bool
	runGoalCalled      bool
	summarizeCalled    bool
	cancelCalled       bool
	runErr             error
	goalRunErr         error
	summarizeErr       error
	modelOverride      string
	modelOverrideErr   error
	copilotUsageErr    error
	claudeUsageErr     error
	goalObjective      string
	summarizeSessionID string
	lastRunMessages    []string
	activePersona      string
	currentModel       string
	// sessionModelOverrides mimics the agent-side runtime model switch keyed by
	// Pando session id (pando_setup model).
	sessionModelOverrides map[string]string
	availableModels       []ACPModelInfo
	sessionOverrides      map[string]SessionLLMOverrides
	ponytailModes         map[string]string
	cavemanModes          map[string]string
	busy                  bool
	steerErr              error
	steerCalls            []string
	pendingSteering       int

	superpowersModes        map[string]bool
	superpowersFinishCalled bool
	superpowersFinishErr    error
	// superpowersFinishSucceeds mirrors the real adapter contract: the mode is
	// cleared only when the closing turn reaches a successful terminal response.
	superpowersFinishSucceeds bool

	learningModes          map[string]bool
	learningFinishCalled   bool
	learningFinishErr      error
	learningFinishSucceeds bool
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

func (m *mockAgentService) RunGoal(ctx context.Context, sessionID string, objective string) (<-chan AgentEvent, error) {
	m.runGoalCalled = true
	m.goalObjective = objective
	if m.goalRunErr != nil {
		return nil, m.goalRunErr
	}
	ch := make(chan AgentEvent)
	close(ch)
	return ch, nil
}

func (m *mockAgentService) Cancel(sessionID string) {
	m.cancelCalled = true
}

func (m *mockAgentService) Steer(sessionID string, content string, attachments ...message.Attachment) error {
	m.steerCalls = append(m.steerCalls, content)
	if m.steerErr != nil {
		return m.steerErr
	}
	m.pendingSteering++
	return nil
}

func (m *mockAgentService) PendingSteering(sessionID string) int { return m.pendingSteering }

func (m *mockAgentService) IsSessionBusy(sessionID string) bool { return m.busy }

func (m *mockAgentService) LastRunSystemMessages(sessionID string) []string {
	msgs := append([]string(nil), m.lastRunMessages...)
	m.lastRunMessages = nil
	return msgs
}

func (m *mockAgentService) CurrentModelID() string {
	if strings.TrimSpace(m.currentModel) != "" {
		return strings.TrimSpace(m.currentModel)
	}
	return "test-model"
}

func (m *mockAgentService) SessionModelOverrideID(sessionID string) string {
	return strings.TrimSpace(m.sessionModelOverrides[sessionID])
}

func (m *mockAgentService) AvailableModels() []ACPModelInfo {
	if len(m.availableModels) > 0 {
		return append([]ACPModelInfo(nil), m.availableModels...)
	}
	current := m.CurrentModelID()
	return []ACPModelInfo{
		{ID: current, Name: "Test Model"},
	}
}

func (m *mockAgentService) SetModelOverride(modelID string) error {
	m.modelOverride = modelID
	return m.modelOverrideErr
}

func (m *mockAgentService) SetSessionLLMOverrides(sessionID string, overrides SessionLLMOverrides) {
	if m.sessionOverrides == nil {
		m.sessionOverrides = make(map[string]SessionLLMOverrides)
	}
	if overrides.ReasoningEffort == "" && overrides.ThinkingMode == "" {
		delete(m.sessionOverrides, sessionID)
		return
	}
	m.sessionOverrides[sessionID] = overrides
}

func (m *mockAgentService) SetPonytailMode(sessionID string, mode string) (string, bool) {
	if m.ponytailModes == nil {
		m.ponytailModes = make(map[string]string)
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "lite", "full", "ultra":
		norm := strings.ToLower(strings.TrimSpace(mode))
		m.ponytailModes[sessionID] = norm
		return norm, true
	case "off", "stop", "normal", "none", "disable", "disabled":
		delete(m.ponytailModes, sessionID)
		return "off", true
	default:
		return "", false
	}
}

func (m *mockAgentService) SetCavemanMode(sessionID string, mode string) (string, bool) {
	if m.cavemanModes == nil {
		m.cavemanModes = make(map[string]string)
	}
	parsed, ok := caveman.ParseMode(mode)
	if !ok {
		return "", false
	}
	if parsed.IsActive() {
		m.cavemanModes[sessionID] = parsed.String()
	} else {
		delete(m.cavemanModes, sessionID)
	}
	return parsed.String(), true
}

func (m *mockAgentService) SetSuperpowersMode(sessionID string, enabled bool) {
	if m.superpowersModes == nil {
		m.superpowersModes = make(map[string]bool)
	}
	if enabled {
		m.superpowersModes[sessionID] = true
		return
	}
	delete(m.superpowersModes, sessionID)
}

func (m *mockAgentService) SuperpowersMode(sessionID string) bool {
	return m.superpowersModes[sessionID]
}

func (m *mockAgentService) SuperpowersFinish(ctx context.Context, sessionID string) (<-chan AgentEvent, error) {
	m.superpowersFinishCalled = true
	if m.superpowersFinishErr != nil {
		return nil, m.superpowersFinishErr
	}
	if m.superpowersFinishSucceeds {
		m.SetSuperpowersMode(sessionID, false)
	}
	ch := make(chan AgentEvent)
	close(ch)
	return ch, nil
}

func (m *mockAgentService) SetLearningMode(sessionID string, enabled bool) {
	if m.learningModes == nil {
		m.learningModes = make(map[string]bool)
	}
	if enabled {
		m.learningModes[sessionID] = true
		return
	}
	delete(m.learningModes, sessionID)
}

func (m *mockAgentService) LearningMode(sessionID string) bool {
	return m.learningModes[sessionID]
}

func (m *mockAgentService) LearningFinish(ctx context.Context, sessionID string) (<-chan AgentEvent, error) {
	m.learningFinishCalled = true
	if m.learningFinishErr != nil {
		return nil, m.learningFinishErr
	}
	if m.learningFinishSucceeds {
		m.SetLearningMode(sessionID, false)
	}
	ch := make(chan AgentEvent)
	close(ch)
	return ch, nil
}

func (m *mockAgentService) ListPersonas() []string {
	return []string{"default", "assistant"}
}

func (m *mockAgentService) GetActivePersona() string {
	if m.activePersona != "" {
		return m.activePersona
	}
	return "default"
}

func (m *mockAgentService) SetActivePersona(name string) error {
	m.activePersona = name
	return nil
}

func TestSetSessionModeCleanModeFlag(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	if _, err := agent.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{SessionId: resp.SessionId, ModeId: acpsdk.SessionModeId(cleanModeID)}); err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	sess, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	if !sess.CleanMode() {
		t.Fatal("expected clean mode flag to be enabled")
	}
}

func TestPromptCleanModeClearsPersonaOverride(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	if err := agent.SetSessionPersona(ctx, resp.SessionId, "assistant"); err != nil {
		t.Fatalf("SetSessionPersona failed: %v", err)
	}
	if _, err := agent.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{SessionId: resp.SessionId, ModeId: acpsdk.SessionModeId(cleanModeID)}); err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	_, err = agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("hello")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	mockSvc := agent.agentService.(*mockAgentService)
	if mockSvc.activePersona != "" {
		t.Fatalf("expected clean mode to clear active persona, got %q", mockSvc.activePersona)
	}
}

func (m *mockAgentService) Summarize(ctx context.Context, sessionID string) (<-chan AgentEvent, error) {
	m.summarizeCalled = true
	m.summarizeSessionID = sessionID
	if m.summarizeErr != nil {
		return nil, m.summarizeErr
	}
	ch := make(chan AgentEvent, 2)
	ch <- AgentEvent{Type: AgentEventTypeSummarize, Progress: "Starting summarization..."}
	ch <- AgentEvent{Type: AgentEventTypeSummarize, Progress: "Summary complete"}
	close(ch)
	return ch, nil
}

func (m *mockAgentService) OpenCopilotUsage() error {
	return m.copilotUsageErr
}

func (m *mockAgentService) OpenClaudeUsage() error {
	return m.claudeUsageErr
}

// mockSessionService is a test double for SessionService.
type mockSessionService struct {
	sessions  map[string]ACPSessionInfo
	acpStates map[string]string
	goals     map[string]db.SessionGoal
	created   []string
	counter   int
}

func newMockSessionService() *mockSessionService {
	return &mockSessionService{
		sessions:  make(map[string]ACPSessionInfo),
		acpStates: make(map[string]string),
		goals:     make(map[string]db.SessionGoal),
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

func (m *mockSessionService) SaveSessionTitle(ctx context.Context, id string, title string) (ACPSessionInfo, error) {
	s, ok := m.sessions[id]
	if !ok {
		return ACPSessionInfo{}, errors.New("session not found")
	}
	s.Title = title
	m.sessions[id] = s
	return s, nil
}

func (m *mockSessionService) ListSessions(ctx context.Context) ([]ACPSessionInfo, error) {
	result := make([]ACPSessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result, nil
}

func (m *mockSessionService) GetACPSessionState(ctx context.Context, sessionID string) (string, error) {
	return m.acpStates[sessionID], nil
}

func (m *mockSessionService) GetMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return nil, nil
}

func (m *mockSessionService) SaveACPSessionState(ctx context.Context, sessionID string, state string) error {
	m.acpStates[sessionID] = state
	return nil
}

func (m *mockSessionService) GetActiveGoal(ctx context.Context, sessionID string) (db.SessionGoal, error) {
	goal, ok := m.goals[sessionID]
	if !ok || goal.Status != "running" {
		return db.SessionGoal{}, sql.ErrNoRows
	}
	return goal, nil
}

func (m *mockSessionService) GetGoalBySession(ctx context.Context, sessionID string) (db.SessionGoal, error) {
	goal, ok := m.goals[sessionID]
	if !ok {
		return db.SessionGoal{}, sql.ErrNoRows
	}
	return goal, nil
}

func (m *mockSessionService) CancelGoal(ctx context.Context, sessionID string) (db.SessionGoal, error) {
	goal, err := m.GetActiveGoal(ctx, sessionID)
	if err != nil {
		return db.SessionGoal{}, err
	}
	goal.Status = "cancelled"
	goal.CompletedAt = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
	m.goals[sessionID] = goal
	return goal, nil
}

type mockPermissionService struct {
	autoApproved []string
	removed      []string
	registered   []string
	unregistered []string
	handlers     map[string]func(req PermissionRequestData) bool
}

func newMockPermissionService() *mockPermissionService {
	return &mockPermissionService{
		handlers: make(map[string]func(req PermissionRequestData) bool),
	}
}

func (m *mockPermissionService) AutoApproveSession(sessionID string) {
	m.autoApproved = append(m.autoApproved, sessionID)
}

func (m *mockPermissionService) RemoveAutoApproveSession(sessionID string) {
	m.removed = append(m.removed, sessionID)
}

func (m *mockPermissionService) RegisterSessionHandler(sessionID string, handler func(req PermissionRequestData) bool) {
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

func newTestPandoAgentWithModels(currentModel string, availableModels ...ACPModelInfo) *PandoACPAgent {
	agent := &mockAgentService{
		currentModel:    currentModel,
		availableModels: append([]ACPModelInfo(nil), availableModels...),
	}
	sessions := newMockSessionService()
	return NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), agent, sessions, nil)
}

func sessionConfigValuesByID(t *testing.T, opts []acpsdk.SessionConfigOption) map[string]string {
	t.Helper()

	values := make(map[string]string, len(opts))
	for _, opt := range opts {
		if opt.Select == nil {
			t.Fatalf("expected select config option, got %+v", opt)
		}
		values[string(opt.Select.Id)] = string(opt.Select.CurrentValue)
	}
	return values
}

func sessionConfigOptionByID(t *testing.T, opts []acpsdk.SessionConfigOption, id string) acpsdk.SessionConfigOption {
	t.Helper()

	for _, opt := range opts {
		if opt.Select != nil && string(opt.Select.Id) == id {
			return opt
		}
	}
	t.Fatalf("expected config option %q in %+v", id, opts)
	return acpsdk.SessionConfigOption{}
}

func configOptionValueDescriptionsByID(t *testing.T, opt acpsdk.SessionConfigOption) map[string]string {
	t.Helper()

	if opt.Select == nil || opt.Select.Options.Ungrouped == nil {
		t.Fatalf("expected ungrouped select option, got %+v", opt)
	}
	descriptions := make(map[string]string, len(*opt.Select.Options.Ungrouped))
	for _, value := range *opt.Select.Options.Ungrouped {
		if value.Description != nil {
			descriptions[string(value.Value)] = *value.Description
			continue
		}
		descriptions[string(value.Value)] = ""
	}
	return descriptions
}

func unstableSessionConfigValuesByID(t *testing.T, opts []acpsdk.UnstableSessionConfigOption) map[string]string {
	t.Helper()

	raw, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("marshal unstable config options: %v", err)
	}

	var stable []acpsdk.SessionConfigOption
	if err := json.Unmarshal(raw, &stable); err != nil {
		t.Fatalf("unmarshal unstable config options: %v", err)
	}

	return sessionConfigValuesByID(t, stable)
}

func TestACPServerSessionCancelResetsRunContextWithoutBreakingUpdates(t *testing.T) {
	session := NewACPServerSession("session-1", "/tmp", nil, "pando-1")
	before := session.Context()

	session.Cancel()

	select {
	case <-before.Done():
	default:
		t.Fatal("expected previous run context to be canceled")
	}

	after := session.Context()
	if after == before {
		t.Fatal("expected Cancel to install a fresh run context")
	}
	if err := after.Err(); err != nil {
		t.Fatalf("expected fresh run context to remain active, got %v", err)
	}
}

func TestProcessAgentResponse_ToolCallsIncludeRenderingMetadata(t *testing.T) {
	agent := newTestPandoAgent()
	input := map[string]any{"file_path": "/workspace/project/main.go", "offset": 10, "limit": 5}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	msg := message.Message{Parts: []message.ContentPart{
		message.ToolCall{ID: "tool-1", Name: "view", Input: string(inputJSON)},
	}}

	acpSession := NewACPServerSession(acpsdk.SessionId("session-1"), "/workspace/project", nil, "session-1")
	if err := agent.processAgentResponse(acpSession, msg, false, false); err != nil {
		t.Fatalf("processAgentResponse failed: %v", err)
	}

	stored := agent.pendingToolCalls["tool-1"]
	if stored != string(inputJSON) {
		t.Fatalf("expected pending tool input to be stored, got %q", stored)
	}

	rawInput := parseJSONInput(stored)
	title := toolDisplayTitle("view", rawInput, "/workspace/project")
	if title != "Read main.go (10 - 14)" {
		t.Fatalf("unexpected title: %q", title)
	}
	if kind := mapToolKind("view"); kind != acpsdk.ToolKindRead {
		t.Fatalf("unexpected kind: %q", kind)
	}
	locations := toLocations("view", stored)
	if len(locations) != 1 || locations[0].Path != "/workspace/project/main.go" {
		t.Fatalf("unexpected locations: %+v", locations)
	}
	start := acpsdk.StartToolCall(
		acpsdk.ToolCallId("tool-1"),
		title,
		acpsdk.WithStartKind(mapToolKind("view")),
		acpsdk.WithStartStatus(acpsdk.ToolCallStatusPending),
		acpsdk.WithStartRawInput(rawInput),
		acpsdk.WithStartLocations(locations),
	)
	if start.ToolCall == nil {
		t.Fatal("expected tool_call payload")
	}
	if start.ToolCall.Title != title {
		t.Fatalf("unexpected tool_call title: %q", start.ToolCall.Title)
	}
	if start.ToolCall.Kind != acpsdk.ToolKindRead {
		t.Fatalf("unexpected tool_call kind: %q", start.ToolCall.Kind)
	}
	if start.ToolCall.Status != acpsdk.ToolCallStatusPending {
		t.Fatalf("unexpected tool_call status: %q", start.ToolCall.Status)
	}
	if len(start.ToolCall.Locations) != 1 || start.ToolCall.Locations[0].Path != "/workspace/project/main.go" {
		t.Fatalf("unexpected tool_call locations: %+v", start.ToolCall.Locations)
	}
	if rawMap, ok := start.ToolCall.RawInput.(map[string]interface{}); !ok || rawMap["file_path"] != "/workspace/project/main.go" {
		t.Fatalf("unexpected tool_call raw input: %#v", start.ToolCall.RawInput)
	}
}

// TestToolCallStreamParity verifies that tool call metadata is consistent
// between initial StartToolCall, delta UpdateToolCall, final UpdateToolCall,
// and history replay.
func TestToolCallStreamParity(t *testing.T) {
	// Test data: a view command with file path and range
	input := map[string]any{
		"file_path": "/workspace/project/internal/main.go",
		"offset":    float64(10),
		"limit":     float64(5),
	}
	inputJSON, _ := json.Marshal(input)
	workDir := "/workspace/project"

	// 1. Initial state (empty input) - StartToolCall sends generic title
	emptyInput := map[string]any{}
	startEmpty := acpsdk.StartToolCall(
		acpsdk.ToolCallId("tool-1"),
		"view",
		acpsdk.WithStartKind(mapToolKind("view")),
		acpsdk.WithStartStatus(acpsdk.ToolCallStatusInProgress),
		acpsdk.WithStartRawInput(emptyInput),
	)
	if startEmpty.ToolCall.Title != "view" {
		t.Errorf("Initial title should be tool name, got %q", startEmpty.ToolCall.Title)
	}

	// 2. Delta state (path only) - UpdateToolCall during streaming
	pathOnlyInput := map[string]any{"file_path": "/workspace/project/internal/main.go"}
	pathTitle := toolDisplayTitle("view", pathOnlyInput, workDir)
	deltaUpdate := acpsdk.UpdateToolCall(
		acpsdk.ToolCallId("tool-1"),
		acpsdk.WithUpdateTitle(pathTitle),
		acpsdk.WithUpdateKind(mapToolKind("view")),
		acpsdk.WithUpdateStatus(acpsdk.ToolCallStatusInProgress),
		acpsdk.WithUpdateRawInput(pathOnlyInput),
		acpsdk.WithUpdateLocations(toLocations("view", mustJSON(pathOnlyInput))),
	)
	if deltaUpdate.ToolCallUpdate.Title == nil || *deltaUpdate.ToolCallUpdate.Title != pathTitle {
		t.Errorf("Delta title mismatch")
	}
	if len(deltaUpdate.ToolCallUpdate.Locations) != 1 {
		t.Errorf("Delta update should have 1 location, got %d", len(deltaUpdate.ToolCallUpdate.Locations))
	}

	// 3. Complete state (full input) - final UpdateToolCall
	rawInput := parseJSONInput(string(inputJSON))
	fullTitle := toolDisplayTitle("view", rawInput, workDir)
	locations := toLocations("view", string(inputJSON))
	content := toolCallContent("view", rawInput)
	finalUpdate := acpsdk.UpdateToolCall(
		acpsdk.ToolCallId("tool-1"),
		acpsdk.WithUpdateTitle(fullTitle),
		acpsdk.WithUpdateKind(mapToolKind("view")),
		acpsdk.WithUpdateStatus(acpsdk.ToolCallStatusInProgress),
		acpsdk.WithUpdateRawInput(rawInput),
		acpsdk.WithUpdateLocations(locations),
		acpsdk.WithUpdateContent(content),
	)
	if finalUpdate.ToolCallUpdate.Title == nil || *finalUpdate.ToolCallUpdate.Title != fullTitle {
		t.Errorf("Final title mismatch")
	}
	if len(finalUpdate.ToolCallUpdate.Locations) != 1 {
		t.Errorf("Final update should have 1 location, got %d", len(finalUpdate.ToolCallUpdate.Locations))
	}

	// 4. History replay - same input, same helper calls
	historyInput := message.ToolCall{ID: "tool-1", Name: "view", Input: string(inputJSON)}
	historyRawInput := parseJSONInput(historyInput.Input)
	historyTitle := toolDisplayTitle(historyInput.Name, historyRawInput, workDir)
	historyLocations := toLocations(historyInput.Name, historyInput.Input)
	historyContent := toolCallContent(historyInput.Name, historyRawInput)
	historyUpdate := acpsdk.UpdateToolCall(
		acpsdk.ToolCallId(historyInput.ID),
		acpsdk.WithUpdateTitle(historyTitle),
		acpsdk.WithUpdateKind(mapToolKind(historyInput.Name)),
		acpsdk.WithUpdateStatus(acpsdk.ToolCallStatusInProgress),
		acpsdk.WithUpdateRawInput(historyRawInput),
		acpsdk.WithUpdateLocations(historyLocations),
		acpsdk.WithUpdateContent(historyContent),
	)

	// Verify parity between streaming final update and history replay
	if finalUpdate.ToolCallUpdate.Title == nil || historyUpdate.ToolCallUpdate.Title == nil ||
		*finalUpdate.ToolCallUpdate.Title != *historyUpdate.ToolCallUpdate.Title {
		t.Errorf("Streaming/Hist title parity failed")
	}
	if finalUpdate.ToolCallUpdate.Kind == nil || historyUpdate.ToolCallUpdate.Kind == nil ||
		*finalUpdate.ToolCallUpdate.Kind != *historyUpdate.ToolCallUpdate.Kind {
		t.Errorf("Streaming/Hist kind parity failed")
	}
	if len(finalUpdate.ToolCallUpdate.Locations) != len(historyUpdate.ToolCallUpdate.Locations) {
		t.Errorf("Streaming/Hist locations length parity failed: %d vs %d",
			len(finalUpdate.ToolCallUpdate.Locations), len(historyUpdate.ToolCallUpdate.Locations))
	}
}

// mustJSON marshals v to JSON string, for use in tests only.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestToolCallProgressiveEnrichment verifies that tool call metadata
// becomes more specific as input accumulates during streaming.
func TestToolCallProgressiveEnrichment(t *testing.T) {
	workDir := "/workspace/project"

	// Stage 1: Empty input - generic title
	emptyInput := map[string]any{}
	title1 := toolDisplayTitle("view", emptyInput, workDir)
	if title1 == "" {
		t.Errorf("Stage 1: empty input should produce a non-empty generic title")
	}

	// Stage 2: Path only - shows file name
	pathOnlyInput := map[string]any{"file_path": "/workspace/project/src/utils.go"}
	title2 := toolDisplayTitle("view", pathOnlyInput, workDir)
	if !strings.Contains(title2, "utils.go") {
		t.Errorf("Stage 2: path-only input should include filename, got %q", title2)
	}

	// Stage 3: Complete input - shows file and range
	completeInput := map[string]any{"file_path": "/workspace/project/src/utils.go", "offset": 100, "limit": 50}
	title3 := toolDisplayTitle("view", completeInput, workDir)
	if !strings.Contains(title3, "utils.go") || !strings.Contains(title3, "100") {
		t.Errorf("Stage 3: complete input should include filename and offset, got %q", title3)
	}

	// Verify progressive enrichment: each stage adds more specificity
	if title1 == title2 {
		t.Error("Stage 1 -> 2: title should change when path is added")
	}
	if title2 == title3 {
		t.Error("Stage 2 -> 3: title should change when offset is added")
	}
}

func TestParseTodoWritePlanSupportsStreamingUpdates(t *testing.T) {
	t.Parallel()

	// Complete, valid JSON: exact parse works.
	partial := `{"todos":[{"content":"Investigate logs","status":"in_progress","priority":"high"}]}`
	entries := parseTodoWritePlan(partial)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Content != "Investigate logs" {
		t.Fatalf("unexpected content: %q", entries[0].Content)
	}
	if entries[0].Status != acpsdk.PlanEntryStatusInProgress {
		t.Fatalf("unexpected status: %q", entries[0].Status)
	}
	if entries[0].Priority != acpsdk.PlanEntryPriorityHigh {
		t.Fatalf("unexpected priority: %q", entries[0].Priority)
	}

	// Tolerant parsing: truncated JSON should still return entries when repairable.
	incompleteJSON := `{"todos":[{"content":"Investigate logs"`
	entries = parseTodoWritePlan(incompleteJSON)
	if len(entries) != 1 {
		t.Fatalf("tolerant parser: expected 1 entry for repairable JSON, got %d", len(entries))
	}
	if entries[0].Content != "Investigate logs" {
		t.Fatalf("tolerant parser: unexpected content %q", entries[0].Content)
	}

	// Tolerant parsing: truncated mid-content should still recover the entry.
	midContent := `{"todos":[{"content":"Investigate lo`
	entries = parseTodoWritePlan(midContent)
	if len(entries) != 1 {
		t.Fatalf("tolerant parser mid-content: expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content != "Investigate lo" {
		t.Fatalf("tolerant parser mid-content: unexpected content %q", entries[0].Content)
	}

	// Tolerant parsing: two complete entries + truncated third should return at least the first two.
	twoAndHalf := `{"todos":[{"content":"Step 1","status":"completed"},{"content":"Step 2","status":"in_progress"},{"content":"Step`
	entries = parseTodoWritePlan(twoAndHalf)
	if len(entries) < 2 {
		t.Fatalf("tolerant parser 2.5 entries: expected at least 2 entries, got %d", len(entries))
	}
	if entries[0].Content != "Step 1" || entries[1].Content != "Step 2" {
		t.Fatalf("tolerant parser 2.5 entries: unexpected content %q, %q", entries[0].Content, entries[1].Content)
	}

	// Completely unparseable garbage returns nil.
	garbage := `not json at all`
	if entries := parseTodoWritePlan(garbage); len(entries) != 0 {
		t.Fatalf("expected no entries for garbage input, got %d", len(entries))
	}

	// Empty prefix before todos array: too little data to repair.
	tooShort := `{"todos":[`
	if entries := parseTodoWritePlan(tooShort); len(entries) != 0 {
		t.Fatalf("expected no entries for too-short input, got %d", len(entries))
	}
}

func TestTodoWritePlanUpdateImpliesPlanModeUpdate(t *testing.T) {
	t.Parallel()

	update := acpsdk.UpdatePlan(
		acpsdk.NewPlanEntry("Investigate logs", acpsdk.PlanEntryStatusInProgress, acpsdk.PlanEntryPriorityHigh),
	)
	modeUpdate := acpsdk.UpdateCurrentMode(acpsdk.SessionModeId("plan"))

	if update.Plan == nil {
		t.Fatalf("expected plan update payload")
	}
	if modeUpdate.CurrentModeUpdate == nil {
		t.Fatalf("expected current mode update payload")
	}
	if modeUpdate.CurrentModeUpdate.CurrentModeId != acpsdk.SessionModeId("plan") {
		t.Fatalf("unexpected mode id: %q", modeUpdate.CurrentModeUpdate.CurrentModeId)
	}
	if len(update.Plan.Entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(update.Plan.Entries))
	}
	if update.Plan.Entries[0].Content != "Investigate logs" {
		t.Fatalf("unexpected content: %q", update.Plan.Entries[0].Content)
	}
}

func TestSendPlanModeUpdateNilSessionIsNoop(t *testing.T) {
	t.Parallel()

	agent := newTestPandoAgent()
	entries := []acpsdk.PlanEntry{
		acpsdk.NewPlanEntry("Investigate logs", acpsdk.PlanEntryStatusInProgress, acpsdk.PlanEntryPriorityHigh),
	}
	if err := agent.sendPlanModeUpdate(nil, entries); err != nil {
		t.Fatalf("sendPlanModeUpdate(nil, entries) error = %v", err)
	}
	if err := agent.sendPlanModeUpdate(NewACPServerSession(acpsdk.SessionId("session-plan"), "/tmp", nil, "pando-session-plan"), nil); err != nil {
		t.Fatalf("sendPlanModeUpdate(session, nil) error = %v", err)
	}
}

func TestParseRawInputNeverReturnsString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantMap  bool
		wantKeys int // expected number of keys if wantMap is true
	}{
		{"empty", "", true, 0},
		{"valid object", `{"selector":"input","value":"test"}`, true, 2},
		{"valid array", `[1,2,3]`, false, 0}, // returns []interface{}, not map
		{"partial JSON", `{"selector": "input[type=\"pa`, true, 0},
		{"bare string", `hello world`, true, 0},
		{"truncated mid-key", `{"sel`, true, 0},
		{"just opening brace", `{`, true, 0},
		{"number", `42`, true, 0},
		{"boolean", `true`, true, 0},
		{"null", `null`, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRawInput(tt.input)
			// Must never be a string — that causes double-encoding in ACP JSON-RPC
			if _, isString := result.(string); isString {
				t.Fatalf("parseRawInput(%q) returned string, want map or slice", tt.input)
			}
			if tt.wantMap {
				m, ok := result.(map[string]interface{})
				if !ok {
					t.Fatalf("parseRawInput(%q) = %T, want map[string]interface{}", tt.input, result)
				}
				if len(m) != tt.wantKeys {
					t.Fatalf("parseRawInput(%q) map has %d keys, want %d", tt.input, len(m), tt.wantKeys)
				}
			}
		})
	}
}

func TestHasUsefulRawInputChecksForContent(t *testing.T) {
	t.Parallel()

	if hasUsefulRawInput(map[string]interface{}{}) {
		t.Error("empty map should not be useful")
	}
	if !hasUsefulRawInput(map[string]interface{}{"key": "val"}) {
		t.Error("non-empty map should be useful")
	}
	if hasUsefulRawInput("partial json") {
		t.Error("string should not be useful")
	}
	if hasUsefulRawInput(nil) {
		t.Error("nil should not be useful")
	}
	if !hasUsefulRawInput([]interface{}{1}) {
		t.Error("non-empty slice should be useful")
	}
	if hasUsefulRawInput([]interface{}{}) {
		t.Error("empty slice should not be useful")
	}
}

func TestToolCallStreamStartsWithAvailableRawInput(t *testing.T) {
	t.Parallel()

	rawInput := map[string]any{"command": "go test ./internal/api"}
	start := acpsdk.StartToolCall(
		acpsdk.ToolCallId("tool-raw-input"),
		toolDisplayTitle("bash", rawInput, "/workspace/project"),
		acpsdk.WithStartKind(mapToolKind("bash")),
		acpsdk.WithStartStatus(acpsdk.ToolCallStatusPending),
		acpsdk.WithStartRawInput(rawInput),
		acpsdk.WithStartContent([]acpsdk.ToolCallContent{acpsdk.ToolTerminalRef("tool-raw-input")}),
	)
	if start.ToolCall == nil {
		t.Fatal("expected tool_call payload")
	}
	inputMap, ok := start.ToolCall.RawInput.(map[string]any)
	if !ok {
		t.Fatalf("expected raw input map, got %#v", start.ToolCall.RawInput)
	}
	if inputMap["command"] != "go test ./internal/api" {
		t.Fatalf("unexpected command in start raw input: %#v", inputMap)
	}
}

func TestToolCallDeltaCanPromoteEmptyStartToEnrichedPending(t *testing.T) {
	t.Parallel()

	emptyStart := acpsdk.StartToolCall(
		acpsdk.ToolCallId("tool-delta"),
		"bash",
		acpsdk.WithStartKind(mapToolKind("bash")),
		acpsdk.WithStartStatus(acpsdk.ToolCallStatusPending),
		acpsdk.WithStartRawInput(map[string]any{}),
	)
	if emptyStart.ToolCall == nil {
		t.Fatal("expected initial tool_call payload")
	}

	enriched := map[string]any{"command": "grep -n foo pando-acp.log"}
	delta := acpsdk.UpdateToolCall(
		acpsdk.ToolCallId("tool-delta"),
		acpsdk.WithUpdateStatus(acpsdk.ToolCallStatusPending),
		acpsdk.WithUpdateKind(mapToolKind("bash")),
		acpsdk.WithUpdateTitle(toolDisplayTitle("bash", enriched, "/workspace/project")),
		acpsdk.WithUpdateRawInput(enriched),
		acpsdk.WithUpdateContent([]acpsdk.ToolCallContent{acpsdk.ToolTerminalRef("tool-delta")}),
	)
	if delta.ToolCallUpdate == nil || delta.ToolCallUpdate.Status == nil {
		t.Fatal("expected enriched tool_call_update payload")
	}
	if *delta.ToolCallUpdate.Status != acpsdk.ToolCallStatusPending {
		t.Fatalf("unexpected delta status: %q", *delta.ToolCallUpdate.Status)
	}
	inputMap, ok := delta.ToolCallUpdate.RawInput.(map[string]any)
	if !ok || inputMap["command"] != "grep -n foo pando-acp.log" {
		t.Fatalf("unexpected delta raw input: %#v", delta.ToolCallUpdate.RawInput)
	}
}

func TestToolMetaCapabilityGatesTerminalInfo(t *testing.T) {
	t.Parallel()

	agent := newTestPandoAgent()
	meta := agent.toolMeta("bash", "tool-1", true)
	if _, ok := meta["terminal_info"]; ok {
		t.Fatalf("expected terminal_info to be absent without terminal capability: %#v", meta)
	}

	agent.clientSupportsTerminalOutput = true
	meta = agent.toolMeta("bash", "tool-1", true)
	term, ok := meta["terminal_info"].(map[string]any)
	if !ok || term["terminal_id"] != "tool-1" {
		t.Fatalf("expected terminal_info when capability enabled, got %#v", meta)
	}
}

func TestToolStartContentFallsBackWithoutTerminalCapability(t *testing.T) {
	t.Parallel()

	agent := newTestPandoAgent()
	fallback := toolCallContent("bash", map[string]any{"command": "echo hi", "description": "run command"})
	content := agent.toolStartContent("bash", "tool-1", fallback)
	if len(content) != len(fallback) {
		t.Fatalf("expected fallback content without terminal capability, got %#v", content)
	}
}

func TestToolStartContentUsesTerminalRefWhenCapabilityEnabled(t *testing.T) {
	t.Parallel()

	agent := newTestPandoAgent()
	agent.clientSupportsTerminalOutput = true
	content := agent.toolStartContent("bash", "tool-1", nil)
	if len(content) != 1 || content[0].Terminal == nil || content[0].Terminal.TerminalId != "tool-1" {
		t.Fatalf("expected terminal ref content, got %#v", content)
	}
}

func TestToolCallContentShowsPathHintForPartialWriteAndEditInputs(t *testing.T) {
	t.Parallel()

	writeContent := toolCallContent("write", map[string]any{"path": "/workspace/project/internal/out.go"})
	if len(writeContent) != 1 || writeContent[0].Content == nil {
		t.Fatalf("expected write path hint content, got %#v", writeContent)
	}

	editContent := toolCallContent("edit", map[string]any{"path": "/workspace/project/internal/edit.go"})
	if len(editContent) != 1 || writeContent[0].Content == nil {
		t.Fatalf("expected edit path hint content, got %#v", editContent)
	}
}

// TestDeferredStartToolCallSendsEnrichedFirst verifies that when a tool call
// starts with empty input (ToolUseStart), the first StartToolCall is deferred
// until enriched input arrives, so ACP clients like Zed never see an empty card.
func TestDeferredStartToolCallSendsEnrichedFirst(t *testing.T) {
	t.Parallel()

	agent := newTestPandoAgent()
	workDir := "/workspace/project"

	// Simulate ToolUseStart with empty input:
	// The streaming path sets pendingToolCalls but does NOT set startedToolCalls
	// because input is empty (deferred).
	agent.pendingToolCallsMu.Lock()
	agent.pendingToolCalls["tool-deferred"] = ""
	// startedToolCalls["tool-deferred"] is intentionally NOT set
	agent.pendingToolCallsMu.Unlock()

	// Simulate processAgentResponse receiving the complete tool call
	// with full input. Since wasStarted is false, it should send
	// StartToolCall with full enriched data.
	msg := message.Message{Parts: []message.ContentPart{
		message.ToolCall{ID: "tool-deferred", Name: "bash", Input: `{"command":"go test ./...","timeout":120000}`},
	}}

	acpSession := NewACPServerSession(acpsdk.SessionId("session-deferred"), workDir, nil, "session-deferred")
	if err := agent.processAgentResponse(acpSession, msg, false, false); err != nil {
		t.Fatalf("processAgentResponse failed: %v", err)
	}

	// Verify the stored input was updated to the full input
	agent.pendingToolCallsMu.Lock()
	storedInput := agent.pendingToolCalls["tool-deferred"]
	agent.pendingToolCallsMu.Unlock()
	if storedInput != `{"command":"go test ./...","timeout":120000}` {
		t.Fatalf("expected stored input to be updated, got %q", storedInput)
	}

	// Verify that the enriched StartToolCall would have proper metadata
	rawInput := parseJSONInput(storedInput)
	title := toolDisplayTitle("bash", rawInput, workDir)
	if title != "go test ./..." {
		t.Fatalf("expected enriched title 'go test ./...', got %q", title)
	}
	kind := mapToolKind("bash")
	if kind != acpsdk.ToolKindExecute {
		t.Fatalf("expected ToolKindExecute, got %q", kind)
	}
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

func TestPandoACPAgent_SetConnection_BackfillsAvailableCommandsForExistingSessions(t *testing.T) {
	// logs is read by the test while the async sendAvailableCommandsUpdate
	// goroutine writes it, so it must be concurrency-safe.
	logs := &syncBuffer{}
	logger := log.New(logs, "", 0)
	mockAgent := &mockAgentService{}
	sessions := newMockSessionService()
	permSvc := newMockPermissionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", logger, mockAgent, sessions, permSvc)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	agent.SetConnection(&acpsdk.AgentSideConnection{})
	time.Sleep(20 * time.Millisecond)

	if !strings.Contains(logs.String(), "sendAvailableCommandsUpdate") {
		t.Fatalf("expected SetConnection to trigger available commands update, got logs:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), string(resp.SessionId)) {
		t.Fatalf("expected SetConnection-triggered update to mention session %s, got logs:\n%s", resp.SessionId, logs.String())
	}
}

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
	if !acpSess.AskPermission() {
		t.Error("Expected legacy ask mode to enable ask-permission compatibility")
	}
	if acpSess.PermissionConfigured() {
		t.Error("Expected legacy SetSessionMode to keep permission in inherited mode")
	}
}

func TestPandoACPAgent_SetSessionMode_Goal(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.SetSessionMode(ctx, acpsdk.SetSessionModeRequest{
		SessionId: resp.SessionId,
		ModeId:    goalModeID,
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	acpSess, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	if acpSess.Mode() != goalModeID {
		t.Fatalf("expected mode %q, got %q", goalModeID, acpSess.Mode())
	}
	if acpSess.AskPermission() {
		t.Fatal("expected goal mode to default to auto-approve")
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

func TestPandoACPAgent_Prompt_BusySessionQueuesSteering(t *testing.T) {
	mockAgent := &mockAgentService{busy: true}
	sessions := newMockSessionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, sessions, nil)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("focus on the auth bug")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if len(mockAgent.steerCalls) != 1 || mockAgent.steerCalls[0] != "focus on the auth bug" {
		t.Fatalf("expected steering to be queued, got steerCalls=%v", mockAgent.steerCalls)
	}
	if mockAgent.runCalled {
		t.Fatal("expected Run NOT to be called while session is busy")
	}
}

func TestPandoACPAgent_Prompt_IdleSessionRunsNormally(t *testing.T) {
	mockAgent := &mockAgentService{busy: false}
	sessions := newMockSessionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, sessions, nil)
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

	if len(mockAgent.steerCalls) != 0 {
		t.Fatalf("did not expect steering for idle session, got %v", mockAgent.steerCalls)
	}
	if !mockAgent.runCalled {
		t.Fatal("expected Run to be called for idle session")
	}
}

func TestPandoACPAgent_Prompt_GoalSlashStartsGoalRunner(t *testing.T) {
	mockAgent := &mockAgentService{}
	sessions := newMockSessionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), mockAgent, sessions, nil)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("/goal Implement phase two")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	if !mockAgent.runGoalCalled {
		t.Fatal("expected goal runner path to be used")
	}
	if mockAgent.goalObjective != "Implement phase two" {
		t.Fatalf("unexpected goal objective %q", mockAgent.goalObjective)
	}
	acpSess, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	if acpSess.Mode() != goalModeID {
		t.Fatalf("expected goal mode after /goal, got %q", acpSess.Mode())
	}
}

func TestPandoACPAgent_Prompt_GoalSlashRegistersTemporaryPermissionHandler(t *testing.T) {
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

	_, err = agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("/goal Implement phase two")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	found := false
	for _, sessionID := range permSvc.registered {
		if sessionID == string(resp.SessionId) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected goal mode permission handler registration for session %s, got %+v", resp.SessionId, permSvc.registered)
	}
	foundCleanup := false
	for _, sessionID := range permSvc.unregistered {
		if sessionID == string(resp.SessionId) {
			foundCleanup = true
			break
		}
	}
	if !foundCleanup {
		t.Fatalf("expected goal mode permission handler cleanup for session %s, got %+v", resp.SessionId, permSvc.unregistered)
	}
}

func TestPandoACPAgent_GoalPermissionHandlerApprovesSafeAndRejectsDangerousWithoutConnection(t *testing.T) {
	config.SetForTests(&config.Config{
		Goal: config.GoalConfig{
			AutoApprove:       true,
			DangerousPatterns: []string{"DROP TABLE"},
		},
	})
	t.Cleanup(func() {
		config.SetForTests(nil)
	})

	permSvc := newMockPermissionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), &mockAgentService{}, newMockSessionService(), permSvc)

	cleanup := agent.configurePermissionMode(acpsdk.SessionId("session-1"), goalModeID, false)
	defer cleanup()

	handler := permSvc.handlers["session-1"]
	if handler == nil {
		t.Fatal("expected goal permission handler to be registered")
	}

	if !handler(PermissionRequestData{
		SessionID: "session-1",
		ToolName:  "bash",
		Params:    map[string]any{"command": "go test ./..."},
	}) {
		t.Fatal("expected safe goal-mode command to auto-approve")
	}

	if handler(PermissionRequestData{
		SessionID: "session-1",
		ToolName:  "bash",
		Params:    map[string]any{"command": "sqlite3 app.db 'DROP TABLE users;'"},
	}) {
		t.Fatal("expected dangerous goal-mode command to require confirmation")
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

	foundRemoval := false
	for _, sessionID := range permSvc.removed {
		if sessionID == string(resp.SessionId) {
			foundRemoval = true
			break
		}
	}
	if !foundRemoval {
		t.Fatalf("expected auto-approve removal for session %s, got %+v", resp.SessionId, permSvc.removed)
	}
	foundRegistration := false
	for _, sessionID := range permSvc.registered {
		if sessionID == string(resp.SessionId) {
			foundRegistration = true
			break
		}
	}
	if !foundRegistration {
		t.Fatalf("expected handler registration for session %s, got %+v", resp.SessionId, permSvc.registered)
	}
	foundUnregistration := false
	for _, sessionID := range permSvc.unregistered {
		if sessionID == string(resp.SessionId) {
			foundUnregistration = true
			break
		}
	}
	if !foundUnregistration {
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

func TestPandoACPAgent_HandleSlashGoalStatusSyncsSessionGoal(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	sessions := agent.sessionService.(*mockSessionService)
	sessions.goals[string(resp.SessionId)] = db.SessionGoal{
		ID:            "goal-1",
		SessionID:     string(resp.SessionId),
		Objective:     "Implement ACP goal mode",
		Status:        "running",
		Iteration:     2,
		MaxIterations: 5,
		StartedAt:     time.Now().Add(-2 * time.Minute).Unix(),
		LastProgress:  sql.NullString{String: "Registered goal mode", Valid: true},
		NextStep:      sql.NullString{String: "Emit session updates", Valid: true},
	}

	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}

	_, err = agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandGoalStatus})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}

	goal := acpSession.Goal()
	if goal == nil {
		t.Fatal("expected session goal state to be populated")
	}
	if goal.Objective != "Implement ACP goal mode" {
		t.Fatalf("unexpected objective %q", goal.Objective)
	}
	if goal.NextStep != "Emit session updates" {
		t.Fatalf("unexpected next step %q", goal.NextStep)
	}
}

func TestPandoACPAgent_HandleSlashGoalCancelCancelsGoal(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	sessions := agent.sessionService.(*mockSessionService)
	sessions.goals[string(resp.SessionId)] = db.SessionGoal{
		ID:            "goal-2",
		SessionID:     string(resp.SessionId),
		Objective:     "Implement ACP goal mode",
		Status:        "running",
		Iteration:     1,
		MaxIterations: 5,
		StartedAt:     time.Now().Add(-time.Minute).Unix(),
	}

	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}

	cancelled := false
	acpSession.SetGoalCancel(func() { cancelled = true })

	stopReason, err := agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandGoalCancel})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonCancelled {
		t.Fatalf("expected cancelled stop reason, got %q", stopReason)
	}
	if !cancelled {
		t.Fatal("expected running goal execution to be cancelled")
	}
	goal := sessions.goals[string(resp.SessionId)]
	if goal.Status != "cancelled" {
		t.Fatalf("expected persisted goal to be cancelled, got %q", goal.Status)
	}
	if acpSession.Goal() == nil || acpSession.Goal().Status != "cancelled" {
		t.Fatalf("expected session goal snapshot to be cancelled, got %+v", acpSession.Goal())
	}
}

func TestPandoACPAgent_HandleSlashGoalWithoutObjectiveShowsUsage(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}

	stopReason, err := agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandGoal})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("expected end turn stop reason, got %q", stopReason)
	}
}

func TestPandoACPAgent_HandleSlashSummarizeStartsManualSummary(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}

	stopReason, err := agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandSummarize})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("expected end turn stop reason, got %q", stopReason)
	}

	mockAgent := agent.agentService.(*mockAgentService)
	if !mockAgent.summarizeCalled {
		t.Fatal("expected summarize to be invoked")
	}
	if mockAgent.summarizeSessionID != string(resp.SessionId) {
		t.Fatalf("expected summarize session %q, got %q", resp.SessionId, mockAgent.summarizeSessionID)
	}
}

func TestPandoACPAgent_HandleSlashPonytailSetsAndClearsMode(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	mockAgent := agent.agentService.(*mockAgentService)

	// /ponytail ultra → sets ultra.
	stop, err := agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandPonytail, Objective: "ultra"})
	if err != nil {
		t.Fatalf("handleSlashCommand(ultra) failed: %v", err)
	}
	if stop != acpsdk.StopReasonEndTurn {
		t.Fatalf("expected end turn, got %q", stop)
	}
	if got := mockAgent.ponytailModes[acpSession.PandoSessionID()]; got != "ultra" {
		t.Fatalf("expected ultra mode stored, got %q", got)
	}

	// /ponytail with no arg → defaults to full.
	if _, err := agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandPonytail}); err != nil {
		t.Fatalf("handleSlashCommand(default) failed: %v", err)
	}
	if got := mockAgent.ponytailModes[acpSession.PandoSessionID()]; got != "full" {
		t.Fatalf("expected full mode default, got %q", got)
	}

	// /ponytail off → clears.
	if _, err := agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandPonytail, Objective: "off"}); err != nil {
		t.Fatalf("handleSlashCommand(off) failed: %v", err)
	}
	if _, ok := mockAgent.ponytailModes[acpSession.PandoSessionID()]; ok {
		t.Fatal("expected ponytail mode cleared after off")
	}

	// Unknown mode → not applied, no error.
	if _, err := agent.handleSlashCommand(ctx, resp.SessionId, acpSession, slashCommand{Kind: slashCommandPonytail, Objective: "bogus"}); err != nil {
		t.Fatalf("handleSlashCommand(bogus) failed: %v", err)
	}
	if _, ok := mockAgent.ponytailModes[acpSession.PandoSessionID()]; ok {
		t.Fatal("expected no mode stored for unknown input")
	}
}

func TestPandoACPAgent_SetSessionConfigOption_SplitsModePermissionAndAgent(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	modeResp, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigModeID),
			Value:     acpsdk.SessionConfigValueId(askModeID),
		},
	})
	if err != nil {
		t.Fatalf("SetSessionConfigOption(mode) failed: %v", err)
	}

	modelResp, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigModelID),
			Value:     acpsdk.SessionConfigValueId("test-model"),
		},
	})
	if err != nil {
		t.Fatalf("SetSessionConfigOption(model) failed: %v", err)
	}

	askPermResp, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigAskPermissionID),
			Value:     acpsdk.SessionConfigValueId(askPermissionNoValue),
		},
	})
	if err != nil {
		t.Fatalf("SetSessionConfigOption(askPermission) failed: %v", err)
	}

	thinkingResp, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigThinkingStreamModeID),
			Value:     acpsdk.SessionConfigValueId(thinkingStreamModeFull),
		},
	})
	if err != nil {
		t.Fatalf("SetSessionConfigOption(thinking_stream_mode) failed: %v", err)
	}

	agentResp, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigAgentID),
			Value:     acpsdk.SessionConfigValueId("assistant"),
		},
	})
	if err != nil {
		t.Fatalf("SetSessionConfigOption(agent) failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()

	if acpSess.Mode() != askModeID {
		t.Fatalf("expected mode %q, got %q", askModeID, acpSess.Mode())
	}
	if acpSess.Model() != "test-model" {
		t.Fatalf("expected model %q, got %q", "test-model", acpSess.Model())
	}
	if acpSess.AskPermission() {
		t.Fatal("expected askPermission selector to override inherited ask-mode permission")
	}
	if !acpSess.PermissionConfigured() {
		t.Fatal("expected explicit askPermission selection to mark permissionConfigured")
	}
	if acpSess.ThinkingStreamMode() != thinkingStreamModeFull {
		t.Fatalf("expected thinking stream mode %q, got %q", thinkingStreamModeFull, acpSess.ThinkingStreamMode())
	}
	if acpSess.Persona() != "assistant" {
		t.Fatalf("expected persona %q, got %q", "assistant", acpSess.Persona())
	}

	for _, cfgResp := range []acpsdk.SetSessionConfigOptionResponse{modeResp, modelResp, askPermResp, thinkingResp, agentResp} {
		if len(cfgResp.ConfigOptions) != 5 {
			t.Fatalf("expected 5 config options in response, got %d", len(cfgResp.ConfigOptions))
		}
	}
}

func TestPandoACPAgent_NewSessionResponse_UsesSeparatedACPSelectors(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	if resp.Modes == nil {
		t.Fatal("expected legacy modes to be present")
	}
	if len(resp.Modes.AvailableModes) != 4 {
		t.Fatalf("expected 4 modes including clean, got %d", len(resp.Modes.AvailableModes))
	}

	if len(resp.ConfigOptions) != 5 {
		t.Fatalf("expected 5 config options, got %d", len(resp.ConfigOptions))
	}

	got := sessionConfigValuesByID(t, resp.ConfigOptions)

	if got[sessionConfigModelID] != "test-model" {
		t.Fatalf("expected model currentValue %q, got %q", "test-model", got[sessionConfigModelID])
	}
	if got[sessionConfigModeID] != agentModeID {
		t.Fatalf("expected mode currentValue %q, got %q", agentModeID, got[sessionConfigModeID])
	}
	if got[sessionConfigAskPermissionID] != askPermissionNoValue {
		t.Fatalf("expected askPermission currentValue %q, got %q", askPermissionNoValue, got[sessionConfigAskPermissionID])
	}
	if got[sessionConfigThinkingStreamModeID] != thinkingStreamModeGrouped {
		t.Fatalf("expected thinking stream mode currentValue %q, got %q", thinkingStreamModeGrouped, got[sessionConfigThinkingStreamModeID])
	}
	if got[sessionConfigAgentID] != "default" {
		t.Fatalf("expected agent currentValue %q, got %q", "default", got[sessionConfigAgentID])
	}
}

func TestPandoACPAgent_NewSessionResponse_ModelAwareThinkingOptions(t *testing.T) {
	reasoningModelID := llmmodels.ModelID("test.reasoning-effort-model")
	llmmodels.SetSupportedModel(llmmodels.Model{
		ID:                      reasoningModelID,
		Name:                    "Reasoning Effort Test Model",
		Provider:                llmmodels.ProviderOpenAI,
		CanReason:               true,
		SupportsReasoningEffort: true,
	})
	defer llmmodels.DeleteSupportedModels(reasoningModelID)

	availableModels := []ACPModelInfo{
		{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
		{ID: string(llmmodels.Claude3Haiku), Name: "Claude 3 Haiku"},
		{ID: string(reasoningModelID), Name: "Reasoning Effort Test Model"},
	}

	testCases := []struct {
		name         string
		currentModel string
		wantPresent  []string
		wantAbsent   []string
	}{
		{
			name:         "anthropic reasoning model shows thinking mode",
			currentModel: string(llmmodels.Claude46Sonnet),
			wantPresent:  []string{sessionConfigThinkingStreamModeID, sessionConfigThinkingModeID},
			wantAbsent:   []string{sessionConfigReasoningEffortID},
		},
		{
			name:         "reasoning effort model shows reasoning effort",
			currentModel: string(reasoningModelID),
			wantPresent:  []string{sessionConfigThinkingStreamModeID, sessionConfigReasoningEffortID},
			wantAbsent:   []string{sessionConfigThinkingModeID},
		},
		{
			name:         "non reasoning model hides inference reasoning controls",
			currentModel: string(llmmodels.Claude3Haiku),
			wantPresent:  []string{sessionConfigThinkingStreamModeID},
			wantAbsent:   []string{sessionConfigReasoningEffortID, sessionConfigThinkingModeID},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			agent := newTestPandoAgentWithModels(tc.currentModel, availableModels...)
			resp, err := agent.NewSession(context.Background(), acpsdk.NewSessionRequest{Cwd: "/tmp"})
			if err != nil {
				t.Fatalf("NewSession failed: %v", err)
			}

			got := sessionConfigValuesByID(t, resp.ConfigOptions)
			for _, optionID := range tc.wantPresent {
				if _, ok := got[optionID]; !ok {
					t.Fatalf("expected config option %q to be present, got %v", optionID, got)
				}
			}
			for _, optionID := range tc.wantAbsent {
				if _, ok := got[optionID]; ok {
					t.Fatalf("expected config option %q to be absent, got %v", optionID, got)
				}
			}
		})
	}
}

func TestPandoACPAgent_NewSession_DefaultsThinkingOverridesForReasoningModel(t *testing.T) {
	availableModels := []ACPModelInfo{
		{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
	}
	agent := newTestPandoAgentWithModels(string(llmmodels.Claude46Sonnet), availableModels...)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	if acpSession.ThinkingStreamMode() != defaultACPThinkingStreamMode {
		t.Fatalf("expected thinking_stream_mode %q, got %q", defaultACPThinkingStreamMode, acpSession.ThinkingStreamMode())
	}
	if acpSession.ThinkingMode() != defaultACPThinkingMode {
		t.Fatalf("expected thinking_mode %q, got %q", defaultACPThinkingMode, acpSession.ThinkingMode())
	}
	if acpSession.ReasoningEffort() != "" {
		t.Fatalf("expected reasoning_effort to remain empty for anthropic model, got %q", acpSession.ReasoningEffort())
	}

	values := sessionConfigValuesByID(t, resp.ConfigOptions)
	if got := values[sessionConfigThinkingStreamModeID]; got != defaultACPThinkingStreamMode {
		t.Fatalf("expected thinking_stream_mode currentValue %q, got %q", defaultACPThinkingStreamMode, got)
	}
	if got := values[sessionConfigThinkingModeID]; got != defaultACPThinkingMode {
		t.Fatalf("expected thinking_mode currentValue %q, got %q", defaultACPThinkingMode, got)
	}

	if _, err := agent.processPromptWithAgent(ctx, acpSession, "hello world"); err != nil {
		t.Fatalf("processPromptWithAgent failed: %v", err)
	}
	mockSvc := agent.agentService.(*mockAgentService)
	if got := mockSvc.sessionOverrides[acpSession.PandoSessionID()]; got.ThinkingMode != defaultACPThinkingMode || got.ReasoningEffort != "" {
		t.Fatalf("expected default thinking override to be forwarded, got %#v", got)
	}

	rawState := agent.sessionService.(*mockSessionService).acpStates[acpSession.PandoSessionID()]
	state, err := unmarshalPersistedACPState(rawState)
	if err != nil {
		t.Fatalf("unmarshal persisted ACP state: %v", err)
	}
	if state.Model != string(llmmodels.Claude46Sonnet) {
		t.Fatalf("expected persisted model %q, got %q", llmmodels.Claude46Sonnet, state.Model)
	}
	if state.ThinkingMode != defaultACPThinkingMode {
		t.Fatalf("expected persisted thinking_mode %q, got %q", defaultACPThinkingMode, state.ThinkingMode)
	}
	if state.ThinkingStreamMode != defaultACPThinkingStreamMode {
		t.Fatalf("expected persisted thinking_stream_mode %q, got %q", defaultACPThinkingStreamMode, state.ThinkingStreamMode)
	}
}

func TestPandoACPAgent_ModelChange_ClearsIncompatibleThinkingOverrides(t *testing.T) {
	reasoningModelID := llmmodels.ModelID("test.reasoning-effort-model")
	llmmodels.SetSupportedModel(llmmodels.Model{
		ID:                      reasoningModelID,
		Name:                    "Reasoning Effort Test Model",
		Provider:                llmmodels.ProviderOpenAI,
		CanReason:               true,
		SupportsReasoningEffort: true,
	})
	defer llmmodels.DeleteSupportedModels(reasoningModelID)

	availableModels := []ACPModelInfo{
		{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
		{ID: string(llmmodels.Claude3Haiku), Name: "Claude 3 Haiku"},
		{ID: string(reasoningModelID), Name: "Reasoning Effort Test Model"},
	}
	agent := newTestPandoAgentWithModels(string(llmmodels.Claude46Sonnet), availableModels...)
	ctx := context.Background()

	var updates bytes.Buffer
	conn := acpsdk.NewAgentSideConnection(
		NewSimpleACPAgent("1.0.0-test", log.New(io.Discard, "", 0)),
		&updates,
		bytes.NewReader(nil),
	)
	agent.SetConnection(conn)

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	if _, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigThinkingModeID),
			Value:     acpsdk.SessionConfigValueId(thinkingModeHigh),
		},
	}); err != nil {
		t.Fatalf("SetSessionConfigOption(thinking_mode) failed: %v", err)
	}

	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	if acpSession.ThinkingMode() != thinkingModeHigh {
		t.Fatalf("expected thinking mode %q, got %q", thinkingModeHigh, acpSession.ThinkingMode())
	}

	modelResp, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigModelID),
			Value:     acpsdk.SessionConfigValueId(string(reasoningModelID)),
		},
	})
	if err != nil {
		t.Fatalf("SetSessionConfigOption(model) failed: %v", err)
	}

	if acpSession.ThinkingMode() != "" {
		t.Fatalf("expected incompatible thinking mode to be cleared, got %q", acpSession.ThinkingMode())
	}
	if acpSession.ReasoningEffort() != defaultACPReasoningEffort {
		t.Fatalf("expected reasoning effort to default to %q, got %q", defaultACPReasoningEffort, acpSession.ReasoningEffort())
	}
	modelValues := sessionConfigValuesByID(t, modelResp.ConfigOptions)
	if _, ok := modelValues[sessionConfigThinkingModeID]; ok {
		t.Fatalf("expected thinking_mode option to be removed after model switch, got %v", modelValues)
	}
	if got := modelValues[sessionConfigReasoningEffortID]; got != defaultACPReasoningEffort {
		t.Fatalf("expected reasoning_effort currentValue %q, got %q", defaultACPReasoningEffort, got)
	}

	if _, err := agent.SetSessionConfigOption(ctx, acpsdk.SetSessionConfigOptionRequest{
		ValueId: &acpsdk.SetSessionConfigOptionValueId{
			SessionId: resp.SessionId,
			ConfigId:  acpsdk.SessionConfigId(sessionConfigReasoningEffortID),
			Value:     acpsdk.SessionConfigValueId(reasoningEffortHigh),
		},
	}); err != nil {
		t.Fatalf("SetSessionConfigOption(reasoning_effort) failed: %v", err)
	}
	if acpSession.ReasoningEffort() != reasoningEffortHigh {
		t.Fatalf("expected reasoning effort %q, got %q", reasoningEffortHigh, acpSession.ReasoningEffort())
	}

	updates.Reset()
	if _, err := agent.UnstableSetSessionModel(ctx, acpsdk.UnstableSetSessionModelRequest{
		SessionId: resp.SessionId,
		ModelId:   acpsdk.UnstableModelId(llmmodels.Claude3Haiku),
	}); err != nil {
		t.Fatalf("UnstableSetSessionModel failed: %v", err)
	}

	if acpSession.ReasoningEffort() != "" {
		t.Fatalf("expected incompatible reasoning effort to be cleared, got %q", acpSession.ReasoningEffort())
	}
	rawUpdates := updates.String()
	if !strings.Contains(rawUpdates, sessionConfigThinkingStreamModeID) || strings.Contains(rawUpdates, sessionConfigReasoningEffortID) || strings.Contains(rawUpdates, sessionConfigThinkingModeID) {
		t.Fatalf("expected refreshed config options for non-reasoning model, got raw updates: %s", rawUpdates)
	}
}

func TestPandoACPAgent_ProcessAgentEventStream_HonorsThinkingStreamMode(t *testing.T) {
	testCases := []struct {
		name string
		mode string
		want []string
	}{
		{
			name: "off streams only final reasoning",
			mode: thinkingStreamModeOff,
			want: []string{"alphabeta"},
		},
		{
			name: "grouped flushes buffered reasoning once",
			mode: thinkingStreamModeGrouped,
			want: []string{"alphabeta"},
		},
		{
			name: "full streams each thinking delta",
			mode: thinkingStreamModeFull,
			want: []string{"alpha", "beta"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			agent := newTestPandoAgent()
			acpSession, updates := newThoughtCaptureSession(t)
			acpSession.SetThinkingStreamMode(tc.mode)

			eventChan := make(chan AgentEvent, 3)
			eventChan <- AgentEvent{Type: AgentEventTypeThinkingDelta, Delta: "alpha"}
			eventChan <- AgentEvent{Type: AgentEventTypeThinkingDelta, Delta: "beta"}
			eventChan <- AgentEvent{
				Type: AgentEventTypeResponse,
				Message: message.Message{
					ID: "assistant-msg-1",
					Parts: []message.ContentPart{
						message.ReasoningContent{Thinking: "alphabeta"},
					},
				},
			}
			close(eventChan)

			stopReason, err := agent.processAgentEventStream(context.Background(), acpSession, eventChan)
			if err != nil {
				t.Fatalf("processAgentEventStream failed: %v", err)
			}
			if stopReason != acpsdk.StopReasonEndTurn {
				t.Fatalf("unexpected stop reason: %q", stopReason)
			}

			if got := thoughtUpdateTexts(t, updates.String()); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected thought updates for mode %q: got %#v want %#v\nraw=%s", tc.mode, got, tc.want, updates.String())
			}
		})
	}
}

func TestPandoACPAgent_ProcessAgentEventStream_LogsGroupedThinkingFlush(t *testing.T) {
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", logger, &mockAgentService{}, newMockSessionService(), nil)
	acpSession, _ := newThoughtCaptureSession(t)
	acpSession.SetThinkingStreamMode(thinkingStreamModeGrouped)

	eventChan := make(chan AgentEvent, 2)
	eventChan <- AgentEvent{Type: AgentEventTypeThinkingDelta, Delta: strings.Repeat("x", acpGroupedThinkingFlushChars)}
	eventChan <- AgentEvent{
		Type: AgentEventTypeResponse,
		Message: message.Message{
			ID: "assistant-msg-1",
			Parts: []message.ContentPart{
				message.ReasoningContent{Thinking: strings.Repeat("x", acpGroupedThinkingFlushChars)},
			},
		},
	}
	close(eventChan)

	if _, err := agent.processAgentEventStream(context.Background(), acpSession, eventChan); err != nil {
		t.Fatalf("processAgentEventStream failed: %v", err)
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, `Applied thinking stream mode "grouped"`) {
		t.Fatalf("expected applied thinking stream mode log, got %s", logOutput)
	}
	if !strings.Contains(logOutput, "Flushing grouped thinking") || !strings.Contains(logOutput, "reason=size") {
		t.Fatalf("expected grouped thinking flush log with size reason, got %s", logOutput)
	}
}

func TestPandoACPAgent_ProcessPromptWithAgent_LogsAppliedThinkingSettings(t *testing.T) {
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	mockAgent := &mockAgentService{
		currentModel: string(llmmodels.Claude46Sonnet),
		availableModels: []ACPModelInfo{
			{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
		},
	}
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", logger, mockAgent, newMockSessionService(), nil)
	acpSession := NewACPServerSession(acpsdk.SessionId("session-1"), "/tmp", nil, "pando-session-1")
	acpSession.SetModel(string(llmmodels.Claude46Sonnet))
	acpSession.SetThinkingStreamMode(thinkingStreamModeFull)
	acpSession.SetThinkingMode(thinkingModeHigh)

	if _, err := agent.processPromptWithAgent(context.Background(), acpSession, "hello world"); err != nil {
		t.Fatalf("processPromptWithAgent failed: %v", err)
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, `Applying ACP session overrides for session session-1: model="anthropic.claude-sonnet-4-6" stream_mode="full" reasoning_effort="" thinking_mode="high"`) {
		t.Fatalf("expected ACP session overrides log, got %s", logOutput)
	}
}

func TestBuildSessionConfigOptions_ThinkingVisibilityDescriptions(t *testing.T) {
	agent := newTestPandoAgentWithModels(string(llmmodels.Claude46Sonnet),
		ACPModelInfo{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
	)
	session := NewACPServerSession(acpsdk.SessionId("session-1"), "/tmp", nil, "pando-session-1")
	session.SetModel(string(llmmodels.Claude46Sonnet))

	options := buildSessionConfigOptions(agent.agentService, session)
	option := sessionConfigOptionByID(t, options, sessionConfigThinkingStreamModeID)
	if option.Select == nil || option.Select.Description == nil {
		t.Fatalf("expected thinking visibility description, got %+v", option)
	}
	if got := *option.Select.Description; got != "How reasoning is shown while the model is generating a response" {
		t.Fatalf("unexpected thinking visibility description %q", got)
	}

	values := configOptionValueDescriptionsByID(t, option)
	if got := values[thinkingStreamModeOff]; got != "Show only the final reasoning summary" {
		t.Fatalf("unexpected off description %q", got)
	}
	if got := values[thinkingStreamModeGrouped]; got != "Show reasoning in periodic grouped blocks" {
		t.Fatalf("unexpected grouped description %q", got)
	}
	if got := values[thinkingStreamModeFull]; got != "Show every reasoning chunk as it arrives; may be noisy" {
		t.Fatalf("unexpected full description %q", got)
	}
}

func newThoughtCaptureSession(t *testing.T) (*ACPServerSession, *bytes.Buffer) {
	t.Helper()

	var updates bytes.Buffer
	conn := acpsdk.NewAgentSideConnection(
		NewSimpleACPAgent("1.0.0-test", log.New(io.Discard, "", 0)),
		&updates,
		bytes.NewReader(nil),
	)

	return NewACPServerSession(acpsdk.SessionId("session-1"), "/tmp", conn, "pando-session-1"), &updates
}

func thoughtUpdateTexts(t *testing.T, raw string) []string {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(raw))
	type notification struct {
		Params struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		} `json:"params"`
	}

	var texts []string
	for {
		var note notification
		if err := decoder.Decode(&note); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode ACP update: %v", err)
		}
		if note.Params.Update.SessionUpdate == "agent_thought_chunk" {
			texts = append(texts, note.Params.Update.Content.Text)
		}
	}

	return texts
}

func TestAvailableCommands_ExposeGoalSlashCommands(t *testing.T) {
	commands := availableCommands()
	if len(commands) != 18 {
		t.Fatalf("expected 18 available commands, got %d", len(commands))
	}

	got := map[string]acpsdk.AvailableCommand{}
	for _, cmd := range commands {
		got[cmd.Name] = cmd
		if strings.HasPrefix(cmd.Name, "/") {
			t.Fatalf("expected available command %q to omit leading slash for ACP clients", cmd.Name)
		}
	}

	for _, name := range []string{goalCommandToken, autopilotCommandToken, goalStatusCommandToken, goalCancelCommandToken, compactCommandToken, summarizeCommandToken, dbCompactCommandToken, ponytailCommandToken, improveAgentsMdCommandToken, superpowersCommandToken, superpowersFinishCommandToken, cavemanCommandToken, cavemanFinishCommandToken, learningCommandToken, learningFinishCommandToken, vulnhuntCommandToken, vulnhunterFixCommandToken, vulnhuntFixVerifyCommandToken} {
		if _, ok := got[name]; !ok {
			t.Fatalf("expected available command %q to be exposed", name)
		}
	}

	for _, name := range []string{goalCommandToken, autopilotCommandToken, ponytailCommandToken, improveAgentsMdCommandToken, superpowersCommandToken, cavemanCommandToken, learningCommandToken, vulnhuntCommandToken, vulnhunterFixCommandToken, vulnhuntFixVerifyCommandToken} {
		cmd := got[name]
		if cmd.Input == nil || cmd.Input.Unstructured == nil || strings.TrimSpace(cmd.Input.Unstructured.Hint) == "" {
			t.Fatalf("expected available command %q to include an unstructured input hint, got %#v", name, cmd.Input)
		}
	}

	for _, name := range []string{goalStatusCommandToken, goalCancelCommandToken, compactCommandToken, summarizeCommandToken, dbCompactCommandToken, superpowersFinishCommandToken, cavemanFinishCommandToken, learningFinishCommandToken} {
		if got[name].Input != nil {
			t.Fatalf("expected available command %q to omit input metadata, got %#v", name, got[name].Input)
		}
	}

	for _, cmd := range commands {
		payload, err := json.Marshal(cmd)
		if err != nil {
			t.Fatalf("marshal available command %q: %v", cmd.Name, err)
		}
		if strings.Contains(string(payload), "\"input\":{}") {
			t.Fatalf("expected available command %q to avoid empty input payloads, got %s", cmd.Name, payload)
		}
	}
}

func TestParseSlashCommand_UsesRegistry(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   slashCommand
		wantOK bool
	}{
		{name: "goal with objective", input: "/goal ship it", want: slashCommand{Kind: slashCommandGoal, Objective: "ship it"}, wantOK: true},
		{name: "goal without objective", input: "/goal", want: slashCommand{Kind: slashCommandGoal, Objective: ""}, wantOK: true},
		{name: "autopilot alias", input: "/autopilot investigate", want: slashCommand{Kind: slashCommandGoal, Objective: "investigate"}, wantOK: true},
		{name: "goal status", input: "/goal-status", want: slashCommand{Kind: slashCommandGoalStatus}, wantOK: true},
		{name: "compact", input: "/compact", want: slashCommand{Kind: slashCommandSummarize}, wantOK: true},
		{name: "summarize alias", input: "/summarize", want: slashCommand{Kind: slashCommandSummarize}, wantOK: true},
		{name: "db-compact", input: "/db-compact", want: slashCommand{Kind: slashCommandDBCompact}, wantOK: true},
		{name: "unknown", input: "/unknown", want: slashCommand{}, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSlashCommand(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("parseSlashCommand(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("parseSlashCommand(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPandoACPAgent_ResumeSession_IncludesConfigOptions(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	newResp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	resumeResp, err := agent.ResumeSession(ctx, acpsdk.ResumeSessionRequest{
		SessionId: newResp.SessionId,
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}

	if len(resumeResp.ConfigOptions) != 5 {
		t.Fatalf("expected 5 resume config options, got %d", len(resumeResp.ConfigOptions))
	}
}

func TestPandoACPAgent_LoadSession_RestoresPersistedACPThinkingPreferences(t *testing.T) {
	agent := newTestPandoAgentWithModels(
		string(llmmodels.Claude46Sonnet),
		ACPModelInfo{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
	)
	ctx := context.Background()
	sessID := "persisted-load-session"
	sessionSvc := agent.sessionService.(*mockSessionService)
	sessionSvc.sessions[sessID] = ACPSessionInfo{ID: sessID, Title: "Persisted Load"}

	payload, err := json.Marshal(persistedACPState{
		Model:              string(llmmodels.Claude46Sonnet),
		ThinkingMode:       thinkingModeHigh,
		ThinkingStreamMode: thinkingStreamModeFull,
	})
	if err != nil {
		t.Fatalf("marshal persisted state: %v", err)
	}
	sessionSvc.acpStates[sessID] = string(payload)

	resp, err := agent.LoadSession(ctx, acpsdk.LoadSessionRequest{
		SessionId: acpsdk.SessionId(sessID),
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	acpSession, err := agent.getSession(acpsdk.SessionId(sessID))
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	if acpSession.Model() != string(llmmodels.Claude46Sonnet) {
		t.Fatalf("expected restored model %q, got %q", llmmodels.Claude46Sonnet, acpSession.Model())
	}
	if acpSession.ThinkingMode() != thinkingModeHigh {
		t.Fatalf("expected restored thinking_mode %q, got %q", thinkingModeHigh, acpSession.ThinkingMode())
	}
	if acpSession.ThinkingStreamMode() != thinkingStreamModeFull {
		t.Fatalf("expected restored thinking_stream_mode %q, got %q", thinkingStreamModeFull, acpSession.ThinkingStreamMode())
	}

	values := sessionConfigValuesByID(t, resp.ConfigOptions)
	if got := values[sessionConfigThinkingModeID]; got != thinkingModeHigh {
		t.Fatalf("expected LoadSession thinking_mode %q, got %q", thinkingModeHigh, got)
	}
	if got := values[sessionConfigThinkingStreamModeID]; got != thinkingStreamModeFull {
		t.Fatalf("expected LoadSession thinking_stream_mode %q, got %q", thinkingStreamModeFull, got)
	}
}

func TestPandoACPAgent_ResumeSession_RestoresPersistedACPThinkingPreferences(t *testing.T) {
	agent := newTestPandoAgentWithModels(
		string(llmmodels.Claude46Sonnet),
		ACPModelInfo{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
	)
	ctx := context.Background()
	sessID := "persisted-resume-session"
	sessionSvc := agent.sessionService.(*mockSessionService)
	sessionSvc.sessions[sessID] = ACPSessionInfo{ID: sessID, Title: "Persisted Resume"}

	payload, err := json.Marshal(persistedACPState{
		Model:              string(llmmodels.Claude46Sonnet),
		ThinkingMode:       thinkingModeLow,
		ThinkingStreamMode: thinkingStreamModeOff,
	})
	if err != nil {
		t.Fatalf("marshal persisted state: %v", err)
	}
	sessionSvc.acpStates[sessID] = string(payload)

	resp, err := agent.ResumeSession(ctx, acpsdk.ResumeSessionRequest{
		SessionId: acpsdk.SessionId(sessID),
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}

	acpSession, err := agent.getSession(acpsdk.SessionId(sessID))
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	if acpSession.Model() != string(llmmodels.Claude46Sonnet) {
		t.Fatalf("expected restored model %q, got %q", llmmodels.Claude46Sonnet, acpSession.Model())
	}
	if acpSession.ThinkingMode() != thinkingModeLow {
		t.Fatalf("expected restored thinking_mode %q, got %q", thinkingModeLow, acpSession.ThinkingMode())
	}
	if acpSession.ThinkingStreamMode() != thinkingStreamModeOff {
		t.Fatalf("expected restored thinking_stream_mode %q, got %q", thinkingStreamModeOff, acpSession.ThinkingStreamMode())
	}

	values := unstableSessionConfigValuesByID(t, resp.ConfigOptions)
	if got := values[sessionConfigThinkingModeID]; got != thinkingModeLow {
		t.Fatalf("expected ResumeSession thinking_mode %q, got %q", thinkingModeLow, got)
	}
	if got := values[sessionConfigThinkingStreamModeID]; got != thinkingStreamModeOff {
		t.Fatalf("expected ResumeSession thinking_stream_mode %q, got %q", thinkingStreamModeOff, got)
	}
}

// TestPandoACPAgent_SetSessionModel verifies model updates.
func TestPandoACPAgent_SetSessionModel(t *testing.T) {
	agent := newTestPandoAgentWithModels(string(llmmodels.Claude46Sonnet),
		ACPModelInfo{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
		ACPModelInfo{ID: string(llmmodels.Claude3Haiku), Name: "Claude 3 Haiku"},
	)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = agent.UnstableSetSessionModel(ctx, acpsdk.UnstableSetSessionModelRequest{
		SessionId: resp.SessionId,
		ModelId:   acpsdk.UnstableModelId(llmmodels.Claude46Sonnet),
	})
	if err != nil {
		t.Fatalf("UnstableSetSessionModel failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()

	if acpSess.Model() != string(llmmodels.Claude46Sonnet) {
		t.Errorf("Expected model %q, got %q", llmmodels.Claude46Sonnet, acpSess.Model())
	}

	mockSvc := agent.agentService.(*mockAgentService)
	overrides, ok := mockSvc.sessionOverrides[acpSess.PandoSessionID()]
	if !ok {
		t.Fatal("expected session overrides to be updated after model change")
	}
	if overrides.Model != string(llmmodels.Claude46Sonnet) {
		t.Fatalf("override model = %q, want %q", overrides.Model, llmmodels.Claude46Sonnet)
	}
}

func TestPandoACPAgent_CurrentUsageSnapshotPrefersSelectedModelContextWindow(t *testing.T) {
	agent := newTestPandoAgentWithModels(string(llmmodels.Claude3Haiku),
		ACPModelInfo{ID: string(llmmodels.Claude46Sonnet), Name: "Claude Sonnet 4.6"},
		ACPModelInfo{ID: string(llmmodels.Claude3Haiku), Name: "Claude 3 Haiku"},
	)
	ctx := context.Background()
	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()
	acpSess.SetModel(string(llmmodels.Claude46Sonnet))

	svc := agent.sessionService.(*mockSessionService)
	sess := svc.sessions[acpSess.PandoSessionID()]
	sess.PromptTokens = 1200
	sess.CompletionTokens = 300
	sess.ContextWindow = 1024
	svc.sessions[acpSess.PandoSessionID()] = sess

	used, size, ok := agent.currentUsageSnapshot(ctx, acpSess)
	if !ok {
		t.Fatal("expected currentUsageSnapshot to succeed")
	}
	if used != 1500 {
		t.Fatalf("used = %d, want %d", used, 1500)
	}
	if size != int(llmmodels.SupportedModels()[llmmodels.Claude46Sonnet].ContextWindow) {
		t.Fatalf("size = %d, want model context window %d", size, llmmodels.SupportedModels()[llmmodels.Claude46Sonnet].ContextWindow)
	}
}

func TestPandoACPAgent_CurrentUsageSnapshotFallsBackToSessionContextWindow(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()
	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()
	acpSess.SetModel("unknown-provider-model")

	svc := agent.sessionService.(*mockSessionService)
	sess := svc.sessions[acpSess.PandoSessionID()]
	sess.PromptTokens = 10
	sess.CompletionTokens = 5
	sess.ContextWindow = 4096
	svc.sessions[acpSess.PandoSessionID()] = sess

	_, size, ok := agent.currentUsageSnapshot(ctx, acpSess)
	if !ok {
		t.Fatal("expected currentUsageSnapshot to succeed")
	}
	if size != 4096 {
		t.Fatalf("size = %d, want %d", size, 4096)
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
	list, err := agent.ListSessions(ctx, acpsdk.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(list.Sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(list.Sessions))
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

// TestPandoACPAgent_SetSessionPersona verifies persona updates per session.
func TestPandoACPAgent_SetSessionPersona(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	// Create session first
	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Set persona to "assistant"
	err = agent.SetSessionPersona(ctx, resp.SessionId, "assistant")
	if err != nil {
		t.Fatalf("SetSessionPersona failed: %v", err)
	}

	agent.sessionsMu.RLock()
	acpSess := agent.sessions[resp.SessionId]
	agent.sessionsMu.RUnlock()

	if acpSess.Persona() != "assistant" {
		t.Errorf("Expected persona 'assistant', got %q", acpSess.Persona())
	}

	// Clear persona
	err = agent.SetSessionPersona(ctx, resp.SessionId, "")
	if err != nil {
		t.Fatalf("SetSessionPersona (clear) failed: %v", err)
	}

	if acpSess.Persona() != "" {
		t.Errorf("Expected persona cleared, got %q", acpSess.Persona())
	}

	// Invalid persona should fail
	err = agent.SetSessionPersona(ctx, resp.SessionId, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent persona, got nil")
	}
}

// TestPandoACPAgent_NewSessionResponse_IncludesPersonaState verifies that
// NewSession and LoadSession responses include persona state in Meta.
func TestPandoACPAgent_NewSessionResponse_IncludesPersonaState(t *testing.T) {
	agent := newTestPandoAgent()
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	// Meta should contain persona state (since the mock returns personas)
	if resp.Meta == nil {
		t.Fatal("Expected Meta with persona state, got nil")
	}

	personaState := buildSessionPersonaState(agent.agentService, "")
	if personaState == nil {
		t.Fatal("Expected persona state, got nil")
	}

	if len(personaState.AvailablePersonas) != 2 {
		t.Errorf("Expected 2 available personas, got %d", len(personaState.AvailablePersonas))
	}

	if personaState.CurrentPersonaId != "default" {
		t.Errorf("Expected current persona 'default', got %q", personaState.CurrentPersonaId)
	}
}
