package sessions

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

// ---- mock message.Service ----

type mockMessageService struct {
	messages map[string][]message.Message
}

func (m *mockMessageService) Subscribe(ctx context.Context) <-chan pubsub.Event[message.Message] {
	ch := make(chan pubsub.Event[message.Message])
	go func() { <-ctx.Done(); close(ch) }()
	return ch
}

func (m *mockMessageService) Create(_ context.Context, _ string, _ message.CreateMessageParams) (message.Message, error) {
	return message.Message{}, nil
}

func (m *mockMessageService) Update(_ context.Context, _ message.Message) error { return nil }

func (m *mockMessageService) Get(_ context.Context, id string) (message.Message, error) {
	return message.Message{}, nil
}

func (m *mockMessageService) List(_ context.Context, sessionID string) ([]message.Message, error) {
	return m.messages[sessionID], nil
}

func (m *mockMessageService) Delete(_ context.Context, _ string) error { return nil }

func (m *mockMessageService) DeleteSessionMessages(_ context.Context, _ string) error { return nil }

// ---- mock session.Service ----

type mockSessionService struct {
	sessions map[string]session.Session
}

func (m *mockSessionService) Subscribe(ctx context.Context) <-chan pubsub.Event[session.Session] {
	ch := make(chan pubsub.Event[session.Session])
	go func() { <-ctx.Done(); close(ch) }()
	return ch
}

func (m *mockSessionService) Create(_ context.Context, title string) (session.Session, error) {
	return session.Session{Title: title}, nil
}

func (m *mockSessionService) CreateTitleSession(_ context.Context, _ string) (session.Session, error) {
	return session.Session{}, nil
}

func (m *mockSessionService) CreateTaskSession(_ context.Context, _, _, title string) (session.Session, error) {
	return session.Session{Title: title}, nil
}

func (m *mockSessionService) Get(_ context.Context, id string) (session.Session, error) {
	s, ok := m.sessions[id]
	if !ok {
		return session.Session{ID: id, Title: "Test Session"}, nil
	}
	return s, nil
}

func (m *mockSessionService) List(_ context.Context) ([]session.Session, error) {
	return nil, nil
}

func (m *mockSessionService) Save(_ context.Context, s session.Session) (session.Session, error) {
	return s, nil
}

func (m *mockSessionService) Delete(_ context.Context, _ string) error { return nil }

// ---- helpers ----

func makeMsg(role message.MessageRole, text string) message.Message {
	return message.Message{
		Role:  role,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	}
}

func newTestIndexer(t *testing.T, msgs map[string][]message.Message, sessions map[string]session.Session) (*SessionIndexer, *SessionRAGStore) {
	t.Helper()
	store, _ := newTestStore(t)
	msgSvc := &mockMessageService{messages: msgs}
	sessSvc := &mockSessionService{sessions: sessions}
	return NewSessionIndexer(store, msgSvc, sessSvc), store
}

// ---- ExtractConversationText tests ----

func TestExtractConversationText_BasicRoles(t *testing.T) {
	msgs := []message.Message{
		makeMsg(message.User, "Hello there"),
		makeMsg(message.Assistant, "Hi! How can I help you?"),
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{Content: "tool output"}}},
	}

	result := ExtractConversationText(msgs)

	if !strings.Contains(result, "[user]: Hello there") {
		t.Errorf("expected user message, got: %q", result)
	}
	if !strings.Contains(result, "[assistant]: Hi! How can I help you?") {
		t.Errorf("expected assistant message, got: %q", result)
	}
	if strings.Contains(result, "tool output") {
		t.Errorf("tool messages should be excluded, got: %q", result)
	}
}

func TestExtractConversationText_IncludesReasoning(t *testing.T) {
	msgs := []message.Message{
		{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ReasoningContent{Thinking: "thinking step"},
				message.TextContent{Text: "final answer"},
			},
		},
	}

	result := ExtractConversationText(msgs)

	if !strings.Contains(result, "thinking step") {
		t.Errorf("expected reasoning content, got: %q", result)
	}
	if !strings.Contains(result, "final answer") {
		t.Errorf("expected text content, got: %q", result)
	}
}

func TestExtractConversationText_TruncatesLongMessages(t *testing.T) {
	longText := strings.Repeat("a", maxMessageTextLen+100)
	msgs := []message.Message{
		makeMsg(message.User, longText),
	}

	result := ExtractConversationText(msgs)

	if !strings.Contains(result, "...[truncated]") {
		t.Errorf("expected truncation marker, got length %d", len(result))
	}
}

func TestExtractConversationText_IgnoresToolCalls(t *testing.T) {
	msgs := []message.Message{
		makeMsg(message.User, "run a tool"),
		{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "1", Name: "bash", Input: "ls"},
				message.TextContent{Text: "Done!"},
			},
		},
	}

	result := ExtractConversationText(msgs)

	if strings.Contains(result, "bash") {
		t.Errorf("tool calls should be excluded from text, got: %q", result)
	}
	if !strings.Contains(result, "Done!") {
		t.Errorf("expected assistant text, got: %q", result)
	}
}

func TestExtractConversationText_EmptyMessages(t *testing.T) {
	result := ExtractConversationText(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// ---- CountTurns tests ----

func TestCountTurns(t *testing.T) {
	tests := []struct {
		name  string
		msgs  []message.Message
		turns int
	}{
		{
			name:  "empty",
			msgs:  nil,
			turns: 0,
		},
		{
			name: "one pair",
			msgs: []message.Message{
				makeMsg(message.User, "hi"),
				makeMsg(message.Assistant, "hello"),
			},
			turns: 1,
		},
		{
			name: "two pairs",
			msgs: []message.Message{
				makeMsg(message.User, "q1"),
				makeMsg(message.Assistant, "a1"),
				makeMsg(message.User, "q2"),
				makeMsg(message.Assistant, "a2"),
			},
			turns: 2,
		},
		{
			name: "user without assistant",
			msgs: []message.Message{
				makeMsg(message.User, "hello"),
			},
			turns: 0,
		},
		{
			name: "assistant only",
			msgs: []message.Message{
				makeMsg(message.Assistant, "hi"),
			},
			turns: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountTurns(tt.msgs)
			if got != tt.turns {
				t.Errorf("CountTurns() = %d, want %d", got, tt.turns)
			}
		})
	}
}

// ---- IndexSession tests ----

func TestIndexerIndexSession_Success(t *testing.T) {
	sessionID := "test-sess-1"
	msgs := map[string][]message.Message{
		sessionID: {
			makeMsg(message.User, "What is Go? Go is a programming language created by Google."),
			makeMsg(message.Assistant, "Go is a statically typed, compiled language designed for simplicity and efficiency."),
			makeMsg(message.User, "How do I write a goroutine?"),
			makeMsg(message.Assistant, "You can use the go keyword before a function call to run it concurrently."),
		},
	}

	indexer, store := newTestIndexer(t, msgs, nil)
	ctx := context.Background()

	if err := indexer.IndexSession(ctx, sessionID); err != nil {
		t.Fatalf("IndexSession: %v", err)
	}

	doc, err := store.GetSessionDocument(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionDocument: %v", err)
	}
	if doc == nil {
		t.Fatal("expected document, got nil")
	}
	if doc.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", doc.SessionID, sessionID)
	}
	if doc.MessageCount != 4 {
		t.Errorf("MessageCount = %d, want 4", doc.MessageCount)
	}
}

func TestIndexerIndexSession_TooFewMessages(t *testing.T) {
	sessionID := "test-sess-2"
	msgs := map[string][]message.Message{
		sessionID: {
			makeMsg(message.User, "hi"),
		},
	}

	indexer, store := newTestIndexer(t, msgs, nil)
	ctx := context.Background()

	// Should return nil without indexing
	if err := indexer.IndexSession(ctx, sessionID); err != nil {
		t.Fatalf("IndexSession: %v", err)
	}

	doc, err := store.GetSessionDocument(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionDocument: %v", err)
	}
	if doc != nil {
		t.Error("expected no document for session with too few messages, got one")
	}
}

func TestIndexerIndexSession_ShortText(t *testing.T) {
	sessionID := "test-sess-3"
	// Two messages but very short combined text (< minConversationLen)
	msgs := map[string][]message.Message{
		sessionID: {
			makeMsg(message.User, "hi"),
			makeMsg(message.Assistant, "hello"),
		},
	}

	indexer, store := newTestIndexer(t, msgs, nil)
	ctx := context.Background()

	if err := indexer.IndexSession(ctx, sessionID); err != nil {
		t.Fatalf("IndexSession: %v", err)
	}

	doc, err := store.GetSessionDocument(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionDocument: %v", err)
	}
	if doc != nil {
		t.Error("expected no document for session with short text, got one")
	}
}
