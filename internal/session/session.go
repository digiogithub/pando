package session

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/pubsub"
)

// evaluatorService is the minimal interface used by session to trigger evaluation.
// A local interface is used to avoid import cycles between session and evaluator.
type evaluatorService interface {
	EvaluateSession(ctx context.Context, sessionID string) error
}

// IPCPublisher is the minimal interface used by session to publish ZMQ events.
// A local interface is used to avoid import cycles between session and ipc packages.
type IPCPublisher interface {
	Publish(topic string, payload any) error
}

// ipcTopicSessionUpdate is the ZMQ topic for session creation and update events.
// Defined locally to avoid importing internal/ipc/protocol (which would create an import cycle).
const ipcTopicSessionUpdate = "session.update"

// ipcTopicSessionDeleted is the ZMQ topic for session deletion events.
const ipcTopicSessionDeleted = "session.deleted"

// ipcSessionPayload is the payload for session IPC events.
// Defined locally to avoid importing internal/ipc/protocol.
type ipcSessionPayload struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int64     `json:"message_count"`
}

var globalIPCPublisher IPCPublisher

// SetIPCPublisher sets the ZMQ IPC publisher for cross-instance event broadcasting.
// Call this from app.go after the Bus is started (only for the primary instance).
func SetIPCPublisher(p IPCPublisher) {
	globalIPCPublisher = p
}

var globalEvaluator evaluatorService

// SetEvaluator sets the evaluator service used to trigger self-evaluation at session end.
func SetEvaluator(e evaluatorService) {
	globalEvaluator = e
}

// globalLuaManager is the package-level Lua filter manager for session lifecycle hooks.
var globalLuaManager *luaengine.FilterManager

// SetLuaManager sets the Lua filter manager used for session lifecycle hooks.
func SetLuaManager(fm *luaengine.FilterManager) {
	globalLuaManager = fm
}

// SnapshotCreator is an interface for creating snapshots without importing the snapshot package directly.
type SnapshotCreator interface {
	CreateSessionSnapshot(ctx context.Context, sessionID, snapshotType, description string) error
}

var globalSnapshotCreator SnapshotCreator

// SetSnapshotCreator sets the snapshot creator used for session lifecycle snapshots.
func SetSnapshotCreator(sc SnapshotCreator) {
	globalSnapshotCreator = sc
}

type Session struct {
	ID               string
	ParentSessionID  string
	Title            string
	MessageCount     int64
	PromptTokens     int64
	CompletionTokens int64
	SummaryMessageID string
	Cost             float64
	CreatedAt        int64
	UpdatedAt        int64
}

type Service interface {
	pubsub.Suscriber[Session]
	Create(ctx context.Context, title string) (Session, error)
	CreateTitleSession(ctx context.Context, parentSessionID string) (Session, error)
	CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	List(ctx context.Context) ([]Session, error)
	Save(ctx context.Context, session Session) (Session, error)
	Delete(ctx context.Context, id string) error
	EndSession(ctx context.Context, id string) error
}

type service struct {
	*pubsub.Broker[Session]
	q db.Querier
}

func (s *service) Create(ctx context.Context, title string) (Session, error) {
	dbSession, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:    uuid.New().String(),
		Title: title,
	})
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	s.Publish(pubsub.CreatedEvent, session)
	s.publishIPC(ipcTopicSessionUpdate, ipcPayloadFromSession(session))
	logging.Debug("Session created", "title", title)

	// Register session cache
	tools.RegisterSessionCache(session.ID)

	// Hook 2: hook_session_start — informational
	if globalLuaManager != nil && globalLuaManager.IsEnabled() {
		hookData := map[string]interface{}{
			"session_id": session.ID,
			"title":      session.Title,
			"created_at": time.Unix(session.CreatedAt, 0).Format(time.RFC3339),
		}
		globalLuaManager.ExecuteHook(ctx, luaengine.HookSessionStart, hookData) //nolint:errcheck
	}

	// Create start snapshot asynchronously
	if globalSnapshotCreator != nil {
		go func() {
			if err := globalSnapshotCreator.CreateSessionSnapshot(
				context.Background(), session.ID, "start", "Session start: "+session.Title,
			); err != nil {
				logging.Error("Failed to create start snapshot", "sessionID", session.ID, "error", err)
			} else {
				logging.Debug("Start snapshot created", "sessionID", session.ID)
			}
		}()
	}

	return session, nil
}

func (s *service) CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (Session, error) {
	dbSession, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:              toolCallID,
		ParentSessionID: sql.NullString{String: parentSessionID, Valid: true},
		Title:           title,
	})
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	s.Publish(pubsub.CreatedEvent, session)
	return session, nil
}

func (s *service) CreateTitleSession(ctx context.Context, parentSessionID string) (Session, error) {
	dbSession, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:              "title-" + parentSessionID,
		ParentSessionID: sql.NullString{String: parentSessionID, Valid: true},
		Title:           "Generate a title",
	})
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	s.Publish(pubsub.CreatedEvent, session)
	return session, nil
}

func (s *service) Delete(ctx context.Context, id string) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	err = s.q.DeleteSession(ctx, session.ID)
	if err != nil {
		return err
	}
	s.Publish(pubsub.DeletedEvent, session)
	s.publishIPC(ipcTopicSessionDeleted, ipcPayloadFromSession(session))
	return nil
}

func (s *service) Get(ctx context.Context, id string) (Session, error) {
	dbSession, err := s.q.GetSessionByID(ctx, id)
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	logging.Debug("Session retrieved", "sessionID", id)

	// Hook 3: hook_session_restore — informational
	if globalLuaManager != nil && globalLuaManager.IsEnabled() {
		hookData := map[string]interface{}{
			"session_id":    session.ID,
			"title":         session.Title,
			"message_count": session.MessageCount,
		}
		globalLuaManager.ExecuteHook(ctx, luaengine.HookSessionRestore, hookData) //nolint:errcheck
	}

	return session, nil
}

func (s *service) Save(ctx context.Context, session Session) (Session, error) {
	dbSession, err := s.q.UpdateSession(ctx, db.UpdateSessionParams{
		ID:               session.ID,
		Title:            session.Title,
		PromptTokens:     session.PromptTokens,
		CompletionTokens: session.CompletionTokens,
		SummaryMessageID: sql.NullString{
			String: session.SummaryMessageID,
			Valid:  session.SummaryMessageID != "",
		},
		Cost: session.Cost,
	})
	if err != nil {
		return Session{}, err
	}
	session = s.fromDBItem(dbSession)
	s.Publish(pubsub.UpdatedEvent, session)
	s.publishIPC(ipcTopicSessionUpdate, ipcPayloadFromSession(session))
	logging.Debug("Session saved", "sessionID", session.ID, "title", session.Title)
	return session, nil
}

func (s *service) List(ctx context.Context) ([]Session, error) {
	dbSessions, err := s.q.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, len(dbSessions))
	for i, dbSession := range dbSessions {
		sessions[i] = s.fromDBItem(dbSession)
	}
	return sessions, nil
}

func (s service) fromDBItem(item db.Session) Session {
	return Session{
		ID:               item.ID,
		ParentSessionID:  item.ParentSessionID.String,
		Title:            item.Title,
		MessageCount:     item.MessageCount,
		PromptTokens:     item.PromptTokens,
		CompletionTokens: item.CompletionTokens,
		SummaryMessageID: item.SummaryMessageID.String,
		Cost:             item.Cost,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}

func (s *service) EndSession(ctx context.Context, id string) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	// Publish a user-facing notification so the desktop app (and other UIs) can
	// surface an OS-level notification when the agent finishes its work.
	notify.Info(notify.SourceAgent, "Session completed: "+session.Title, 30*time.Second)

	// Publish the session update event so subscribers (SSE, desktop) are notified.
	s.Publish(pubsub.UpdatedEvent, session)
	s.publishIPC(ipcTopicSessionUpdate, ipcPayloadFromSession(session))

	// Create end snapshot asynchronously
	if globalSnapshotCreator != nil {
		go func() {
			if err := globalSnapshotCreator.CreateSessionSnapshot(
				context.Background(), session.ID, "end", "Session end: "+session.Title,
			); err != nil {
				logging.Error("Failed to create end snapshot", "sessionID", session.ID, "error", err)
			} else {
				logging.Debug("End snapshot created", "sessionID", session.ID)
			}
		}()
	}

	// Execute Lua hook
	if globalLuaManager != nil && globalLuaManager.IsEnabled() {
		hookData := map[string]interface{}{
			"session_id":    session.ID,
			"title":         session.Title,
			"message_count": session.MessageCount,
		}
		globalLuaManager.ExecuteHook(ctx, luaengine.HookSessionEnd, hookData) //nolint:errcheck
	}

	// Trigger async self-evaluation (non-blocking, after snapshot and Lua hooks).
	// Evaluator errors never fail EndSession.
	if globalEvaluator != nil {
		if err := globalEvaluator.EvaluateSession(ctx, id); err != nil {
			slog.Warn("evaluator: failed to trigger evaluation", "session_id", id, "err", err)
		}
	}

	// Clear session cache
	tools.UnregisterSessionCache(id)

	// Close browser session if one was created for this session
	tools.CloseBrowserSession(id)

	return nil
}

// publishIPC publishes a session event to the ZMQ Bus (best-effort, non-blocking).
// It is a no-op if no IPC publisher has been set.
func (s *service) publishIPC(topic string, payload any) {
	if globalIPCPublisher == nil {
		return
	}
	if err := globalIPCPublisher.Publish(topic, payload); err != nil {
		logging.Warn("ipc: failed to publish session event", "topic", topic, "error", err)
	}
}

// ipcPayloadFromSession constructs an ipcSessionPayload from a Session.
func ipcPayloadFromSession(sess Session) ipcSessionPayload {
	return ipcSessionPayload{
		ID:           sess.ID,
		Title:        sess.Title,
		UpdatedAt:    time.Unix(sess.UpdatedAt, 0),
		MessageCount: sess.MessageCount,
	}
}

func NewService(q db.Querier) Service {
	broker := pubsub.NewBroker[Session]()
	return &service{
		broker,
		q,
	}
}
