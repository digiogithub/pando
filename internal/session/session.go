package session

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/google/uuid"
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

// RecordSnapshot asynchronously records a delta snapshot for a session. It is
// the hook used by the agent loop after every completed turn, so the session
// history contains the incremental changes the agent made instead of only the
// baseline captured at session start. It is a no-op when no snapshot creator is
// registered, and never blocks the caller.
func RecordSnapshot(sessionID, description string) {
	if globalSnapshotCreator == nil || sessionID == "" {
		return
	}
	sc := globalSnapshotCreator
	go func() {
		if err := sc.CreateSessionSnapshot(context.Background(), sessionID, "delta", description); err != nil {
			logging.Error("Failed to record delta snapshot", "sessionID", sessionID, "error", err)
		}
	}()
}

// Default title values that carry no information about the session content.
// They are produced by the UIs on session creation ("New Chat" / "New
// Session") or by the delegation runner ("delegated task") and are replaced
// as soon as a real title (LLM-generated or prompt-derived) is available.
var defaultTitles = map[string]bool{
	"":              true,
	"New Chat":      true,
	"New Session":   true,
	"delegated task": true,
	"Generate a title": true,
}

// IsDefaultTitle reports whether the title is one of the placeholder titles
// assigned at session creation time.
func IsDefaultTitle(title string) bool {
	return defaultTitles[title]
}

// TitleFromPrompt builds a fallback title from the first user prompt: the
// first line, whitespace-collapsed, truncated to 100 runes with an ellipsis.
func TitleFromPrompt(prompt string) string {
	firstLine := prompt
	if idx := strings.IndexAny(firstLine, "\r\n"); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = strings.Join(strings.Fields(firstLine), " ")
	if firstLine == "" {
		return ""
	}
	runes := []rune(firstLine)
	if len(runes) > 100 {
		return string(runes[:99]) + "…"
	}
	return firstLine
}

type Session struct {
	ID                  string
	ParentSessionID     string
	Title               string
	MessageCount        int64
	PromptTokens        int64
	CompletionTokens    int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	SummaryMessageID    string
	Cost                float64
	CreatedAt           int64
	UpdatedAt           int64
}

type Service interface {
	pubsub.Suscriber[Session]
	Create(ctx context.Context, title string) (Session, error)
	CreateTitleSession(ctx context.Context, parentSessionID string) (Session, error)
	CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (Session, error)
	GetACPSessionState(ctx context.Context, sessionID string) (string, error)
	Get(ctx context.Context, id string) (Session, error)
	List(ctx context.Context) ([]Session, error)
	SaveACPSessionState(ctx context.Context, sessionID string, state string) error
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

	// Register the session cache (idempotently) so tool-response pagination
	// works for resumed/loaded sessions, not just newly created ones.
	tools.EnsureSessionCache(session.ID)

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

func (s *service) GetACPSessionState(ctx context.Context, sessionID string) (string, error) {
	state, err := s.q.GetSessionACPState(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if !state.Valid {
		return "", nil
	}
	return state.String, nil
}

func (s *service) Save(ctx context.Context, session Session) (Session, error) {
	dbSession, err := s.q.UpdateSession(ctx, db.UpdateSessionParams{
		ID:                  session.ID,
		Title:               session.Title,
		PromptTokens:        session.PromptTokens,
		CompletionTokens:    session.CompletionTokens,
		CacheReadTokens:     session.CacheReadTokens,
		CacheCreationTokens: session.CacheCreationTokens,
		ReasoningTokens:     session.ReasoningTokens,
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

func (s *service) SaveACPSessionState(ctx context.Context, sessionID string, state string) error {
	state = strings.TrimSpace(state)
	return s.q.UpdateSessionACPState(ctx, db.UpdateSessionACPStateParams{
		ID: sessionID,
		ACPState: sql.NullString{
			String: state,
			Valid:  state != "",
		},
	})
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
		ID:                  item.ID,
		ParentSessionID:     item.ParentSessionID.String,
		Title:               item.Title,
		MessageCount:        item.MessageCount,
		PromptTokens:        item.PromptTokens,
		CompletionTokens:    item.CompletionTokens,
		CacheReadTokens:     item.CacheReadTokens,
		CacheCreationTokens: item.CacheCreationTokens,
		ReasoningTokens:     item.ReasoningTokens,
		SummaryMessageID:    item.SummaryMessageID.String,
		Cost:                item.Cost,
		CreatedAt:           item.CreatedAt,
		UpdatedAt:           item.UpdatedAt,
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
		Broker: broker,
		q:      q,
	}
}
