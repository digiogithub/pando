package sessions

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

// SessionRAGStore manages session documents with chunking, embeddings, and hybrid search.
// It uses SQLite for storage with FTS5 for full-text search and in-memory vector search.
type SessionRAGStore struct {
	db           *sql.DB
	embedder     embeddings.Embedder
	chunkSize    int
	chunkOverlap int
}

// NewSessionRAGStore creates a new SessionRAGStore instance.
func NewSessionRAGStore(db *sql.DB, embedder embeddings.Embedder, chunkSize, chunkOverlap int) *SessionRAGStore {
	if chunkSize <= 0 {
		chunkSize = embeddings.DefaultChunkSize
	}
	if chunkOverlap < 0 {
		chunkOverlap = embeddings.DefaultChunkOverlap
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 2
	}

	return &SessionRAGStore{
		db:           db,
		embedder:     embedder,
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
	}
}

// InitTables creates the required tables if they do not exist.
func (s *SessionRAGStore) InitTables(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS session_rag_documents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			metadata TEXT NOT NULL DEFAULT '{}',
			message_count INTEGER NOT NULL DEFAULT 0,
			turn_count INTEGER NOT NULL DEFAULT 0,
			model TEXT DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(session_id)
		)`,
		`CREATE TABLE IF NOT EXISTS session_rag_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER NOT NULL REFERENCES session_rag_documents(id) ON DELETE CASCADE,
			chunk_index INTEGER NOT NULL,
			content TEXT NOT NULL,
			embedding BLOB,
			role TEXT DEFAULT '',
			turn_start INTEGER DEFAULT 0,
			turn_end INTEGER DEFAULT 0,
			created_at DATETIME NOT NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS session_rag_fts USING fts5(
			content,
			tokenize = 'porter unicode61'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_rag_docs_session ON session_rag_documents(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_session_rag_chunks_doc ON session_rag_chunks(document_id)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sessions: init tables: %w", err)
		}
	}
	return nil
}

// IndexSession stores or replaces a session document with its chunks and embeddings.
// If a document with the same session_id already exists it is deleted and re-inserted.
func (s *SessionRAGStore) IndexSession(
	ctx context.Context,
	sessionID, title, content string,
	metadata map[string]interface{},
	messageCount, turnCount int,
	model string,
) error {
	if sessionID == "" {
		return fmt.Errorf("sessions: session_id cannot be empty")
	}

	// Delete any existing document for this session first (upsert via delete+insert)
	if err := s.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("sessions: delete before re-index: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sessions: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Serialize metadata
	metaJSON := "{}"
	if len(metadata) > 0 {
		b, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("sessions: marshal metadata: %w", err)
		}
		metaJSON = string(b)
	}

	now := time.Now().UTC()

	// Insert document
	res, err := tx.ExecContext(ctx, `
		INSERT INTO session_rag_documents
			(session_id, title, content, metadata, message_count, turn_count, model, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, title, content, metaJSON, messageCount, turnCount, model, now, now,
	)
	if err != nil {
		return fmt.Errorf("sessions: insert document: %w", err)
	}

	docID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("sessions: last insert id: %w", err)
	}

	// Chunk the content
	chunks := embeddings.ChunkText(content, s.chunkSize, s.chunkOverlap)
	if len(chunks) == 0 {
		return tx.Commit()
	}

	// Generate embeddings for all chunks
	embedVecs, err := s.embedder.EmbedDocuments(ctx, chunks)
	if err != nil {
		return fmt.Errorf("sessions: embed chunks: %w", err)
	}

	if len(embedVecs) != len(chunks) {
		return fmt.Errorf("sessions: embedding count mismatch: got %d, expected %d", len(embedVecs), len(chunks))
	}

	// Insert chunks with embeddings and FTS
	for i, chunk := range chunks {
		var embBlob []byte
		if i < len(embedVecs) {
			embBlob = serializeFloat32(embedVecs[i])
		}

		chunkRes, err := tx.ExecContext(ctx, `
			INSERT INTO session_rag_chunks (document_id, chunk_index, content, embedding, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			docID, i, chunk, embBlob, now,
		)
		if err != nil {
			return fmt.Errorf("sessions: insert chunk %d: %w", i, err)
		}

		chunkID, err := chunkRes.LastInsertId()
		if err != nil {
			return fmt.Errorf("sessions: chunk last insert id: %w", err)
		}

		if _, err = tx.ExecContext(ctx, `
			INSERT INTO session_rag_fts(rowid, content)
			VALUES (?, ?)`,
			chunkID, chunk,
		); err != nil {
			return fmt.Errorf("sessions: insert fts chunk %d: %w", i, err)
		}
	}

	return tx.Commit()
}

// DeleteSession removes a session document and all its chunks from the store.
func (s *SessionRAGStore) DeleteSession(ctx context.Context, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sessions: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Get document ID
	var docID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM session_rag_documents WHERE session_id = ?`, sessionID).Scan(&docID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // Already deleted
		}
		return fmt.Errorf("sessions: find document: %w", err)
	}

	// Get chunk IDs and content for FTS deletion
	rows, err := tx.QueryContext(ctx, `
		SELECT id, content FROM session_rag_chunks WHERE document_id = ?`, docID)
	if err != nil {
		return fmt.Errorf("sessions: list chunks: %w", err)
	}

	type chunkInfo struct {
		id      int64
		content string
	}
	var chunks []chunkInfo

	for rows.Next() {
		var c chunkInfo
		if err := rows.Scan(&c.id, &c.content); err != nil {
			rows.Close()
			return fmt.Errorf("sessions: scan chunk: %w", err)
		}
		chunks = append(chunks, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Delete from FTS index
	for _, c := range chunks {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO session_rag_fts(session_rag_fts, rowid, content)
			VALUES ('delete', ?, ?)`,
			c.id, c.content,
		); err != nil {
			return fmt.Errorf("sessions: fts delete chunk %d: %w", c.id, err)
		}
	}

	// Delete chunks
	if _, err = tx.ExecContext(ctx, `DELETE FROM session_rag_chunks WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("sessions: delete chunks: %w", err)
	}

	// Delete document
	if _, err = tx.ExecContext(ctx, `DELETE FROM session_rag_documents WHERE id = ?`, docID); err != nil {
		return fmt.Errorf("sessions: delete document: %w", err)
	}

	return tx.Commit()
}

// GetSessionDocument retrieves a session document by session_id.
// Returns nil, nil if not found.
func (s *SessionRAGStore) GetSessionDocument(ctx context.Context, sessionID string) (*SessionDocument, error) {
	var doc SessionDocument
	var metaJSON string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, title, content, metadata, message_count, turn_count, model, created_at, updated_at
		FROM session_rag_documents
		WHERE session_id = ?`,
		sessionID,
	).Scan(
		&doc.ID, &doc.SessionID, &doc.Title, &doc.Content, &metaJSON,
		&doc.MessageCount, &doc.TurnCount, &doc.Model,
		&doc.CreatedAt, &doc.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("sessions: get document: %w", err)
	}

	if metaJSON != "" && metaJSON != "{}" {
		if err := json.Unmarshal([]byte(metaJSON), &doc.Metadata); err != nil {
			return nil, fmt.Errorf("sessions: unmarshal metadata: %w", err)
		}
	}

	return &doc, nil
}

// SearchSessions performs hybrid search combining vector similarity and FTS.
// Results are fused using Reciprocal Rank Fusion (RRF) and deduplicated by session_id.
func (s *SessionRAGStore) SearchSessions(ctx context.Context, query string, limit int) ([]SessionSearchResult, error) {
	if limit <= 0 {
		limit = 5
	}

	// Generate query embedding
	queryEmb, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sessions: embed query: %w", err)
	}

	subLimit := limit * 3

	type result struct {
		items []SessionSearchResult
		err   error
	}
	vecCh := make(chan result, 1)
	ftsCh := make(chan result, 1)

	go func() {
		items, err := s.searchVector(ctx, queryEmb, subLimit)
		vecCh <- result{items, err}
	}()

	go func() {
		items, err := s.searchFTS(ctx, query, subLimit)
		ftsCh <- result{items, err}
	}()

	vec := <-vecCh
	fts := <-ftsCh

	if vec.err != nil {
		return nil, fmt.Errorf("sessions: vector search: %w", vec.err)
	}
	if fts.err != nil {
		return nil, fmt.Errorf("sessions: fts search: %w", fts.err)
	}

	return rrfFuse(vec.items, fts.items, limit), nil
}

// searchVector performs vector similarity search on session chunks.
func (s *SessionRAGStore) searchVector(ctx context.Context, queryEmb []float32, limit int) ([]SessionSearchResult, error) {
	queryNorm := l2norm(queryEmb)
	if queryNorm == 0 {
		return nil, fmt.Errorf("sessions: query embedding is zero vector")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.content, c.role, c.turn_start, c.turn_end, c.embedding,
		       d.session_id, d.title
		FROM session_rag_chunks c
		JOIN session_rag_documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("sessions: load embeddings: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		result SessionSearchResult
		score  float64
	}
	var candidates []candidate

	for rows.Next() {
		var chunkID int64
		var blob []byte
		var r SessionSearchResult
		r.Source = "session"

		if err := rows.Scan(
			&chunkID, &r.Content, &r.Role, &r.TurnStart, &r.TurnEnd, &blob,
			&r.SessionID, &r.Title,
		); err != nil {
			return nil, fmt.Errorf("sessions: scan chunk: %w", err)
		}

		vec := deserializeFloat32(blob)
		if len(vec) != len(queryEmb) {
			continue
		}

		score := cosine(queryEmb, queryNorm, vec)
		r.Similarity = score
		r.Score = score
		candidates = append(candidates, candidate{result: r, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	results := make([]SessionSearchResult, 0, limit)
	for i, cand := range candidates {
		if i >= limit {
			break
		}
		cand.result.Score = cand.score
		results = append(results, cand.result)
	}

	return results, nil
}

// searchFTS performs full-text search using SQLite FTS5.
func (s *SessionRAGStore) searchFTS(ctx context.Context, query string, limit int) ([]SessionSearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.content, c.role, c.turn_start, c.turn_end,
		       d.session_id, d.title,
		       -bm25(session_rag_fts) AS score
		FROM session_rag_fts
		JOIN session_rag_chunks c ON c.id = session_rag_fts.rowid
		JOIN session_rag_documents d ON d.id = c.document_id
		WHERE session_rag_fts MATCH ?
		ORDER BY score DESC
		LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sessions: fts search: %w", err)
	}
	defer rows.Close()

	var results []SessionSearchResult
	for rows.Next() {
		var r SessionSearchResult
		var rawScore float64
		r.Source = "session"

		if err := rows.Scan(
			&r.Content, &r.Role, &r.TurnStart, &r.TurnEnd,
			&r.SessionID, &r.Title,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("sessions: scan fts result: %w", err)
		}

		r.Score = rawScore
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Normalize scores to [0, 1]
	if len(results) > 0 && results[0].Score > 0 {
		max := results[0].Score
		for i := range results {
			results[i].Score /= max
		}
	}

	return results, nil
}

// rrfFuse merges two ranked result lists using Reciprocal Rank Fusion.
// It deduplicates by session_id, keeping the best-scoring chunk per session.
func rrfFuse(vecResults, ftsResults []SessionSearchResult, limit int) []SessionSearchResult {
	const rrfK = 60.0

	type entry struct {
		result SessionSearchResult
		rrf    float64
	}

	// Deduplicate by session_id: keep the chunk that appears first (highest ranked)
	bySession := make(map[string]*entry)

	for rank, r := range vecResults {
		score := 1.0 / (rrfK + float64(rank+1))
		if e, ok := bySession[r.SessionID]; ok {
			e.rrf += score
		} else {
			re := r
			bySession[r.SessionID] = &entry{result: re, rrf: score}
		}
	}

	for rank, r := range ftsResults {
		score := 1.0 / (rrfK + float64(rank+1))
		if e, ok := bySession[r.SessionID]; ok {
			e.rrf += score
		} else {
			re := r
			bySession[r.SessionID] = &entry{result: re, rrf: score}
		}
	}

	fused := make([]*entry, 0, len(bySession))
	for _, e := range bySession {
		fused = append(fused, e)
	}

	sort.Slice(fused, func(i, j int) bool {
		return fused[i].rrf > fused[j].rrf
	})

	results := make([]SessionSearchResult, 0, limit)
	for i, e := range fused {
		if i >= limit {
			break
		}
		e.result.Score = e.rrf
		results = append(results, e.result)
	}

	return results
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

// cosine computes the cosine similarity between query and v.
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
