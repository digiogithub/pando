package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

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
	runCalled        bool
	cancelCalled     bool
	runErr           error
	modelOverride    string
	modelOverrideErr error
	copilotUsageErr  error
	claudeUsageErr   error
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

func (m *mockAgentService) ListPersonas() []string {
	return []string{"default", "assistant"}
}

func (m *mockAgentService) GetActivePersona() string {
	return "default"
}

func (m *mockAgentService) SetActivePersona(name string) error {
	return nil
}

func (m *mockAgentService) ListAvailableTools() []ACPToolInfo {
	return []ACPToolInfo{
		{Name: "bash", Description: "Execute bash commands"},
		{Name: "edit", Description: "Edit files"},
	}
}

func (m *mockAgentService) OpenCopilotUsage() error {
	return m.copilotUsageErr
}

func (m *mockAgentService) OpenClaudeUsage() error {
	return m.claudeUsageErr
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

func (m *mockSessionService) GetMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return nil, nil
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
	if !acpSess.AskPermission() {
		t.Error("Expected legacy ask mode to enable ask-permission compatibility")
	}
	if acpSess.PermissionConfigured() {
		t.Error("Expected legacy SetSessionMode to keep permission in inherited mode")
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
	if acpSess.Persona() != "assistant" {
		t.Fatalf("expected persona %q, got %q", "assistant", acpSess.Persona())
	}

	for _, cfgResp := range []acpsdk.SetSessionConfigOptionResponse{modeResp, modelResp, askPermResp, agentResp} {
		if len(cfgResp.ConfigOptions) != 4 {
			t.Fatalf("expected 4 config options in response, got %d", len(cfgResp.ConfigOptions))
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
	if len(resp.Modes.AvailableModes) != 2 {
		t.Fatalf("expected 2 legacy modes, got %d", len(resp.Modes.AvailableModes))
	}

	if len(resp.ConfigOptions) != 4 {
		t.Fatalf("expected 4 config options, got %d", len(resp.ConfigOptions))
	}

	got := map[string]string{}
	for _, opt := range resp.ConfigOptions {
		if opt.Select == nil {
			t.Fatalf("expected select config option, got %+v", opt)
		}
		got[string(opt.Select.Id)] = string(opt.Select.CurrentValue)
	}

	if got[sessionConfigModelID] != "test-model" {
		t.Fatalf("expected model currentValue %q, got %q", "test-model", got[sessionConfigModelID])
	}
	if got[sessionConfigModeID] != agentModeID {
		t.Fatalf("expected mode currentValue %q, got %q", agentModeID, got[sessionConfigModeID])
	}
	if got[sessionConfigAskPermissionID] != askPermissionNoValue {
		t.Fatalf("expected askPermission currentValue %q, got %q", askPermissionNoValue, got[sessionConfigAskPermissionID])
	}
	if got[sessionConfigAgentID] != "default" {
		t.Fatalf("expected agent currentValue %q, got %q", "default", got[sessionConfigAgentID])
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

	if len(resumeResp.ConfigOptions) != 4 {
		t.Fatalf("expected 4 resume config options, got %d", len(resumeResp.ConfigOptions))
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

	_, err = agent.UnstableSetSessionModel(ctx, acpsdk.UnstableSetSessionModelRequest{
		SessionId: resp.SessionId,
		ModelId:   "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("UnstableSetSessionModel failed: %v", err)
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
