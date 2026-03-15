package sessions

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

const (
	maxMessageTextLen = 3000
	minConversationLen = 50
)

// SessionIndexer extracts text from agent conversations and indexes them via SessionRAGStore.
type SessionIndexer struct {
	store    *SessionRAGStore
	messages message.Service
	sessions session.Service
}

// NewSessionIndexer creates a new SessionIndexer.
func NewSessionIndexer(store *SessionRAGStore, messages message.Service, sessions session.Service) *SessionIndexer {
	return &SessionIndexer{
		store:    store,
		messages: messages,
		sessions: sessions,
	}
}

// IndexSession retrieves messages for the given session, extracts conversation text,
// and stores it in the RAG store.
func (idx *SessionIndexer) IndexSession(ctx context.Context, sessionID string) error {
	sess, err := idx.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session indexer: get session %s: %w", sessionID, err)
	}

	msgs, err := idx.messages.List(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session indexer: list messages %s: %w", sessionID, err)
	}

	// Count messages with indexable text (user or assistant roles)
	indexable := 0
	for _, m := range msgs {
		if m.Role != message.User && m.Role != message.Assistant {
			continue
		}
		for _, part := range m.Parts {
			switch p := part.(type) {
			case message.TextContent:
				if strings.TrimSpace(p.Text) != "" {
					indexable++
				}
			case message.ReasoningContent:
				if strings.TrimSpace(p.Thinking) != "" {
					indexable++
				}
			}
		}
	}

	if indexable < 2 {
		return nil
	}

	conversationText := ExtractConversationText(msgs)
	if len(strings.TrimSpace(conversationText)) < minConversationLen {
		return nil
	}

	// Find last known model
	var lastModel string
	for i := len(msgs) - 1; i >= 0; i-- {
		if string(msgs[i].Model) != "" {
			lastModel = string(msgs[i].Model)
			break
		}
	}

	metadata := map[string]interface{}{
		"session_id": sessionID,
		"model":      lastModel,
	}

	turnCount := CountTurns(msgs)

	if err := idx.store.IndexSession(
		ctx,
		sessionID,
		sess.Title,
		conversationText,
		metadata,
		len(msgs),
		turnCount,
		lastModel,
	); err != nil {
		return fmt.Errorf("session indexer: index session %s: %w", sessionID, err)
	}

	logging.Info("Session indexed for RAG",
		"session_id", sessionID,
		"title", sess.Title,
		"messages", len(msgs),
		"turns", turnCount,
		"text_len", len(conversationText),
	)
	return nil
}

// ExtractConversationText builds a human-readable transcript from messages.
// Only user and assistant roles are included; tool messages are ignored.
// Each individual message text is truncated to maxMessageTextLen characters.
func ExtractConversationText(msgs []message.Message) string {
	var sb strings.Builder

	for _, m := range msgs {
		if m.Role != message.User && m.Role != message.Assistant {
			continue
		}

		var parts []string
		for _, part := range m.Parts {
			switch p := part.(type) {
			case message.TextContent:
				if t := strings.TrimSpace(p.Text); t != "" {
					parts = append(parts, t)
				}
			case message.ReasoningContent:
				if t := strings.TrimSpace(p.Thinking); t != "" {
					parts = append(parts, t)
				}
			}
		}

		if len(parts) == 0 {
			continue
		}

		text := strings.Join(parts, "\n")
		if len(text) > maxMessageTextLen {
			text = text[:maxMessageTextLen] + "...[truncated]"
		}

		role := string(m.Role)
		sb.WriteString("[")
		sb.WriteString(role)
		sb.WriteString("]: ")
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// CountTurns counts user→assistant consecutive pairs in the message list.
func CountTurns(msgs []message.Message) int {
	turns := 0
	i := 0
	for i < len(msgs) {
		if msgs[i].Role == message.User {
			// Look for the next assistant message
			j := i + 1
			for j < len(msgs) && msgs[j].Role != message.Assistant {
				j++
			}
			if j < len(msgs) && msgs[j].Role == message.Assistant {
				turns++
				i = j + 1
				continue
			}
		}
		i++
	}
	return turns
}
