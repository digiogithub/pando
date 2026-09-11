package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/embeddings"
)

const sessionIndexSubject = "session"

// ephemeralIndexSessionPrefixes are session ID prefixes that must never be
// indexed into remembrances: their content duplicates what is already indexed
// elsewhere (the enrichment loop's KB/code/events lookups) or is pure scratch
// (a one-line title-generation prompt), so indexing them only adds writes and
// clutters search results with retrieval-trace noise. ctxenrich- sessions used
// to accumulate unindexed-but-unfiltered leftovers before the enrichment loop
// deleted its own sessions — see
// [[pando/fixes/context_enricher_agent_loop_first_event.md]].
var ephemeralIndexSessionPrefixes = []string{ctxEnrichSessionIDPrefix, "title-"}

// isEphemeralIndexSession reports whether sessionID belongs to one of the
// synthetic child-session kinds that ephemeralIndexSessionPrefixes lists.
func isEphemeralIndexSession(sessionID string) bool {
	for _, prefix := range ephemeralIndexSessionPrefixes {
		if strings.HasPrefix(sessionID, prefix) {
			return true
		}
	}
	return false
}

// shouldIndexOnEvent reports whether ev should trigger (or refresh the
// debounce for) a session index run.
//
// Created events always qualify: a new user message, the empty assistant
// message agent.go creates before streaming, and — importantly — the
// tool-result message agent.go creates in one shot with every result already
// attached (streamAndHandleEvents' `a.messages.Create(...Role: message.Tool)`
// call, which has no follow-up Update), so tool results are indexed via this
// branch, not the Updated one below.
//
// Updated events only qualify once the message carries its terminal Finish
// part (message.Message.IsFinished): the agent persists every streamed
// ThinkingDelta/ContentDelta/ToolCall delta with messages.Update with no
// throttle (agent.go's processEvent), so without this filter the debounce
// would still reset on every delta. Every leg of a turn's assistant message
// is guaranteed to end with exactly one Update that does carry a Finish part
// — set via AddFinish on the EventComplete/cancellation/panic paths — so the
// final state of a turn is always still indexed, just not every intermediate
// delta.
func shouldIndexOnEvent(ev pubsub.Event[message.Message]) bool {
	switch ev.Type {
	case pubsub.CreatedEvent:
		return true
	case pubsub.UpdatedEvent:
		return ev.Payload.IsFinished()
	default:
		return false
	}
}

func (app *App) initRemembrancesSessionIndexing(ctx context.Context, svc *rag.RemembrancesService, cfg *config.RemembrancesConfig) {
	if svc == nil || svc.Events == nil || cfg == nil || !cfg.AutoIndexSessions {
		return
	}

	subCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	eventsCh := app.Messages.Subscribe(subCtx)
	scheduler := newSessionIndexScheduler(
		func(ctx context.Context, sessionID string) error {
			return app.indexSessionConversation(ctx, svc, sessionID)
		},
		sessionIndexDebounce,
		sessionIndexMinInterval,
	)

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		for {
			select {
			case <-subCtx.Done():
				scheduler.stopAll()
				return
			case ev, ok := <-eventsCh:
				if !ok {
					return
				}
				if !shouldIndexOnEvent(ev) {
					continue
				}
				sessionID := ev.Payload.SessionID
				if strings.TrimSpace(sessionID) == "" || isEphemeralIndexSession(sessionID) {
					continue
				}
				// subCtx (not context.Background()): a run's own context —
				// and therefore replaceSessionEventsWithRetry's backoff sleep
				// — is canceled promptly on shutdown instead of outliving it.
				scheduler.notify(subCtx, sessionID)
			}
		}
	}()

	logging.Info("remembrances: automatic session indexing enabled")
}

func (app *App) indexSessionConversation(ctx context.Context, svc *rag.RemembrancesService, sessionID string) error {
	// Belt and braces: the watcher above already filters these out before
	// scheduling the debounce timer, but this method is also called directly
	// (tests, potential future manual re-index paths), so it must refuse
	// ephemeral sessions on its own too.
	if isEphemeralIndexSession(sessionID) {
		return nil
	}
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

	chunkEmbeddings, err := svc.DocumentEmbedder().EmbedDocuments(ctx, chunks)
	if err != nil {
		return fmt.Errorf("embed session chunks: %w", err)
	}
	if len(chunkEmbeddings) != len(chunks) {
		return fmt.Errorf("session chunk embedding count mismatch: got %d, expected %d", len(chunkEmbeddings), len(chunks))
	}

	if svc.Events == nil {
		return fmt.Errorf("session event store not configured")
	}
	// chunks/chunkEmbeddings are already computed above and reused by every
	// retry attempt below — a retry must never re-embed.
	err = replaceSessionEventsWithRetry(ctx, sess.ID, func() error {
		return svc.Events.ReplaceSessionEvents(ctx, sess.ID, sessionIndexSubject, metadata, chunks, chunkEmbeddings)
	})
	if err != nil {
		return fmt.Errorf("replace session events: %w", err)
	}
	return nil
}

// sessionIndexReplaceRetries is how many additional attempts
// replaceSessionEventsWithRetry makes after an initial attempt that fails
// with a transient SQLite BUSY/LOCKED error (dbproxy.IsBusyOrLockedError).
// ReplaceSessionEvents always replaces the full set of chunks for a session,
// so retrying it is safe; any other error (including a permanent one) is
// returned immediately without retrying.
const sessionIndexReplaceRetries = 3

// sessionIndexReplaceBaseBackoff is the delay before the first retry; it
// doubles on each subsequent retry, giving 250ms/500ms/1s for the 3 retries
// sessionIndexReplaceRetries allows.
const sessionIndexReplaceBaseBackoff = 250 * time.Millisecond

// replaceSessionEventsWithRetry calls replace up to 1+sessionIndexReplaceRetries
// times, retrying only on a transient busy/locked error. replace must be a
// closure that reuses the same already-computed chunks/embeddings on every
// call (see the sole call site above) — this function itself never triggers
// re-embedding, it just calls replace again. Each retry is logged at Warn;
// the caller (sessionIndexScheduler.run) logs the final failure, if any, at
// Error. Returns promptly — without sleeping past — a canceled ctx, so
// shutdown is not delayed by a stuck retry loop.
func replaceSessionEventsWithRetry(ctx context.Context, sessionID string, replace func() error) error {
	backoff := sessionIndexReplaceBaseBackoff
	var err error
	for attempt := 0; attempt <= sessionIndexReplaceRetries; attempt++ {
		err = replace()
		if err == nil {
			return nil
		}
		if attempt == sessionIndexReplaceRetries || !dbproxy.IsBusyOrLockedError(err) {
			return err
		}
		logging.Warn("remembrances session index: transient lock, retrying",
			"session_id", sessionID, "attempt", attempt+1, "backoff", backoff, "error", err)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return err
}

func cloneSessionMetadata(metadata map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(metadata)+2)
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func extractMessageSearchParts(msg message.Message) []string {
	parts := make([]string, 0)
	if text := strings.TrimSpace(msg.Content().Text); text != "" {
		parts = append(parts, text)
	}
	if thinking := strings.TrimSpace(msg.ReasoningContent().Thinking); thinking != "" {
		parts = append(parts, thinking)
	}
	for _, call := range msg.ToolCalls() {
		entry := strings.TrimSpace(call.Name)
		if strings.TrimSpace(call.Input) != "" {
			entry += "\ninput: " + call.Input
		}
		if entry != "" {
			parts = append(parts, entry)
		}
	}
	for _, result := range msg.ToolResults() {
		entry := strings.TrimSpace(result.Name)
		if strings.TrimSpace(result.Content) != "" {
			entry += "\nresult: " + result.Content
		}
		if strings.TrimSpace(result.Metadata) != "" {
			var decoded interface{}
			if json.Unmarshal([]byte(result.Metadata), &decoded) == nil {
				if pretty, err := json.Marshal(decoded); err == nil {
					entry += "\nmetadata: " + string(pretty)
				}
			}
		}
		if entry != "" {
			parts = append(parts, entry)
		}
	}
	return parts
}
