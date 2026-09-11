package events

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/rag/embeddings"
)

// EventStore manages temporal events with semantic search capabilities.
type EventStore struct {
	db       *sql.DB
	embedder embeddings.Embedder
	proxy    *dbproxy.DBProxy
}

// NewEventStore creates a new EventStore backed by db.
// The embedder is used to generate embeddings for event content.
func NewEventStore(db *sql.DB, embedder embeddings.Embedder) *EventStore {
	return &EventStore{
		db:       db,
		embedder: embedder,
	}
}

// SetWriteProxy configures a DB proxy for mutating operations.
func (s *EventStore) SetWriteProxy(proxy *dbproxy.DBProxy) {
	s.proxy = proxy
}

// SaveEvent stores a new event with its embedding and updates the FTS index.
// The embedding is generated from the content using the configured embedder.
// Returns the auto-assigned event ID.
func (s *EventStore) SaveEvent(ctx context.Context, subject, content string, metadata map[string]interface{}) (int64, error) {
	// Generate embedding for the content.
	embedding, err := s.embedder.EmbedQuery(ctx, content)
	if err != nil {
		return 0, fmt.Errorf("events: embed content: %w", err)
	}

	// Marshal metadata to JSON.
	var metaJSON []byte
	if metadata == nil {
		metaJSON = []byte("{}")
	} else {
		metaJSON, err = json.Marshal(metadata)
		if err != nil {
			return 0, fmt.Errorf("events: marshal metadata: %w", err)
		}
	}

	if id, forwarded, err := dbproxy.ForwardWithResult[int64](ctx, s.proxy, "SaveEvent", saveEventRequest{
		Subject:   subject,
		Content:   content,
		Metadata:  metadata,
		Embedding: embedding,
	}); forwarded {
		return id, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("events: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC()
	embBlob := serializeFloat32(embedding)

	res, err := tx.ExecContext(ctx, `
		INSERT INTO events (subject, content, metadata, embedding, event_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		subject, content, string(metaJSON), embBlob, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("events: insert event: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("events: last insert id: %w", err)
	}

	// Keep the FTS5 external-content index in sync.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO events_fts(rowid, subject, content)
		VALUES (?, ?, ?)`,
		id, subject, content,
	); err != nil {
		return 0, fmt.Errorf("events: insert fts: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("events: commit: %w", err)
	}
	return id, nil
}

type saveEventRequest struct {
	Subject   string                 `json:"subject"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata"`
	Embedding []float32              `json:"embedding"`
}

type replaceSessionEventsRequest struct {
	SessionID  string                 `json:"session_id"`
	Subject    string                 `json:"subject"`
	Metadata   map[string]interface{} `json:"metadata"`
	Chunks     []string               `json:"chunks"`
	Embeddings [][]float32            `json:"embeddings"`
}

// replaceMessageEventsRequest is the IPC payload for ReplaceMessageEvents,
// mirrored (unexported, same field set) by internal/rag/proxy.dispatcher for
// JSON decoding on the primary.
type replaceMessageEventsRequest struct {
	SessionID  string                 `json:"session_id"`
	MessageID  string                 `json:"message_id"`
	Subject    string                 `json:"subject"`
	Metadata   map[string]interface{} `json:"metadata"`
	Chunks     []string               `json:"chunks"`
	Embeddings [][]float32            `json:"embeddings"`
}

// deleteMessageEventsRequest is the IPC payload for DeleteMessageEvents.
type deleteMessageEventsRequest struct {
	MessageID string `json:"message_id"`
	Subject   string `json:"subject"`
}

// SaveEventWithEmbedding inserts an event using a pre-computed embedding.
// Called by the primary IPC dispatcher when a secondary forwards a SaveEvent write.
// No embedding generation is performed; the provided embedding is stored directly.
func (s *EventStore) SaveEventWithEmbedding(ctx context.Context, subject, content string, metadata map[string]interface{}, embedding []float32) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("events: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	id, err := s.insertEventTx(ctx, tx, subject, content, metadata, embedding, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("events: commit: %w", err)
	}
	return id, nil
}

// ReplaceSessionEvents replaces all indexed chunks for a session atomically.
// Embeddings must already be computed; if embedding generation fails before this
// method is called, the previous indexed version remains untouched.
func (s *EventStore) ReplaceSessionEvents(ctx context.Context, sessionID, subject string, metadata map[string]interface{}, chunks []string, embeddings [][]float32) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("events: session_id cannot be empty")
	}
	if strings.TrimSpace(subject) == "" {
		subject = "session"
	}
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("events: chunk embedding count mismatch: got %d embeddings for %d chunks", len(embeddings), len(chunks))
	}

	if forwarded, err := s.proxy.Forward(ctx, "ReplaceSessionEvents", replaceSessionEventsRequest{
		SessionID:  sessionID,
		Subject:    subject,
		Metadata:   metadata,
		Chunks:     chunks,
		Embeddings: embeddings,
	}, dbproxy.DefaultWriteTimeouts.Long); forwarded {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("events: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.deleteSessionEventsTx(ctx, tx, subject, sessionID); err != nil {
		return err
	}

	now := time.Now().UTC()
	for i, chunk := range chunks {
		chunkMetadata := cloneMetadata(metadata)
		chunkMetadata["chunk_index"] = i
		chunkMetadata["chunk_count"] = len(chunks)
		if _, err := s.insertEventTx(ctx, tx, subject, chunk, chunkMetadata, embeddings[i], now); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("events: commit replace session events: %w", err)
	}
	return nil
}

// ReplaceMessageEvents replaces all indexed chunks for exactly one message
// (identified by messageID) atomically, without touching any other message's
// rows — including other messages in the same session. This is the
// per-message counterpart to ReplaceSessionEvents used by the incremental
// session indexer (internal/app/remembrances_indexer.go): only messages that
// are new or whose content changed need to go through this path, so a
// session-wide replace-all (O(n) re-embedding, O(n) write tx) is no longer
// required on every index run. sessionID is carried for logging/API parity
// with ReplaceSessionEvents and is expected in metadata (as "session_id") for
// search-time filtering; the delete scope itself is keyed by messageID alone,
// which is safe because message IDs (internal/message.Message.ID) are
// globally unique UUIDs, not just unique within a session.
// Embeddings must already be computed; if embedding generation fails before
// this method is called, the previous indexed version of this message
// remains untouched.
func (s *EventStore) ReplaceMessageEvents(ctx context.Context, sessionID, messageID, subject string, metadata map[string]interface{}, chunks []string, embeddings [][]float32) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("events: session_id cannot be empty")
	}
	if strings.TrimSpace(messageID) == "" {
		return fmt.Errorf("events: message_id cannot be empty")
	}
	if strings.TrimSpace(subject) == "" {
		subject = "session"
	}
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("events: chunk embedding count mismatch: got %d embeddings for %d chunks", len(embeddings), len(chunks))
	}

	if forwarded, err := s.proxy.Forward(ctx, "ReplaceMessageEvents", replaceMessageEventsRequest{
		SessionID:  sessionID,
		MessageID:  messageID,
		Subject:    subject,
		Metadata:   metadata,
		Chunks:     chunks,
		Embeddings: embeddings,
	}, dbproxy.DefaultWriteTimeouts.Long); forwarded {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("events: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.deleteMessageEventsTx(ctx, tx, subject, messageID); err != nil {
		return err
	}

	now := time.Now().UTC()
	for i, chunk := range chunks {
		chunkMetadata := cloneMetadata(metadata)
		chunkMetadata["chunk_index"] = i
		chunkMetadata["chunk_count"] = len(chunks)
		if _, err := s.insertEventTx(ctx, tx, subject, chunk, chunkMetadata, embeddings[i], now); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("events: commit replace message events: %w", err)
	}
	return nil
}

// DeleteMessageEvents removes every indexed chunk for one message (e.g. when
// the message no longer exists in the session, such as after history
// truncation). No-op — not an error — when the message has no indexed rows.
func (s *EventStore) DeleteMessageEvents(ctx context.Context, messageID, subject string) error {
	if strings.TrimSpace(messageID) == "" {
		return fmt.Errorf("events: message_id cannot be empty")
	}
	if strings.TrimSpace(subject) == "" {
		subject = "session"
	}

	if forwarded, err := s.proxy.Forward(ctx, "DeleteMessageEvents", deleteMessageEventsRequest{
		MessageID: messageID,
		Subject:   subject,
	}, dbproxy.DefaultWriteTimeouts.Default); forwarded {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("events: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.deleteMessageEventsTx(ctx, tx, subject, messageID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("events: commit delete message events: %w", err)
	}
	return nil
}

// SessionHasLegacyRows reports whether sessionID still has any indexed rows
// written by the old whole-transcript replace path (metadata with no
// "message_id" key) rather than the per-message path. It is a cheap
// read-only lookup: the (subject, session_id) equality narrows to this
// session's rows via idx_events_session before the IS NULL filter is
// evaluated, so cost scales with the size of one session, not the whole
// events table. The incremental indexer uses this to lazily migrate a
// session on its first post-upgrade run: when true, it clears every row for
// the session (a plain ReplaceSessionEvents with no chunks) before writing
// any per-message rows, so legacy and per-message rows for the same session
// never coexist.
func (s *EventStore) SessionHasLegacyRows(ctx context.Context, sessionID, subject string) (bool, error) {
	if strings.TrimSpace(subject) == "" {
		subject = "session"
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1
		FROM events
		WHERE subject = ?
		  AND json_extract(metadata, '$.session_id') = ?
		  AND json_extract(metadata, '$.message_id') IS NULL
		LIMIT 1`,
		subject, sessionID,
	).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("events: query legacy session rows: %w", err)
	}
	return exists == 1, nil
}

// MessageEventMarkers returns, for every message currently indexed under
// sessionID (per-message rows only — see SessionHasLegacyRows for legacy
// detection), a map of message_id to its stored content_hash. The incremental
// indexer reads this once per run (a single read-only query, no lock held) to
// decide which messages are unchanged (hash matches, skip re-embedding),
// changed (hash differs, re-embed and replace), or removed (message_id
// present here but not among the session's current messages, delete). A
// message's chunk rows all carry the same content_hash (set once by the
// caller before ReplaceMessageEvents), so picking any one row per message_id
// is sufficient; GROUP BY guarantees exactly one row is returned per message.
func (s *EventStore) MessageEventMarkers(ctx context.Context, sessionID, subject string) (map[string]string, error) {
	if strings.TrimSpace(subject) == "" {
		subject = "session"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT json_extract(metadata, '$.message_id') AS message_id,
		       json_extract(metadata, '$.content_hash') AS content_hash
		FROM events
		WHERE subject = ?
		  AND json_extract(metadata, '$.session_id') = ?
		  AND json_extract(metadata, '$.message_id') IS NOT NULL
		GROUP BY json_extract(metadata, '$.message_id')`,
		subject, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("events: query message markers: %w", err)
	}
	defer rows.Close()

	markers := make(map[string]string)
	for rows.Next() {
		var messageID string
		var hash sql.NullString
		if err := rows.Scan(&messageID, &hash); err != nil {
			return nil, fmt.Errorf("events: scan message marker: %w", err)
		}
		markers[messageID] = hash.String
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("events: iterate message markers: %w", err)
	}
	return markers, nil
}

func (s *EventStore) insertEventTx(ctx context.Context, tx *sql.Tx, subject, content string, metadata map[string]interface{}, embedding []float32, now time.Time) (int64, error) {
	metaJSON := []byte("{}")
	if metadata != nil {
		var err error
		metaJSON, err = json.Marshal(metadata)
		if err != nil {
			return 0, fmt.Errorf("events: marshal metadata: %w", err)
		}
	}

	embBlob := serializeFloat32(embedding)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO events (subject, content, metadata, embedding, event_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		subject, content, string(metaJSON), embBlob, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("events: insert event: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("events: last insert id: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO events_fts(rowid, subject, content)
		VALUES (?, ?, ?)`,
		id, subject, content,
	); err != nil {
		return 0, fmt.Errorf("events: insert fts: %w", err)
	}

	return id, nil
}

// deleteSessionEventsTx deletes every row for one session. The query text
// must stay byte-for-byte identical to the expression indexed by
// idx_events_session (internal/db/migrations/20260911000001_add_events_session_index.sql)
// for SQLite to match that index.
func (s *EventStore) deleteSessionEventsTx(ctx context.Context, tx *sql.Tx, subject, sessionID string) error {
	return s.deleteEventsMatchingTx(ctx, tx,
		`SELECT id, subject, content FROM events WHERE subject = ? AND json_extract(metadata, '$.session_id') = ?`,
		`DELETE FROM events WHERE subject = ? AND json_extract(metadata, '$.session_id') = ?`,
		subject, sessionID,
	)
}

// deleteMessageEventsTx deletes every row for one message. The query text
// must stay byte-for-byte identical to the expression indexed by
// idx_events_message (internal/db/migrations/20260911190000_add_events_message_index.sql)
// for SQLite to match that index.
func (s *EventStore) deleteMessageEventsTx(ctx context.Context, tx *sql.Tx, subject, messageID string) error {
	return s.deleteEventsMatchingTx(ctx, tx,
		`SELECT id, subject, content FROM events WHERE subject = ? AND json_extract(metadata, '$.message_id') = ?`,
		`DELETE FROM events WHERE subject = ? AND json_extract(metadata, '$.message_id') = ?`,
		subject, messageID,
	)
}

// deleteEventsMatchingTx runs selectQuery to find the rows a caller is about
// to delete, removes each from the FTS5 external-content index (which must
// be kept in sync by hand — see the package doc), then runs deleteQuery to
// remove the rows themselves. selectQuery and deleteQuery must accept the
// same args and match the same rows (typically the exact same WHERE clause).
func (s *EventStore) deleteEventsMatchingTx(ctx context.Context, tx *sql.Tx, selectQuery, deleteQuery string, args ...interface{}) error {
	rows, err := tx.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return fmt.Errorf("events: query events for delete: %w", err)
	}
	defer rows.Close()

	type existingEvent struct {
		id      int64
		subject string
		content string
	}
	var existing []existingEvent
	for rows.Next() {
		var item existingEvent
		if err := rows.Scan(&item.id, &item.subject, &item.content); err != nil {
			return fmt.Errorf("events: scan event for delete: %w", err)
		}
		existing = append(existing, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("events: iterate events for delete: %w", err)
	}

	for _, item := range existing {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events_fts(events_fts, rowid, subject, content)
			VALUES ('delete', ?, ?, ?)`,
			item.id, item.subject, item.content,
		); err != nil {
			return fmt.Errorf("events: fts delete: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, deleteQuery, args...); err != nil {
		return fmt.Errorf("events: delete events: %w", err)
	}

	return nil
}

func cloneMetadata(metadata map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(metadata)+2)
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

// SearchEvents performs hybrid search with temporal filters.
// The search combines vector similarity and FTS using RRF, then applies time filters.
func (s *EventStore) SearchEvents(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	opts = defaultSearchOptions(opts)

	// Build the time filter SQL clause.
	var timeFilter string
	var timeArgs []interface{}
	if opts.FromDate != nil || opts.ToDate != nil {
		var conditions []string
		if opts.FromDate != nil {
			conditions = append(conditions, "event_at >= ?")
			timeArgs = append(timeArgs, opts.FromDate.Format(time.RFC3339))
		}
		if opts.ToDate != nil {
			conditions = append(conditions, "event_at <= ?")
			timeArgs = append(timeArgs, opts.ToDate.Format(time.RFC3339))
		}
		if len(conditions) > 0 {
			timeFilter = " AND " + conditions[0]
			if len(conditions) > 1 {
				timeFilter += " AND " + conditions[1]
			}
		}
	}

	// Subject filter.
	var subjectFilter string
	var subjectArgs []interface{}
	if opts.Subject != "" {
		subjectFilter = " AND subject = ?"
		subjectArgs = append(subjectArgs, opts.Subject)
	}

	// If no query, just list events with filters.
	if opts.Query == "" {
		return s.listEventsFiltered(ctx, opts, timeFilter, timeArgs, subjectFilter, subjectArgs)
	}

	// Generate query embedding.
	queryEmbed, err := s.embedder.EmbedQuery(ctx, opts.Query)
	if err != nil {
		return nil, fmt.Errorf("events: embed query: %w", err)
	}

	// Fetch more candidates from each sub-search to improve fusion quality.
	subLimit := opts.Limit * 3

	type result struct {
		items []SearchResult
		err   error
	}
	vecCh := make(chan result, 1)
	ftsCh := make(chan result, 1)

	go func() {
		items, err := s.searchVector(ctx, queryEmbed, subLimit, timeFilter, timeArgs, subjectFilter, subjectArgs)
		vecCh <- result{items, err}
	}()

	go func() {
		items, err := s.searchFTS(ctx, opts.Query, subLimit, timeFilter, timeArgs, opts.Subject)
		ftsCh <- result{items, err}
	}()

	vec := <-vecCh
	fts := <-ftsCh

	if vec.err != nil {
		return nil, fmt.Errorf("events: hybrid vector: %w", vec.err)
	}
	if fts.err != nil {
		return nil, fmt.Errorf("events: hybrid fts: %w", fts.err)
	}

	return rrfFuse(vec.items, fts.items, opts.Limit), nil
}

// searchVector performs vector similarity search with temporal and subject filters.
func (s *EventStore) searchVector(ctx context.Context, embedding []float32, limit int, timeFilter string, timeArgs []interface{}, subjectFilter string, subjectArgs []interface{}) ([]SearchResult, error) {
	queryNorm := l2norm(embedding)
	if queryNorm == 0 {
		return nil, fmt.Errorf("events: query embedding is a zero vector")
	}

	// Build query with filters.
	q := `SELECT id, subject, content, metadata, event_at, created_at, embedding
	      FROM events
	      WHERE embedding IS NOT NULL`
	args := make([]interface{}, 0)

	if timeFilter != "" {
		q += timeFilter
		args = append(args, timeArgs...)
	}
	if subjectFilter != "" {
		q += subjectFilter
		args = append(args, subjectArgs...)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("events: load embeddings: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		event Event
		score float64
	}
	var candidates []candidate

	for rows.Next() {
		var e Event
		var blob []byte
		var metaJSON string
		if err := rows.Scan(
			&e.ID, &e.Subject, &e.Content, &metaJSON,
			&e.EventAt, &e.CreatedAt, &blob,
		); err != nil {
			return nil, fmt.Errorf("events: scan embedding row: %w", err)
		}

		// Parse metadata.
		if err := json.Unmarshal([]byte(metaJSON), &e.Metadata); err != nil {
			// Fallback to empty metadata on parse error.
			e.Metadata = make(map[string]interface{})
		}

		vec := deserializeFloat32(blob)
		if len(vec) != len(embedding) {
			continue // skip malformed embeddings
		}
		score := cosine(embedding, queryNorm, vec)
		candidates = append(candidates, candidate{e, score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by descending similarity.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	results := make([]SearchResult, 0, limit)
	for i, cand := range candidates {
		if i >= limit {
			break
		}
		results = append(results, SearchResult{
			Event: cand.event,
			Score: cand.score,
			Rank:  i + 1,
		})
	}
	return results, nil
}

// sanitizeFTSQuery converts a natural language query to a safe FTS5 MATCH expression.
// Each word is wrapped in double quotes to prevent FTS5 syntax errors from
// special characters such as ., (, ), *, ^, :, AND, OR, NOT.
func sanitizeFTSQuery(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}
	parts := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ReplaceAll(w, `"`, `""`)
		if w != "" {
			parts = append(parts, `"`+w+`"`)
		}
	}
	return strings.Join(parts, " ")
}

// searchFTS performs full-text search with temporal and subject filters.
func (s *EventStore) searchFTS(ctx context.Context, query string, limit int, timeFilter string, timeArgs []interface{}, subject string) ([]SearchResult, error) {
	q := `
		SELECT e.id, e.subject, e.content, e.metadata, e.event_at, e.created_at,
		       -bm25(events_fts) AS score
		FROM events_fts
		JOIN events e ON e.id = events_fts.rowid
		WHERE events_fts MATCH ?`
	args := []interface{}{sanitizeFTSQuery(query)}

	if timeFilter != "" {
		q += timeFilter
		args = append(args, timeArgs...)
	}
	if subject != "" {
		q += " AND e.subject = ?"
		args = append(args, subject)
	}

	q += " ORDER BY score DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("events: fts search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var e Event
		var metaJSON string
		var rawScore float64
		if err := rows.Scan(
			&e.ID, &e.Subject, &e.Content, &metaJSON,
			&e.EventAt, &e.CreatedAt, &rawScore,
		); err != nil {
			return nil, fmt.Errorf("events: scan fts result: %w", err)
		}

		// Parse metadata.
		if err := json.Unmarshal([]byte(metaJSON), &e.Metadata); err != nil {
			e.Metadata = make(map[string]interface{})
		}

		results = append(results, SearchResult{
			Event: e,
			Score: rawScore,
			Rank:  len(results) + 1,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Normalise scores to [0, 1] relative to the top result.
	if len(results) > 0 && results[0].Score > 0 {
		max := results[0].Score
		for i := range results {
			results[i].Score /= max
		}
	}
	return results, nil
}

// listEventsFiltered lists events with filters but no search query.
func (s *EventStore) listEventsFiltered(ctx context.Context, opts SearchOptions, timeFilter string, timeArgs []interface{}, subjectFilter string, subjectArgs []interface{}) ([]SearchResult, error) {
	q := `SELECT id, subject, content, metadata, event_at, created_at
	      FROM events
	      WHERE 1=1`
	args := make([]interface{}, 0)

	if timeFilter != "" {
		q += timeFilter
		args = append(args, timeArgs...)
	}
	if subjectFilter != "" {
		q += subjectFilter
		args = append(args, subjectArgs...)
	}

	q += " ORDER BY event_at DESC LIMIT ?"
	args = append(args, opts.Limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("events: list filtered: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var e Event
		var metaJSON string
		if err := rows.Scan(
			&e.ID, &e.Subject, &e.Content, &metaJSON,
			&e.EventAt, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("events: scan event: %w", err)
		}

		if err := json.Unmarshal([]byte(metaJSON), &e.Metadata); err != nil {
			e.Metadata = make(map[string]interface{})
		}

		results = append(results, SearchResult{
			Event: e,
			Score: 1.0, // no ranking when listing
			Rank:  len(results) + 1,
		})
	}
	return results, rows.Err()
}

// ListEvents returns paginated events filtered by subject (all when subject="").
func (s *EventStore) ListEvents(ctx context.Context, subject string, limit, offset int) ([]Event, error) {
	const sel = `
		SELECT id, subject, content, metadata, event_at, created_at
		FROM events`

	var (
		rows *sql.Rows
		err  error
	)
	if subject == "" {
		rows, err = s.db.QueryContext(ctx, sel+` ORDER BY event_at DESC LIMIT ? OFFSET ?`, limit, offset)
	} else {
		rows, err = s.db.QueryContext(ctx,
			sel+` WHERE subject = ? ORDER BY event_at DESC LIMIT ? OFFSET ?`,
			subject, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("events: list: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var metaJSON string
		if err := rows.Scan(&e.ID, &e.Subject, &e.Content, &metaJSON,
			&e.EventAt, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("events: scan event: %w", err)
		}

		if err := json.Unmarshal([]byte(metaJSON), &e.Metadata); err != nil {
			e.Metadata = make(map[string]interface{})
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// DeleteEvent removes an event and its FTS entry. It is a no-op when the event
// does not exist.
func (s *EventStore) DeleteEvent(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("events: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var subject, content string
	err = tx.QueryRowContext(ctx,
		`SELECT subject, content FROM events WHERE id = ?`, id,
	).Scan(&subject, &content)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // already gone
		}
		return fmt.Errorf("events: read event for delete: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO events_fts(events_fts, rowid, subject, content)
		VALUES ('delete', ?, ?, ?)`,
		id, subject, content,
	); err != nil {
		return fmt.Errorf("events: fts delete: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id); err != nil {
		return fmt.Errorf("events: delete: %w", err)
	}

	return tx.Commit()
}

// CountEvents returns the total number of events.
func (s *EventStore) CountEvents(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("events: count: %w", err)
	}
	return count, nil
}

// RebuildFTS rebuilds the FTS5 index from the events content table.
// Use this to recover from index corruption or after bulk inserts that bypassed
// the normal SaveEvent path.
func (s *EventStore) RebuildFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO events_fts(events_fts) VALUES ('rebuild')`); err != nil {
		return fmt.Errorf("events: fts rebuild: %w", err)
	}
	return nil
}

// rrfFuse merges two ranked result lists using Reciprocal Rank Fusion.
func rrfFuse(vecResults, ftsResults []SearchResult, limit int) []SearchResult {
	const rrfK = 60.0

	type entry struct {
		event Event
		rrf   float64
	}
	byID := make(map[int64]*entry)

	for rank, r := range vecResults {
		e := &entry{event: r.Event}
		e.rrf += 1.0 / (rrfK + float64(rank+1))
		byID[r.Event.ID] = e
	}
	for rank, r := range ftsResults {
		if e, ok := byID[r.Event.ID]; ok {
			e.rrf += 1.0 / (rrfK + float64(rank+1))
		} else {
			byID[r.Event.ID] = &entry{
				event: r.Event,
				rrf:   1.0 / (rrfK + float64(rank+1)),
			}
		}
	}

	fused := make([]*entry, 0, len(byID))
	for _, e := range byID {
		fused = append(fused, e)
	}
	sort.Slice(fused, func(i, j int) bool {
		return fused[i].rrf > fused[j].rrf
	})

	results := make([]SearchResult, 0, limit)
	for i, e := range fused {
		if i >= limit {
			break
		}
		results = append(results, SearchResult{
			Event: e.event,
			Score: e.rrf,
			Rank:  i + 1,
		})
	}
	return results
}

// cosine computes the cosine similarity between query and v.
// queryNorm must be pre-computed as l2norm(query) for efficiency.
func cosine(query []float32, queryNorm float64, v []float32) float64 {
	var dot float64
	var vNormSq float64
	for i := range query {
		dot += float64(query[i]) * float64(v[i])
		vNormSq += float64(v[i]) * float64(v[i])
	}
	vNorm := math.Sqrt(vNormSq)
	if vNorm == 0 {
		return 0
	}
	return dot / (queryNorm * vNorm)
}

// l2norm computes the L2 norm of a float32 vector.
func l2norm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

// serializeFloat32 encodes a []float32 as a little-endian byte blob.
func serializeFloat32(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// deserializeFloat32 decodes a little-endian byte blob back into []float32.
func deserializeFloat32(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
