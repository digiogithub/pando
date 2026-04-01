package kb

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

// KBStore manages knowledge base documents with chunking, embeddings, and hybrid search.
// It uses SQLite for storage with FTS5 for full-text search and in-memory vector search.
type KBStore struct {
	db           *sql.DB
	embedder     embeddings.Embedder
	chunkSize    int
	chunkOverlap int
}

// NewKBStore creates a new KBStore instance.
//
// Parameters:
//   - db: SQLite database connection
//   - embedder: Embedder for generating chunk embeddings
//   - chunkSize: Maximum chunk size in characters (0 = use default)
//   - chunkOverlap: Overlap between consecutive chunks (0 = use default)
func NewKBStore(db *sql.DB, embedder embeddings.Embedder, chunkSize, chunkOverlap int) *KBStore {
	if chunkSize <= 0 {
		chunkSize = embeddings.DefaultChunkSize
	}
	if chunkOverlap < 0 {
		chunkOverlap = embeddings.DefaultChunkOverlap
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 2
	}

	return &KBStore{
		db:           db,
		embedder:     embedder,
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
	}
}

// AddDocument adds a new document to the knowledge base.
// It chunks the content, generates embeddings, and updates the FTS index.
func (s *KBStore) AddDocument(ctx context.Context, filePath, content string, metadata map[string]interface{}) error {
	if filePath == "" {
		return fmt.Errorf("kb: file_path cannot be empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kb: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Serialize metadata
	metaJSON := "{}"
	if len(metadata) > 0 {
		b, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("kb: marshal metadata: %w", err)
		}
		metaJSON = string(b)
	}

	now := time.Now().UTC()

	// Insert document
	res, err := tx.ExecContext(ctx, `
		INSERT INTO kb_documents (file_path, content, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		filePath, content, metaJSON, now, now,
	)
	if err != nil {
		return fmt.Errorf("kb: insert document: %w", err)
	}

	docID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("kb: last insert id: %w", err)
	}

	// Chunk the content
	chunks := embeddings.ChunkText(content, s.chunkSize, s.chunkOverlap)
	if len(chunks) == 0 {
		// No chunks, still commit the document
		return tx.Commit()
	}

	// Generate embeddings for all chunks
	embedVecs, err := s.embedder.EmbedDocuments(ctx, chunks)
	if err != nil {
		return fmt.Errorf("kb: embed chunks: %w", err)
	}

	if len(embedVecs) != len(chunks) {
		return fmt.Errorf("kb: embedding count mismatch: got %d, expected %d", len(embedVecs), len(chunks))
	}

	// Insert chunks with embeddings
	for i, chunk := range chunks {
		var embBlob []byte
		if i < len(embedVecs) {
			embBlob = serializeFloat32(embedVecs[i])
		}

		chunkRes, err := tx.ExecContext(ctx, `
			INSERT INTO kb_chunks (document_id, chunk_index, content, embedding, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			docID, i, chunk, embBlob, now,
		)
		if err != nil {
			return fmt.Errorf("kb: insert chunk %d: %w", i, err)
		}

		chunkID, err := chunkRes.LastInsertId()
		if err != nil {
			return fmt.Errorf("kb: chunk last insert id: %w", err)
		}

		// Update FTS index
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO kb_fts(rowid, content)
			VALUES (?, ?)`,
			chunkID, chunk,
		); err != nil {
			return fmt.Errorf("kb: insert fts: %w", err)
		}
	}

	return tx.Commit()
}

// GetDocument retrieves a document by file path.
func (s *KBStore) GetDocument(ctx context.Context, filePath string) (*Document, error) {
	var doc Document
	var metaJSON string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, file_path, content, metadata, created_at, updated_at
		FROM kb_documents
		WHERE file_path = ?`,
		filePath,
	).Scan(&doc.ID, &doc.FilePath, &doc.Content, &metaJSON, &doc.CreatedAt, &doc.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("kb: get document: %w", err)
	}

	// Parse metadata
	if metaJSON != "" && metaJSON != "{}" {
		if err := json.Unmarshal([]byte(metaJSON), &doc.Metadata); err != nil {
			return nil, fmt.Errorf("kb: unmarshal metadata: %w", err)
		}
	}

	return &doc, nil
}

// DeleteDocument removes a document and all its chunks from the knowledge base.
func (s *KBStore) DeleteDocument(ctx context.Context, filePath string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kb: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Get document ID
	var docID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM kb_documents WHERE file_path = ?`, filePath).Scan(&docID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // Already deleted
		}
		return fmt.Errorf("kb: find document: %w", err)
	}

	// Get chunk IDs and content for FTS deletion
	rows, err := tx.QueryContext(ctx, `
		SELECT id, content FROM kb_chunks WHERE document_id = ?`,
		docID,
	)
	if err != nil {
		return fmt.Errorf("kb: list chunks: %w", err)
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
			return fmt.Errorf("kb: scan chunk: %w", err)
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
			INSERT INTO kb_fts(kb_fts, rowid, content)
			VALUES ('delete', ?, ?)`,
			c.id, c.content,
		); err != nil {
			return fmt.Errorf("kb: fts delete chunk %d: %w", c.id, err)
		}
	}

	// Delete chunks (CASCADE will handle this, but being explicit)
	if _, err = tx.ExecContext(ctx, `DELETE FROM kb_chunks WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("kb: delete chunks: %w", err)
	}

	// Delete document
	if _, err = tx.ExecContext(ctx, `DELETE FROM kb_documents WHERE id = ?`, docID); err != nil {
		return fmt.Errorf("kb: delete document: %w", err)
	}

	return tx.Commit()
}

// UpdateDocument updates an existing document's content and metadata.
// It re-chunks and re-embeds the content.
func (s *KBStore) UpdateDocument(ctx context.Context, filePath, content string, metadata map[string]interface{}) error {
	// Delete existing document (including chunks)
	if err := s.DeleteDocument(ctx, filePath); err != nil {
		return fmt.Errorf("kb: delete for update: %w", err)
	}

	// Re-add with new content
	return s.AddDocument(ctx, filePath, content, metadata)
}

// SearchDocuments performs hybrid search combining vector similarity and FTS.
// Results are fused using Reciprocal Rank Fusion (RRF).
func (s *KBStore) SearchDocuments(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}

	// Generate query embedding
	queryEmb, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("kb: embed query: %w", err)
	}

	// Fetch more candidates for better fusion
	subLimit := limit * 3

	// Concurrent vector and FTS search
	type result struct {
		items []SearchResult
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
		return nil, fmt.Errorf("kb: vector search: %w", vec.err)
	}
	if fts.err != nil {
		return nil, fmt.Errorf("kb: fts search: %w", fts.err)
	}

	// Fuse results using RRF
	return rrfFuse(vec.items, fts.items, limit), nil
}

// searchVector performs vector similarity search on chunks.
func (s *KBStore) searchVector(ctx context.Context, queryEmb []float32, limit int) ([]SearchResult, error) {
	queryNorm := l2norm(queryEmb)
	if queryNorm == 0 {
		return nil, fmt.Errorf("kb: query embedding is zero vector")
	}

	// Load all chunks with embeddings
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.document_id, c.content, c.embedding,
		       d.file_path, d.content, d.metadata, d.created_at, d.updated_at
		FROM kb_chunks c
		JOIN kb_documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("kb: load embeddings: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		chunkID      int64
		chunkContent string
		document     Document
		score        float64
	}
	var candidates []candidate

	for rows.Next() {
		var c candidate
		var blob []byte
		var metaJSON string

		if err := rows.Scan(
			&c.chunkID, &c.document.ID, &c.chunkContent, &blob,
			&c.document.FilePath, &c.document.Content, &metaJSON,
			&c.document.CreatedAt, &c.document.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("kb: scan chunk: %w", err)
		}

		// Parse metadata
		if metaJSON != "" && metaJSON != "{}" {
			if err := json.Unmarshal([]byte(metaJSON), &c.document.Metadata); err != nil {
				continue // Skip malformed metadata
			}
		}

		vec := deserializeFloat32(blob)
		if len(vec) != len(queryEmb) {
			continue // Skip dimension mismatch
		}

		c.score = cosine(queryEmb, queryNorm, vec)
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by descending similarity
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Return top-k
	results := make([]SearchResult, 0, limit)
	for i, cand := range candidates {
		if i >= limit {
			break
		}
		results = append(results, SearchResult{
			Document:     cand.document,
			ChunkContent: cand.chunkContent,
			Score:        cand.score,
			Rank:         i + 1,
		})
	}

	return results, nil
}

// searchFTS performs full-text search using SQLite FTS5.
func (s *KBStore) searchFTS(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.content,
		       d.id, d.file_path, d.content, d.metadata, d.created_at, d.updated_at,
		       -bm25(kb_fts) AS score
		FROM kb_fts
		JOIN kb_chunks c ON c.id = kb_fts.rowid
		JOIN kb_documents d ON d.id = c.document_id
		WHERE kb_fts MATCH ?
		ORDER BY score DESC
		LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("kb: fts search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var chunkID int64
		var metaJSON string
		var rawScore float64

		if err := rows.Scan(
			&chunkID, &r.ChunkContent,
			&r.Document.ID, &r.Document.FilePath, &r.Document.Content, &metaJSON,
			&r.Document.CreatedAt, &r.Document.UpdatedAt,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("kb: scan fts result: %w", err)
		}

		// Parse metadata
		if metaJSON != "" && metaJSON != "{}" {
			if err := json.Unmarshal([]byte(metaJSON), &r.Document.Metadata); err != nil {
				continue // Skip malformed metadata
			}
		}

		r.Score = rawScore
		r.Rank = len(results) + 1
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

// ListDocuments returns paginated documents.
func (s *KBStore) ListDocuments(ctx context.Context, limit, offset int) ([]Document, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, file_path, content, metadata, created_at, updated_at
		FROM kb_documents
		ORDER BY id
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("kb: list documents: %w", err)
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var doc Document
		var metaJSON string

		if err := rows.Scan(
			&doc.ID, &doc.FilePath, &doc.Content, &metaJSON,
			&doc.CreatedAt, &doc.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("kb: scan document: %w", err)
		}

		// Parse metadata
		if metaJSON != "" && metaJSON != "{}" {
			if err := json.Unmarshal([]byte(metaJSON), &doc.Metadata); err != nil {
				return nil, fmt.Errorf("kb: unmarshal metadata: %w", err)
			}
		}

		docs = append(docs, doc)
	}

	return docs, rows.Err()
}

// CountDocuments returns the total number of documents.
func (s *KBStore) CountDocuments(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_documents`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("kb: count documents: %w", err)
	}
	return count, nil
}

// SyncDirectory imports or syncs all .md files from a directory.
// Existing documents are updated, new files are added.
func (s *KBStore) SyncDirectory(ctx context.Context, dirPath string) error {
	_, err := s.SyncDirectoryWithStats(ctx, dirPath, true)
	return err
}

// RebuildFTS rebuilds the FTS5 index from kb_chunks.
func (s *KBStore) RebuildFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO kb_fts(kb_fts) VALUES ('rebuild')`); err != nil {
		return fmt.Errorf("kb: fts rebuild: %w", err)
	}
	return nil
}

// rrfFuse merges two ranked result lists using Reciprocal Rank Fusion.
func rrfFuse(vecResults, ftsResults []SearchResult, limit int) []SearchResult {
	const rrfK = 60.0

	type entry struct {
		doc          Document
		chunkContent string
		rrf          float64
	}

	// Use document ID + chunk content as key for deduplication
	type key struct {
		docID   int64
		content string
	}
	byKey := make(map[key]*entry)

	for rank, r := range vecResults {
		k := key{r.Document.ID, r.ChunkContent}
		e := &entry{
			doc:          r.Document,
			chunkContent: r.ChunkContent,
		}
		e.rrf += 1.0 / (rrfK + float64(rank+1))
		byKey[k] = e
	}

	for rank, r := range ftsResults {
		k := key{r.Document.ID, r.ChunkContent}
		if e, ok := byKey[k]; ok {
			e.rrf += 1.0 / (rrfK + float64(rank+1))
		} else {
			byKey[k] = &entry{
				doc:          r.Document,
				chunkContent: r.ChunkContent,
				rrf:          1.0 / (rrfK + float64(rank+1)),
			}
		}
	}

	fused := make([]*entry, 0, len(byKey))
	for _, e := range byKey {
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
			Document:     e.doc,
			ChunkContent: e.chunkContent,
			Score:        e.rrf,
			Rank:         i + 1,
		})
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
