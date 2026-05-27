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

type recordingEventStore struct {
	events    []recordedEvent
	saveErr   error
	callCount int
}

type recordedEvent struct {
	subject   string
	content   string
	metadata  map[string]interface{}
	embedding []float32
}

func (s *recordingEventStore) SaveEventWithEmbedding(ctx context.Context, subject, content string, metadata map[string]interface{}, embedding []float32) (int64, error) {
	s.callCount++
	if s.saveErr != nil {
		return 0, s.saveErr
	}
	metadataCopy := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		metadataCopy[key] = value
	}
	embCopy := append([]float32(nil), embedding...)
	s.events = append(s.events, recordedEvent{
		subject:   subject,
		content:   content,
		metadata:  metadataCopy,
		embedding: embCopy,
	})
	return int64(s.callCount), nil
}

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
func (s *indexingSessionService) List(ctx context.Context) ([]session.Session, error) {
	return nil, errors.New("not implemented")
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

func TestSaveSessionEventChunk(t *testing.T) {
	store := &recordingEventStore{}
	metadata := map[string]interface{}{"session_id": "session-1"}
	embedding := []float32{1, 2}

	id, err := saveSessionEventChunk(context.Background(), store, "session", "chunk", metadata, embedding)
	if err != nil {
		t.Fatalf("saveSessionEventChunk() error = %v", err)
	}
	if id != 1 {
		t.Fatalf("id = %d, want 1", id)
	}
	if len(store.events) != 1 {
		t.Fatalf("saved events = %d, want 1", len(store.events))
	}
	if store.events[0].content != "chunk" {
		t.Fatalf("content = %q, want chunk", store.events[0].content)
	}
}

func TestIndexSessionConversationPropagatesEmbedErrors(t *testing.T) {
	embedder := &recordingEmbedder{err: errors.New("boom")}
	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{ID: "session-1", Title: "Chunky"}},
		Messages: &indexingMessagesService{msgs: []message.Message{{
			SessionID: "session-1",
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: "hello"}},
		}}},
	}
	svc := &rag.RemembrancesService{}
	setDocumentEmbedderForTest(svc, embedder)

	err := app.indexSessionConversation(context.Background(), svc, "session-1")
	if err == nil || !strings.Contains(err.Error(), "embed session chunks") {
		t.Fatalf("expected embed error, got %v", err)
	}
}

func TestCloneSessionMetadataCreatesIndependentCopy(t *testing.T) {
	original := map[string]interface{}{"session_id": "session-1"}
	cloned := cloneSessionMetadata(original)
	cloned["session_id"] = "session-2"

	if original["session_id"] != "session-1" {
		t.Fatalf("original metadata was mutated: %v", original)
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
		Messages: &indexingMessagesService{msgs: []message.Message{{
			SessionID: "session-1",
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: content}},
		}}},
	}
	svc := &rag.RemembrancesService{}
	setDocumentEmbedderForTest(svc, embedder)

	expected := embeddings.ChunkText("Session title: Chunky session\n\nUSER:\n"+content, embeddings.DefaultChunkSize, embeddings.DefaultChunkOverlap)
	if len(expected) < 2 {
		t.Fatalf("expected chunked content, got %d chunks", len(expected))
	}

	originalSave := saveSessionEventChunkFn
	saveSessionEventChunkFn = func(ctx context.Context, store sessionEventChunkSaver, subject, content string, metadata map[string]interface{}, embedding []float32) (int64, error) {
		return 0, errors.New("save failed")
	}
	defer func() { saveSessionEventChunkFn = originalSave }()

	err := app.indexSessionConversation(context.Background(), svc, "session-1")
	if err == nil || !strings.Contains(err.Error(), "save session event chunk") {
		t.Fatalf("expected save error, got %v", err)
	}
	if !reflect.DeepEqual(embedder.texts, expected) {
		t.Fatalf("embedded chunks = %#v, want %#v", embedder.texts, expected)
	}
}

func setDocumentEmbedderForTest(svc *rag.RemembrancesService, embedder embeddings.Embedder) {
	field := reflect.ValueOf(svc).Elem().FieldByName("docEmbedder")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(embedder))
}
