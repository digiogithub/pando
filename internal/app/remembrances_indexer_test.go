package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/session"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

type recordingEmbedder struct {
	texts []string
	err   error
}

func (e *recordingEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.texts = append([]string(nil), texts...)
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1), float32(len(texts[i]))}
	}
	return out, nil
}

func (e *recordingEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("unexpected EmbedQuery call")
}

func (e *recordingEmbedder) Dimension() int { return 2 }

type indexingMessagesService struct {
	msgs []message.Message
}

func (s *indexingMessagesService) Subscribe(ctx context.Context) <-chan pubsub.Event[message.Message] {
	return nil
}
func (s *indexingMessagesService) Create(ctx context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	return message.Message{}, errors.New("not implemented")
}
func (s *indexingMessagesService) Update(ctx context.Context, msg message.Message) error {
	return errors.New("not implemented")
}
func (s *indexingMessagesService) Get(ctx context.Context, id string) (message.Message, error) {
	return message.Message{}, errors.New("not implemented")
}
func (s *indexingMessagesService) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	return s.msgs, nil
}
func (s *indexingMessagesService) Delete(ctx context.Context, id string) error {
	return errors.New("not implemented")
}
func (s *indexingMessagesService) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	return errors.New("not implemented")
}

type indexingSessionService struct {
	sess session.Session
}

func (s *indexingSessionService) Subscribe(ctx context.Context) <-chan pubsub.Event[session.Session] {
	return nil
}
func (s *indexingSessionService) Create(ctx context.Context, title string) (session.Session, error) {
	return session.Session{}, errors.New("not implemented")
}
func (s *indexingSessionService) CreateTitleSession(ctx context.Context, parentSessionID string) (session.Session, error) {
	return session.Session{}, errors.New("not implemented")
}
func (s *indexingSessionService) CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (session.Session, error) {
	return session.Session{}, errors.New("not implemented")
}
func (s *indexingSessionService) Get(ctx context.Context, id string) (session.Session, error) {
	return s.sess, nil
}
func (s *indexingSessionService) GetACPSessionState(ctx context.Context, sessionID string) (string, error) {
	return "", errors.New("not implemented")
}
func (s *indexingSessionService) List(ctx context.Context) ([]session.Session, error) {
	return nil, errors.New("not implemented")
}
func (s *indexingSessionService) SaveACPSessionState(ctx context.Context, sessionID string, state string) error {
	return errors.New("not implemented")
}
func (s *indexingSessionService) Save(ctx context.Context, sess session.Session) (session.Session, error) {
	return session.Session{}, errors.New("not implemented")
}
func (s *indexingSessionService) Delete(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

func (s *indexingSessionService) EndSession(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

func TestCloneSessionMetadataCreatesIndependentCopy(t *testing.T) {
	original := map[string]interface{}{"session_id": "session-1"}
	cloned := cloneSessionMetadata(original)
	cloned["session_id"] = "session-2"

	if original["session_id"] != "session-1" {
		t.Fatalf("original metadata was mutated: %v", original)
	}
}

// newIndexingService returns a remembrances service with an in-memory event
// store and the given document embedder.
func newIndexingService(t *testing.T, embedder embeddings.Embedder) *rag.RemembrancesService {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`
	CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		subject TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata TEXT NOT NULL DEFAULT '{}',
		embedding BLOB,
		event_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL
	);
	CREATE VIRTUAL TABLE events_fts USING fts5(subject, content, content='events', content_rowid='id');
	`); err != nil {
		t.Fatalf("create events schema: %v", err)
	}
	svc := &rag.RemembrancesService{Events: events.NewEventStore(conn, embedder)}
	setDocumentEmbedderForTest(svc, embedder)
	return svc
}

func textMessage(role message.MessageRole, text string) message.Message {
	return message.Message{
		SessionID: "session-1",
		Role:      role,
		Parts:     []message.ContentPart{message.TextContent{Text: text}},
	}
}

func TestIndexSessionConversationPropagatesEmbedErrors(t *testing.T) {
	embedder := &recordingEmbedder{err: errors.New("boom")}
	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{ID: "session-1", Title: "Chunky"}},
		Messages: &indexingMessagesService{msgs: []message.Message{textMessage(message.User, "hello")}},
	}
	svc := newIndexingService(t, embedder)

	err := app.indexSessionConversation(context.Background(), svc, "session-1")
	if err == nil || !strings.Contains(err.Error(), "embed session chunks") {
		t.Fatalf("expected embed error, got %v", err)
	}
}

func TestIndexSessionConversationChunksContentForEmbeddings(t *testing.T) {
	embedder := &recordingEmbedder{}
	content := strings.Repeat("A", embeddings.DefaultChunkSize+200)
	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{
			ID:        "session-1",
			Title:     "Chunky session",
			UpdatedAt: time.Now().Unix(),
		}},
		Messages: &indexingMessagesService{msgs: []message.Message{textMessage(message.User, content)}},
	}
	svc := newIndexingService(t, embedder)

	if err := app.indexSessionConversation(context.Background(), svc, "session-1"); err != nil {
		t.Fatalf("indexSessionConversation() error = %v", err)
	}

	expected := embeddings.ChunkText("Session title: Chunky session\n\nUSER:\n"+content, embeddings.DefaultChunkSize, embeddings.DefaultChunkOverlap)
	if len(expected) < 2 {
		t.Fatalf("expected chunked content, got %d chunks", len(expected))
	}
	if !reflect.DeepEqual(embedder.texts, expected) {
		t.Fatalf("embedded chunks = %#v, want %#v", embedder.texts, expected)
	}
}

// TestIndexSessionConversationReusesEmbeddings checks that a growing session
// only embeds its new chunks and that an unchanged session embeds nothing.
func TestIndexSessionConversationReusesEmbeddings(t *testing.T) {
	embedder := &recordingEmbedder{}
	msgs := &indexingMessagesService{}
	for i := 0; i < 8; i++ {
		msgs.msgs = append(msgs.msgs, textMessage(message.User, fmt.Sprintf("Question %d. %s", i, strings.Repeat("words here. ", 40))))
	}
	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{ID: "session-1", Title: "Long"}},
		Messages: msgs,
	}
	svc := newIndexingService(t, embedder)
	ctx := context.Background()

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("first pass error = %v", err)
	}
	firstPass := len(embedder.texts)
	if firstPass < 4 {
		t.Fatalf("first pass embedded %d chunks, want a multi-chunk session", firstPass)
	}

	embedder.texts = nil
	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("unchanged pass error = %v", err)
	}
	if embedder.texts != nil {
		t.Fatalf("unchanged session embedded %d chunks, want 0", len(embedder.texts))
	}

	msgs.msgs = append(msgs.msgs, textMessage(message.Assistant, "A short answer."))
	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("grown pass error = %v", err)
	}
	if n := len(embedder.texts); n == 0 || n >= firstPass {
		t.Fatalf("grown session embedded %d chunks, want only the changed tail (< %d)", n, firstPass)
	}

	stored, err := svc.Events.SessionChunks(ctx, sessionIndexSubject, "session-1")
	if err != nil {
		t.Fatalf("SessionChunks() error = %v", err)
	}
	for i, c := range stored {
		if len(c.Embedding) == 0 {
			t.Fatalf("stored chunk %d has no embedding", i)
		}
	}
}

func TestExtractMessageSearchPartsSkipsReasoningAndCapsTools(t *testing.T) {
	msg := message.Message{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: "secret chain of thought"},
			message.TextContent{Text: "visible answer"},
			message.ToolResult{Name: "view", Content: strings.Repeat("x", sessionIndexToolResultMaxChars*3)},
		},
	}
	parts := extractMessageSearchParts(msg)
	joined := strings.Join(parts, "\n")
	if strings.Contains(joined, "secret chain of thought") {
		t.Errorf("reasoning was indexed: %q", joined)
	}
	if !strings.Contains(joined, "visible answer") {
		t.Errorf("text missing: %q", joined)
	}
	if len(joined) > sessionIndexToolResultMaxChars+200 {
		t.Errorf("tool result not capped: %d bytes", len(joined))
	}
}

func TestSessionIndexSchedulerCoalescesAndBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	var mu sync.Mutex
	fail := true
	done := make(chan struct{}, 4)
	s := newSessionIndexScheduler(ctx, 20*time.Millisecond, func(context.Context, string) error {
		runs.Add(1)
		mu.Lock()
		defer mu.Unlock()
		defer func() { done <- struct{}{} }()
		if fail {
			fail = false
			return fmt.Errorf("embed: %w", context.DeadlineExceeded)
		}
		return nil
	})
	s.cooldown = 150 * time.Millisecond
	defer s.stop()

	for i := 0; i < 5; i++ {
		s.schedule("session-1")
	}
	start := time.Now()
	<-done
	if got := runs.Load(); got != 1 {
		t.Fatalf("runs after burst = %d, want 1 (coalesced)", got)
	}

	// The backend failure pauses indexing and retries after the cooldown.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session was not retried after the cooldown")
	}
	if elapsed := time.Since(start); elapsed < s.cooldown {
		t.Errorf("retry after %v, want at least the %v cooldown", elapsed, s.cooldown)
	}
	if got := runs.Load(); got != 2 {
		t.Errorf("runs = %d, want 2", got)
	}
}

func setDocumentEmbedderForTest(svc *rag.RemembrancesService, embedder embeddings.Embedder) {
	field := reflect.ValueOf(svc).Elem().FieldByName("docEmbedder")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(embedder))
}
