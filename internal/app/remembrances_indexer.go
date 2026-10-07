package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/rag/events"
)

const sessionIndexSubject = "session"

const (
	// sessionIndexIdleDelay is how long a session must stay quiet before it is
	// indexed. Message updates arrive continuously while an agent works (every
	// stream delta, tool call and tool result), so a short delay re-indexed the
	// whole conversation after almost every step.
	sessionIndexIdleDelay = 30 * time.Second
	// sessionIndexTimeout bounds one indexing run.
	sessionIndexTimeout = 5 * time.Minute
	// sessionIndexBackendCooldown pauses session indexing after the embedding
	// backend timed out or refused the request, instead of queuing more work
	// on it.
	sessionIndexBackendCooldown = 2 * time.Minute
	// Tool payloads are capped: they are the bulk of a conversation (file
	// views, search results) and add little to recalling what a session was
	// about.
	sessionIndexToolInputMaxChars  = 500
	sessionIndexToolResultMaxChars = 2000
)

func (app *App) initRemembrancesSessionIndexing(ctx context.Context, svc *rag.RemembrancesService, cfg *config.RemembrancesConfig, skip bool) {
	if svc == nil || svc.Events == nil || cfg == nil || !cfg.AutoIndexSessions {
		return
	}
	if skip {
		logging.Info("remembrances: automatic session indexing skipped (one-shot run)")
		return
	}

	subCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	scheduler := newSessionIndexScheduler(subCtx, sessionIndexIdleDelay, func(runCtx context.Context, sessionID string) error {
		return app.indexSessionConversation(runCtx, svc, sessionID)
	})
	eventsCh := app.Messages.Subscribe(subCtx)

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		for {
			select {
			case <-subCtx.Done():
				scheduler.stop()
				return
			case ev, ok := <-eventsCh:
				if !ok {
					scheduler.stop()
					return
				}
				if ev.Type != pubsub.CreatedEvent && ev.Type != pubsub.UpdatedEvent {
					continue
				}
				sessionID := ev.Payload.SessionID
				if strings.TrimSpace(sessionID) == "" {
					continue
				}
				scheduler.schedule(sessionID)
			}
		}
	}()

	logging.Info("remembrances: automatic session indexing enabled")
}

// sessionIndexScheduler debounces indexing per session, never runs two
// indexing passes of one session at once, and backs off globally while the
// embedding backend is unavailable.
type sessionIndexScheduler struct {
	ctx      context.Context
	delay    time.Duration
	cooldown time.Duration
	timeout  time.Duration
	run      func(ctx context.Context, sessionID string) error

	mu            sync.Mutex
	timers        map[string]*time.Timer
	running       map[string]bool
	dirty         map[string]bool
	cooldownUntil time.Time
	stopped       bool
}

func newSessionIndexScheduler(ctx context.Context, delay time.Duration, run func(ctx context.Context, sessionID string) error) *sessionIndexScheduler {
	return &sessionIndexScheduler{
		ctx:      ctx,
		delay:    delay,
		cooldown: sessionIndexBackendCooldown,
		timeout:  sessionIndexTimeout,
		run:      run,
		timers:   make(map[string]*time.Timer),
		running:  make(map[string]bool),
		dirty:    make(map[string]bool),
	}
}

// schedule (re)starts the idle timer of a session. While a pass for the
// session is running it only marks the session dirty; the pass reschedules it
// when it ends.
func (s *sessionIndexScheduler) schedule(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scheduleLocked(sessionID)
}

func (s *sessionIndexScheduler) scheduleLocked(sessionID string) {
	if s.stopped {
		return
	}
	if s.running[sessionID] {
		s.dirty[sessionID] = true
		return
	}
	if existing := s.timers[sessionID]; existing != nil {
		existing.Stop()
	}
	delay := s.delay
	if wait := time.Until(s.cooldownUntil); wait > delay {
		delay = wait
	}
	s.timers[sessionID] = time.AfterFunc(delay, func() { s.fire(sessionID) })
}

func (s *sessionIndexScheduler) fire(sessionID string) {
	s.mu.Lock()
	delete(s.timers, sessionID)
	if s.stopped || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	if time.Now().Before(s.cooldownUntil) {
		s.scheduleLocked(sessionID)
		s.mu.Unlock()
		return
	}
	s.running[sessionID] = true
	s.mu.Unlock()

	runCtx, cancel := context.WithTimeout(s.ctx, s.timeout)
	err := s.run(runCtx, sessionID)
	cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, sessionID)
	again := s.dirty[sessionID]
	delete(s.dirty, sessionID)
	if err != nil && s.ctx.Err() == nil {
		if embeddings.IsBackendUnavailable(err) {
			s.cooldownUntil = time.Now().Add(s.cooldown)
			again = true
			logging.Warn("remembrances session index paused: embedding backend unavailable",
				"session_id", sessionID, "retry_in", s.cooldown.String(), "error", err)
		} else {
			logging.Error("remembrances session index failed", "session_id", sessionID, "error", err)
		}
	}
	if again {
		s.scheduleLocked(sessionID)
	}
}

func (s *sessionIndexScheduler) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
}

func (app *App) indexSessionConversation(ctx context.Context, svc *rag.RemembrancesService, sessionID string) error {
	sess, err := app.Sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	msgs, err := app.Messages.List(ctx, sessionID)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	var b strings.Builder
	if strings.TrimSpace(sess.Title) != "" {
		b.WriteString("Session title: ")
		b.WriteString(sess.Title)
		b.WriteString("\n\n")
	}
	for _, msg := range msgs {
		b.WriteString(strings.ToUpper(string(msg.Role)))
		b.WriteString(":\n")
		for _, text := range extractMessageSearchParts(msg) {
			if strings.TrimSpace(text) == "" {
				continue
			}
			b.WriteString(text)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	content := strings.TrimSpace(b.String())
	if content == "" {
		return nil
	}

	metadata := map[string]interface{}{
		"session_id":    sess.ID,
		"title":         sess.Title,
		"message_count": len(msgs),
		"source":        "pando_session",
		"updated_at":    sess.UpdatedAt,
	}
	// Attribution, when an extension knows who the user is. The key is absent
	// in a standard build, so an index written without a provider is exactly
	// what it was before. Only the user id is recorded: the address and the
	// group list stay with the extension that holds them.
	if app.Identity != nil {
		if id, ok := app.Identity(ctx); ok && strings.TrimSpace(id.UserID) != "" {
			metadata["user_id"] = id.UserID
		}
	}

	chunks := embeddings.ChunkText(content, embeddings.DefaultChunkSize, embeddings.DefaultChunkOverlap)
	if len(chunks) == 0 {
		return nil
	}

	if svc.Events == nil {
		return fmt.Errorf("session event store not configured")
	}

	// Chunking is stable on a growing conversation (only the tail changes), so
	// the previous pass's vectors are reused and only new chunks are embedded.
	stored, err := svc.Events.SessionChunks(ctx, sessionIndexSubject, sess.ID)
	if err != nil {
		logging.Debug("remembrances session index: read stored chunks failed", "session_id", sess.ID, "error", err)
		stored = nil
	}
	if sameSessionChunks(stored, chunks) {
		return nil
	}

	chunkEmbeddings, embedded, err := embedSessionChunks(ctx, svc.DocumentEmbedder(), chunks, stored)
	if err != nil {
		return fmt.Errorf("embed session chunks: %w", err)
	}
	logging.Debug("remembrances session index: chunks embedded",
		"session_id", sess.ID, "chunks", len(chunks), "embedded", embedded, "reused", len(chunks)-embedded)

	if err := svc.Events.ReplaceSessionEvents(ctx, sess.ID, sessionIndexSubject, metadata, chunks, chunkEmbeddings); err != nil {
		return fmt.Errorf("replace session events: %w", err)
	}
	return nil
}

// sameSessionChunks reports whether the stored chunks already hold exactly
// these chunks with embeddings, in which case nothing has to be written.
func sameSessionChunks(stored []events.StoredChunk, chunks []string) bool {
	if len(stored) != len(chunks) {
		return false
	}
	for i := range chunks {
		if stored[i].Content != chunks[i] || len(stored[i].Embedding) == 0 {
			return false
		}
	}
	return true
}

// embedSessionChunks returns one vector per chunk, reusing stored vectors for
// chunks whose text is unchanged and embedding only the rest. It reports how
// many chunks were sent to the embedder. Stored vectors whose size does not
// match the current model are embedded again.
func embedSessionChunks(ctx context.Context, embedder embeddings.Embedder, chunks []string, stored []events.StoredChunk) ([][]float32, int, error) {
	if embedder == nil {
		return nil, 0, fmt.Errorf("no document embedder configured")
	}
	cache := make(map[string][]float32, len(stored))
	for _, c := range stored {
		if len(c.Embedding) > 0 {
			cache[c.Content] = c.Embedding
		}
	}

	out := make([][]float32, len(chunks))
	embedded := 0
	embedMissing := func(dim int) error {
		var idx []int
		var texts []string
		for i, chunk := range chunks {
			if out[i] != nil && (dim == 0 || len(out[i]) == dim) {
				continue
			}
			idx = append(idx, i)
			texts = append(texts, chunk)
		}
		if len(texts) == 0 {
			return nil
		}
		vecs, err := embedder.EmbedDocuments(ctx, texts)
		if err != nil {
			return err
		}
		if len(vecs) != len(texts) {
			return fmt.Errorf("session chunk embedding count mismatch: got %d, expected %d", len(vecs), len(texts))
		}
		for j, i := range idx {
			out[i] = vecs[j]
		}
		embedded += len(texts)
		return nil
	}

	for i, chunk := range chunks {
		out[i] = cache[chunk]
	}
	dim := embedder.Dimension()
	if err := embedMissing(dim); err != nil {
		return nil, embedded, err
	}
	// The model's size was unknown before this call: now that fresh vectors
	// exist, re-embed reused ones of another size (the model changed).
	if dim == 0 {
		if dim = embedder.Dimension(); dim > 0 {
			if err := embedMissing(dim); err != nil {
				return nil, embedded, err
			}
		}
	}
	return out, embedded, nil
}

// truncateForIndex caps s at max bytes on a UTF-8 boundary.
func truncateForIndex(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + " …[truncated]"
}

func cloneSessionMetadata(metadata map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(metadata)+2)
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

// extractMessageSearchParts returns the text of a message worth indexing for
// session search: the visible text, tool names with capped inputs, and tool
// results with capped content. Reasoning is left out: it is long, repeats the
// answer, and was the largest share of what every pass re-embedded.
func extractMessageSearchParts(msg message.Message) []string {
	parts := make([]string, 0)
	if text := strings.TrimSpace(msg.Content().Text); text != "" {
		parts = append(parts, text)
	}
	for _, call := range msg.ToolCalls() {
		entry := strings.TrimSpace(call.Name)
		if input := strings.TrimSpace(call.Input); input != "" {
			entry += "\ninput: " + truncateForIndex(input, sessionIndexToolInputMaxChars)
		}
		if entry != "" {
			parts = append(parts, entry)
		}
	}
	for _, result := range msg.ToolResults() {
		entry := strings.TrimSpace(result.Name)
		if content := strings.TrimSpace(result.Content); content != "" {
			entry += "\nresult: " + truncateForIndex(content, sessionIndexToolResultMaxChars)
		}
		if entry != "" {
			parts = append(parts, entry)
		}
	}
	return parts
}
