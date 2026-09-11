// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/message"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/session"
)

// sessionHeaderMessageID is the synthetic "message_id" used for the one
// per-session row that carries the session title, so it stays searchable by
// title text even though per-message chunking otherwise indexes exactly one
// row-set per real message. It can never collide with a real message ID
// (a uuid.New().String() value, see internal/message/message.go) because it
// is not a valid UUID.
const sessionHeaderMessageID = "__session_header__"

// contentHashHex returns the hex-encoded sha256 of s. It is the staleness
// marker stored as metadata["content_hash"] on every indexed row for a
// message (or the header). The incremental indexer treats a message as
// unchanged, and skips re-embedding and re-writing it entirely, exactly when
// this hash still matches the one already stored for that message_id.
func contentHashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// messageIndexContent renders msg into the text that gets embedded/indexed
// (content) and the smaller text the staleness hash is computed over
// (hashSource). ok is false when the message has nothing worth indexing
// (extractMessageSearchParts found no text/thinking/tool-call/tool-result
// content at all — e.g. a purely structural message).
//
// hashSource deliberately excludes the session title: including it would
// make every message in a session "change" (and therefore need
// re-embedding) whenever the title changes, defeating the point of
// per-message incremental indexing for what is normally a one-time,
// early-in-the-session event. content does include the title, as a short
// prefix — it gives otherwise-ambiguous short messages like "ok" or "yes"
// useful surrounding context for both semantic and full-text search. The
// trade-off is that one message's indexed text can show a stale title until
// that specific message next changes and is re-embedded on its own; the
// session's current title is always fully up to date on its own header row
// (see sessionHeaderMessageID and sessionHeaderIndexContent) regardless.
func messageIndexContent(sessTitle string, msg message.Message) (content, hashSource string, ok bool) {
	parts := extractMessageSearchParts(msg)
	if len(parts) == 0 {
		return "", "", false
	}

	var body strings.Builder
	body.WriteString(strings.ToUpper(string(msg.Role)))
	body.WriteString(":\n")
	for _, p := range parts {
		body.WriteString(p)
		body.WriteString("\n")
	}
	hashSource = strings.TrimSpace(body.String())

	var full strings.Builder
	if title := strings.TrimSpace(sessTitle); title != "" {
		full.WriteString("Session: ")
		full.WriteString(title)
		full.WriteString("\n")
	}
	full.WriteString(hashSource)
	content = strings.TrimSpace(full.String())
	return content, hashSource, true
}

// sessionHeaderIndexContent renders the session-header row: just the title,
// so a session remains findable by its title text alone (e.g. "what was
// that session about X called") even when no individual message happens to
// restate it. ok is false when the session has no title yet.
func sessionHeaderIndexContent(sessTitle string) (content, hashSource string, ok bool) {
	title := strings.TrimSpace(sessTitle)
	if title == "" {
		return "", "", false
	}
	content = "Session title: " + title
	return content, title, true
}

// messageIndexPlan is one message's (or the header's) desired indexed state
// for a single indexSessionConversation run.
type messageIndexPlan struct {
	messageID string
	content   string
	hash      string
}

// sessionIndexBaseMetadata builds the metadata fields shared by every
// per-message (and header) row written for one session: session_id, title,
// message_count, source, and attribution when available. Per-row fields
// (message_id, content_hash, chunk_index, chunk_count) are added on top by
// the caller and by EventStore.ReplaceMessageEvents/ReplaceSessionEvents.
func sessionIndexBaseMetadata(ctx context.Context, app *App, sess session.Session, messageCount int) map[string]interface{} {
	metadata := map[string]interface{}{
		"session_id":    sess.ID,
		"title":         sess.Title,
		"message_count": messageCount,
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
	return metadata
}

// indexSessionConversation is the session indexer's entry point (called by
// the sessionIndexScheduler — see session_index_scheduler.go). It indexes
// only the messages that are new or whose content changed since the last
// run (detected via a per-message content hash — see messageIndexContent),
// plus a small session-title header row, instead of rebuilding and
// re-embedding the entire transcript on every run. See
// [[pando/analysis/session_index_locked_residual_risk.md]] section 5 (#6)
// and [[pando/features/session_index_incremental_per_message.md]] for the
// full design.
//
// Two situations fall back to the legacy, full-transcript rebuild
// (indexSessionConversationFullRebuild in remembrances_indexer.go), both
// expected to be rare and self-limiting:
//   - Lazy migration: the session still has legacy rows from before this
//     feature existed (metadata with no "message_id" key). Those are
//     cleared (a delete-only ReplaceSessionEvents call, no embedding) before
//     this same run continues into the incremental path below with an empty
//     marker set — so every message in that session is treated as new
//     exactly once, on its first post-upgrade run.
//   - Version skew: the primary does not support the new per-message write
//     methods yet (a secondary running this code talking to an older
//     primary binary during a rolling upgrade). Detected via
//     dbproxy.IsMethodNotSupportedError on the first ReplaceMessageEvents or
//     DeleteMessageEvents call in the run; the whole run then switches to
//     the legacy path so the index is never left straddling both row
//     layouts for one session.
func (app *App) indexSessionConversation(ctx context.Context, svc *rag.RemembrancesService, sessionID string) error {
	// Belt and braces: the watcher already filters these out before
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
	if svc.Events == nil {
		return fmt.Errorf("session event store not configured")
	}

	hasLegacy, err := svc.Events.SessionHasLegacyRows(ctx, sessionID, sessionIndexSubject)
	if err != nil {
		return fmt.Errorf("check legacy session rows: %w", err)
	}
	if hasLegacy {
		// Delete-only: no chunks/embeddings, so no embedding cost. Clears
		// every existing row (legacy or otherwise) for this session before
		// any per-message row is written below, so the two layouts never
		// coexist for one session.
		if err := svc.Events.ReplaceSessionEvents(ctx, sessionID, sessionIndexSubject, nil, nil, nil); err != nil {
			return fmt.Errorf("clear legacy session rows: %w", err)
		}
	}

	existing, err := svc.Events.MessageEventMarkers(ctx, sessionID, sessionIndexSubject)
	if err != nil {
		return fmt.Errorf("read message markers: %w", err)
	}

	plans := make([]messageIndexPlan, 0, len(msgs)+1)
	if content, hashSource, ok := sessionHeaderIndexContent(sess.Title); ok {
		plans = append(plans, messageIndexPlan{
			messageID: sessionHeaderMessageID,
			content:   content,
			hash:      contentHashHex(hashSource),
		})
	}
	for _, msg := range msgs {
		// Defensive only: every message created through message.Service.Create
		// gets a uuid.New().String() ID (internal/message/message.go), so this
		// never happens in practice — but ReplaceMessageEvents/
		// DeleteMessageEvents both reject an empty message_id, and skipping
		// it here (rather than failing the whole run) is the safer response
		// to unexpected data.
		if strings.TrimSpace(msg.ID) == "" {
			continue
		}
		content, hashSource, ok := messageIndexContent(sess.Title, msg)
		if !ok {
			continue
		}
		plans = append(plans, messageIndexPlan{
			messageID: msg.ID,
			content:   content,
			hash:      contentHashHex(hashSource),
		})
	}

	wanted := make(map[string]struct{}, len(plans))
	for _, p := range plans {
		wanted[p.messageID] = struct{}{}
	}

	baseMetadata := sessionIndexBaseMetadata(ctx, app, sess, len(msgs))

	for _, p := range plans {
		if existing[p.messageID] == p.hash {
			continue // unchanged: skip re-embedding and re-writing entirely.
		}
		if err := app.replaceMessageIndex(ctx, svc, sess.ID, p, baseMetadata); err != nil {
			if dbproxy.IsMethodNotSupportedError(err) {
				return app.indexSessionConversationFullRebuild(ctx, svc, sess, msgs)
			}
			return fmt.Errorf("replace message events: %w", err)
		}
	}

	for messageID := range existing {
		if _, ok := wanted[messageID]; ok {
			continue
		}
		messageID := messageID
		err := replaceSessionEventsWithRetry(ctx, sess.ID, func() error {
			return svc.Events.DeleteMessageEvents(ctx, messageID, sessionIndexSubject)
		})
		if err != nil {
			if dbproxy.IsMethodNotSupportedError(err) {
				return app.indexSessionConversationFullRebuild(ctx, svc, sess, msgs)
			}
			return fmt.Errorf("delete stale message events: %w", err)
		}
	}

	return nil
}

// replaceMessageIndex computes chunks/embeddings for one plan entry and
// writes them with EventStore.ReplaceMessageEvents, retrying transient
// busy/locked errors via replaceSessionEventsWithRetry (which reuses the
// already-computed embeddings on every retry — the embedder is never called
// twice for the same write attempt).
func (app *App) replaceMessageIndex(ctx context.Context, svc *rag.RemembrancesService, sessionID string, p messageIndexPlan, baseMetadata map[string]interface{}) error {
	chunks := embeddings.ChunkText(p.content, embeddings.DefaultChunkSize, embeddings.DefaultChunkOverlap)
	if len(chunks) == 0 {
		return nil
	}

	chunkEmbeddings, err := svc.DocumentEmbedder().EmbedDocuments(ctx, chunks)
	if err != nil {
		return fmt.Errorf("embed message chunks: %w", err)
	}
	if len(chunkEmbeddings) != len(chunks) {
		return fmt.Errorf("message chunk embedding count mismatch: got %d, expected %d", len(chunkEmbeddings), len(chunks))
	}

	metadata := cloneSessionMetadata(baseMetadata)
	metadata["message_id"] = p.messageID
	metadata["content_hash"] = p.hash

	return replaceSessionEventsWithRetry(ctx, sessionID, func() error {
		return svc.Events.ReplaceMessageEvents(ctx, sessionID, p.messageID, sessionIndexSubject, metadata, chunks, chunkEmbeddings)
	})
}
