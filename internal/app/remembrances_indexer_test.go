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

	err := app.indexSessionConversation(context.Background(), svc, "session-1")
	if err == nil || !strings.Contains(err.Error(), "session event store not configured") {
		t.Fatalf("expected missing store error, got %v", err)
	}

	expected := embeddings.ChunkText("Session title: Chunky session\n\nUSER:\n"+content, embeddings.DefaultChunkSize, embeddings.DefaultChunkOverlap)
	if len(expected) < 2 {
		t.Fatalf("expected chunked content, got %d chunks", len(expected))
	}
	if !reflect.DeepEqual(embedder.texts, expected) {
		t.Fatalf("embedded chunks = %#v, want %#v", embedder.texts, expected)
	}
}

func setDocumentEmbedderForTest(svc *rag.RemembrancesService, embedder embeddings.Embedder) {
	field := reflect.ValueOf(svc).Elem().FieldByName("docEmbedder")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(embedder))
}
