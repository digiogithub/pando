package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/session"
)

type recordingEmbedder struct {
	texts     []string
	err       error
	callCount int
}

func (e *recordingEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	e.callCount++
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

// TestIndexSessionConversationRequiresEventStore pins the new (post-#6)
// ordering: since checking for legacy rows and reading per-message markers
// both require svc.Events, indexSessionConversation must fail fast with
// "session event store not configured" before ever computing content or
// calling the embedder — unlike the pre-#6 whole-transcript path, which only
// discovered a missing store after already embedding the whole transcript.
func TestIndexSessionConversationRequiresEventStore(t *testing.T) {
	embedder := &recordingEmbedder{}
	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{ID: "session-1", Title: "Chunky"}},
		Messages: &indexingMessagesService{msgs: []message.Message{{
			ID:        "msg-1",
			SessionID: "session-1",
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: "hello"}},
		}}},
	}
	svc := &rag.RemembrancesService{}
	setDocumentEmbedderForTest(svc, embedder)

	err := app.indexSessionConversation(context.Background(), svc, "session-1")
	if err == nil || !strings.Contains(err.Error(), "session event store not configured") {
		t.Fatalf("expected missing store error, got %v", err)
	}
	if embedder.callCount != 0 {
		t.Fatalf("expected EmbedDocuments never called without an event store, got %d calls", embedder.callCount)
	}
}

// TestIndexSessionConversationPropagatesEmbedErrors uses a real temp event
// store (so the incremental path reaches the per-message embed step) and an
// embedder that always errors, and checks the error is surfaced with the
// per-message wrapping text (not the legacy whole-transcript
// "embed session chunks" text, which is now only used by
// indexSessionConversationFullRebuild).
func TestIndexSessionConversationPropagatesEmbedErrors(t *testing.T) {
	embedder := &recordingEmbedder{err: errors.New("boom")}
	store, _ := openTempEventStore(t, embedder)
	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{ID: "session-1", Title: "Chunky"}},
		Messages: &indexingMessagesService{msgs: []message.Message{{
			ID:        "msg-1",
			SessionID: "session-1",
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: "hello"}},
		}}},
	}
	svc := &rag.RemembrancesService{Events: store}
	setDocumentEmbedderForTest(svc, embedder)

	err := app.indexSessionConversation(context.Background(), svc, "session-1")
	if err == nil || !strings.Contains(err.Error(), "embed message chunks") {
		t.Fatalf("expected embed error, got %v", err)
	}
}

// TestIndexSessionConversationChunksLongMessageContentForEmbeddings pins the
// per-message content format (messageIndexContent: an optional "Session: "
// title prefix, then "ROLE:\n" plus the message's own text) and confirms an
// oversized single message is still split with the same embeddings.ChunkText
// logic used before, just scoped to one message instead of the whole
// transcript.
func TestIndexSessionConversationChunksLongMessageContentForEmbeddings(t *testing.T) {
	embedder := &recordingEmbedder{}
	store, _ := openTempEventStore(t, embedder)
	content := strings.Repeat("A", embeddings.DefaultChunkSize+200)
	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{
			ID:        "session-1",
			Title:     "Chunky session",
			UpdatedAt: time.Now().Unix(),
		}},
		Messages: &indexingMessagesService{msgs: []message.Message{{
			ID:        "msg-1",
			SessionID: "session-1",
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: content}},
		}}},
	}
	svc := &rag.RemembrancesService{Events: store}
	setDocumentEmbedderForTest(svc, embedder)

	if err := app.indexSessionConversation(context.Background(), svc, "session-1"); err != nil {
		t.Fatalf("indexSessionConversation() error = %v", err)
	}

	expected := embeddings.ChunkText("Session: Chunky session\nUSER:\n"+content, embeddings.DefaultChunkSize, embeddings.DefaultChunkOverlap)
	if len(expected) < 2 {
		t.Fatalf("expected chunked content, got %d chunks", len(expected))
	}
	if !reflect.DeepEqual(embedder.texts, expected) {
		t.Fatalf("embedded chunks = %#v, want %#v", embedder.texts, expected)
	}

	// +1 for the session-header row (the session has a non-empty title, so
	// sessionHeaderIndexContent also produces one small, single-chunk plan
	// entry alongside the message's own chunks).
	count, err := store.CountEvents(context.Background())
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if want := len(expected) + 1; int(count) != want {
		t.Fatalf("expected %d indexed rows (message chunks + header row), got %d", want, count)
	}
}

func TestIsEphemeralIndexSession(t *testing.T) {
	cases := map[string]bool{
		"ctxenrich-abc123":     true,
		"title-parent-session": true,
		"session-1":            false,
		"":                     false,
	}
	for id, want := range cases {
		if got := isEphemeralIndexSession(id); got != want {
			t.Errorf("isEphemeralIndexSession(%q) = %v, want %v", id, got, want)
		}
	}
}

// TestIndexSessionConversationSkipsEphemeralSessions pins the indexer's skip
// filter: ctxenrich- (agent-loop enrichment child sessions) and title- (title
// generation sessions) must never be indexed into remembrances, since they
// either duplicate content already indexed elsewhere or are pure scratch. The
// App under test has no Sessions/Messages wired, so anything past the guard
// would panic on a nil-interface method call — proving the guard runs first.
func TestIndexSessionConversationSkipsEphemeralSessions(t *testing.T) {
	app := &App{}
	svc := &rag.RemembrancesService{}
	for _, id := range []string{"ctxenrich-" + "abc123", "title-parent-session-1"} {
		if err := app.indexSessionConversation(context.Background(), svc, id); err != nil {
			t.Fatalf("indexSessionConversation(%q) = %v, want nil (ephemeral sessions must be skipped)", id, err)
		}
	}
}

// TestShouldIndexOnEvent pins the finished-message filter (#4 of
// [[pando/plans/sqlite_contention_fix_roadmap.md]]): a Created event always
// qualifies (new user messages, the empty pre-stream assistant message, and
// the one-shot tool-result message — none of which get a follow-up Update),
// while an Updated event only qualifies once message.Message.IsFinished is
// true, so the mid-stream ThinkingDelta/ContentDelta/ToolCall Updates
// agent.go persists on every provider event never trigger a run.
func TestShouldIndexOnEvent(t *testing.T) {
	unfinishedAssistant := message.Message{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: "partial"}},
	}
	finishedAssistant := message.Message{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "done"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}
	userMsg := message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}}
	toolMsg := message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "t1", Content: "ok"}}}

	cases := []struct {
		name string
		ev   pubsub.Event[message.Message]
		want bool
	}{
		{"created user message qualifies", pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: userMsg}, true},
		{"created empty/pre-stream assistant message qualifies", pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: unfinishedAssistant}, true},
		{"created tool-result message qualifies (no follow-up Update ever arrives)", pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: toolMsg}, true},
		{"mid-stream update without a Finish part is skipped", pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: unfinishedAssistant}, false},
		{"final update with a Finish part qualifies", pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: finishedAssistant}, true},
		{"deleted events never qualify", pubsub.Event[message.Message]{Type: pubsub.DeletedEvent, Payload: finishedAssistant}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldIndexOnEvent(tc.ev); got != tc.want {
				t.Errorf("shouldIndexOnEvent(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func setDocumentEmbedderForTest(svc *rag.RemembrancesService, embedder embeddings.Embedder) {
	field := reflect.ValueOf(svc).Elem().FieldByName("docEmbedder")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(embedder))
}
