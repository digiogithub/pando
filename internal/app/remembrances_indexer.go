package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/embeddings"
)

const sessionIndexSubject = "session"

func (app *App) initRemembrancesSessionIndexing(ctx context.Context, svc *rag.RemembrancesService, cfg *config.RemembrancesConfig) {
	if svc == nil || svc.Events == nil || cfg == nil || !cfg.AutoIndexSessions {
		return
	}

	subCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	eventsCh := app.Messages.Subscribe(subCtx)
	var (
		mu     sync.Mutex
		timers = make(map[string]*time.Timer)
		delay  = 1200 * time.Millisecond
	)

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		for {
			select {
			case <-subCtx.Done():
				mu.Lock()
				for _, timer := range timers {
					timer.Stop()
				}
				mu.Unlock()
				return
			case ev, ok := <-eventsCh:
				if !ok {
					return
				}
				if ev.Type != pubsub.CreatedEvent && ev.Type != pubsub.UpdatedEvent {
					continue
				}
				sessionID := ev.Payload.SessionID
				if strings.TrimSpace(sessionID) == "" {
					continue
				}
				mu.Lock()
				if existing := timers[sessionID]; existing != nil {
					existing.Stop()
				}
				timers[sessionID] = time.AfterFunc(delay, func() {
					if err := app.indexSessionConversation(context.Background(), svc, sessionID); err != nil {
						logging.Error("remembrances session index failed", "session_id", sessionID, "error", err)
					}
					mu.Lock()
					delete(timers, sessionID)
					mu.Unlock()
				})
				mu.Unlock()
			}
		}
	}()

	logging.Info("remembrances: automatic session indexing enabled")
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
	if err := svc.Events.ReplaceSessionEvents(ctx, sess.ID, sessionIndexSubject, metadata, chunks, chunkEmbeddings); err != nil {
		return fmt.Errorf("replace session events: %w", err)
	}
	return nil
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
