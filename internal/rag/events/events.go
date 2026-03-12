package events

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/digiogithub/pando/internal/rag/embeddings"
)

// EventStore manages temporal events with semantic search capabilities.
type EventStore struct {
	db       *sql.DB
	embedder embeddings.Embedder
}

// NewEventStore creates a new EventStore backed by db.
// The embedder is used to generate embeddings for event content.
func NewEventStore(db *sql.DB, embedder embeddings.Embedder) *EventStore {
	return &EventStore{
		db:       db,
		embedder: embedder,
	}
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
		items, err := s.searchFTS(ctx, opts.Query, subLimit, timeFilter, timeArgs, subjectFilter, subjectArgs)
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

// searchFTS performs full-text search with temporal and subject filters.
func (s *EventStore) searchFTS(ctx context.Context, query string, limit int, timeFilter string, timeArgs []interface{}, subjectFilter string, subjectArgs []interface{}) ([]SearchResult, error) {
	q := `
		SELECT e.id, e.subject, e.content, e.metadata, e.event_at, e.created_at,
		       -bm25(events_fts) AS score
		FROM events_fts
		JOIN events e ON e.id = events_fts.rowid
		WHERE events_fts MATCH ?`
	args := []interface{}{query}

	if timeFilter != "" {
		q += timeFilter
		args = append(args, timeArgs...)
	}
	if subjectFilter != "" {
		q += subjectFilter
		args = append(args, subjectArgs...)
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
