package kb

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/embeddings"
)

const kbEmbeddingsTimeout = 45 * time.Second

// KBStore manages knowledge base documents with chunking, embeddings, and hybrid search.
// It uses SQLite for storage with FTS5 for full-text search and in-memory vector search.
type KBStore struct {
	db           *sql.DB
	embedder     embeddings.Embedder
	proxy        *dbproxy.DBProxy
	chunkSize    int
	chunkOverlap int
	syncWorkers  int
	fsMirrorPath string
	fsMu         sync.RWMutex
	converter    DocumentConverter

	// selfWriteMu guards selfWrites, the bounded record of filesystem mirror
	// writes/deletes this store just made (selfwrite.go). The watcher
	// consults it to drop the fsnotify event its own mirror write generates,
	// instead of feeding that write back through UpdateDocument and
	// clobbering the metadata the write just stored (PANDO-US-0004).
	selfWriteMu sync.Mutex
	selfWrites  map[string]selfWriteEntry

	// writeObserver and searchMiddleware are the extension hooks (observer.go).
	// Both are nil in a standard build and guarded by fsMu like the other
	// hot-swappable fields above.
	writeObserver    WriteObserver
	searchMiddleware SearchMiddleware

	// wikiLinks toggles [[wiki link]] extraction and the graph queries built on
	// it. Defaults to true; the app sets it from Remembrances.KBWikiLinks.
	wikiLinks bool

	// embeddingModel is the configured document embedder's model id (e.g.
	// "text-embedding-3-small"), set via SetEmbeddingModel from
	// Remembrances.DocumentEmbeddingModel. It is recorded on every chunk this
	// store writes so a later embedder change can be detected instead of
	// silently degrading recall (PANDO-US-0029). Empty is a valid value (no
	// model configured / not set by the caller) and is treated as "unknown",
	// not a mismatch, everywhere it is read.
	embeddingModel string
}

// SetEmbeddingModel records the document embedder's model id so it can be
// written alongside every chunk this store inserts. Called once at startup
// from Remembrances.DocumentEmbeddingModel (internal/rag/service.go).
func (s *KBStore) SetEmbeddingModel(model string) {
	s.embeddingModel = model
}

// EmbeddingModel returns the configured document embedder's model id, as set
// by SetEmbeddingModel. Empty when never set.
func (s *KBStore) EmbeddingModel() string {
	return s.embeddingModel
}

// DocumentConverter converts rich document formats (docx, pdf, xlsx, …) to
// Markdown for on-the-fly KB ingestion. It is implemented by
// internal/convert.Converter and injected via SetDocumentConverter to avoid a
// hard dependency from the kb package on the conversion library.
type DocumentConverter interface {
	// ConvertFile converts the file at path to Markdown text.
	ConvertFile(path string) (string, error)
	// IsConvertibleDocument reports whether path is a document format that
	// should be auto-converted during ingestion (curated subset, excludes
	// plain markdown/text which is indexed verbatim).
	IsConvertibleDocument(path string) bool
}

// SetDocumentConverter installs a converter so that supported document files
// found in the KB source directory are converted to Markdown and indexed,
// referencing the original file. Passing nil disables conversion (the default),
// in which case only .md files are indexed.
func (s *KBStore) SetDocumentConverter(c DocumentConverter) {
	s.fsMu.Lock()
	s.converter = c
	s.fsMu.Unlock()
}

// documentConverter returns the installed converter (may be nil).
func (s *KBStore) documentConverter() DocumentConverter {
	s.fsMu.RLock()
	defer s.fsMu.RUnlock()
	return s.converter
}

type documentMetadata struct {
	FilePath string
	Metadata map[string]interface{}
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
		syncWorkers:  defaultSyncWorkers(),
		wikiLinks:    true,
	}
}

// SetWikiLinksEnabled toggles the [[wiki link]] graph. When false no link rows
// are written and every graph query answers empty, so the KB tools present the
// output they presented before the graph existed.
//
// Turning it off does not wipe the graph: the rows already indexed stay in the
// database, invisible, and light up again if it is turned back on — no reindex
// needed. Only a document rewritten while it is off loses its rows, like any
// update does.
func (s *KBStore) SetWikiLinksEnabled(enabled bool) {
	s.fsMu.Lock()
	s.wikiLinks = enabled
	s.fsMu.Unlock()
}

// WikiLinksEnabled reports whether the wiki link graph is active.
func (s *KBStore) WikiLinksEnabled() bool {
	s.fsMu.RLock()
	defer s.fsMu.RUnlock()
	return s.wikiLinks
}

// SetWriteProxy configures a DB proxy for mutating operations.
func (s *KBStore) SetWriteProxy(proxy *dbproxy.DBProxy) {
	s.proxy = proxy
}

func defaultSyncWorkers() int {
	n := runtime.NumCPU() / 2
	if n < 2 {
		return 2
	}
	if n > 8 {
		return 8
	}
	return n
}

// SetSyncWorkers configures how many concurrent workers are used for KB filesystem
// import preprocessing (file reads and in-memory preparation). SQLite writes remain
// serialized through a single writer goroutine.
func (s *KBStore) SetSyncWorkers(workers int) {
	if workers < 1 {
		workers = 1
	}
	if workers > 32 {
		workers = 32
	}
	s.syncWorkers = workers
}

func (s *KBStore) getSyncWorkers() int {
	if s.syncWorkers < 1 {
		return 1
	}
	return s.syncWorkers
}

// AddDocument adds a new document to the knowledge base.
// It chunks the content, generates embeddings, and updates the FTS index.
//
// The write is published to the observer (observer.go) only after it commits.
func (s *KBStore) AddDocument(ctx context.Context, filePath, content string, metadata map[string]interface{}) error {
	if err := s.addDocument(ctx, filePath, content, metadata); err != nil {
		return err
	}
	s.publishWrite(ctx, WriteEvent{
		Kind:     WriteKindDocument,
		Op:       WriteCreated,
		FilePath: filePath,
		Content:  content,
		Metadata: metadata,
	})
	return nil
}

func (s *KBStore) addDocument(ctx context.Context, filePath, content string, metadata map[string]interface{}) error {
	if filePath == "" {
		return fmt.Errorf("kb: file_path cannot be empty")
	}

	if s.proxy != nil {
		chunks := embeddings.ChunkText(content, s.chunkSize, s.chunkOverlap)
		embedVecs := make([][]float32, 0, len(chunks))
		if len(chunks) > 0 {
			logging.Debug("kb add: embedding start", "file_path", filePath, "chunks", len(chunks), "bytes", len(content))
			embedStartedAt := time.Now()
			embedCtx, embedCancel := context.WithTimeout(ctx, kbEmbeddingsTimeout)
			defer embedCancel()
			var err error
			embedVecs, err = s.embedder.EmbedDocuments(embedCtx, chunks)
			if err != nil {
				logging.Debug("kb add: embedding failed",
					"file_path", filePath,
					"chunks", len(chunks),
					"elapsed", time.Since(embedStartedAt).String(),
					"error", err,
				)
				return fmt.Errorf("kb: embed chunks: %w", err)
			}
			logging.Debug("kb add: embedding completed",
				"file_path", filePath,
				"chunks", len(chunks),
				"vectors", len(embedVecs),
				"elapsed", time.Since(embedStartedAt).String(),
			)
			if len(embedVecs) != len(chunks) {
				return fmt.Errorf("kb: embedding count mismatch: got %d, expected %d", len(embedVecs), len(chunks))
			}
		}
		return s.proxy.WriteWithRetry(ctx, "KBAddDocument", kbAddDocumentRequest{
			FilePath:       filePath,
			Content:        content,
			Metadata:       metadata,
			Chunks:         chunks,
			Embeddings:     embedVecs,
			EmbeddingModel: s.embeddingModel,
		}, dbproxy.DefaultWriteTimeouts.Long)
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

	// Index the [[wiki links]] found in the body, in the same transaction.
	if _, err := s.indexDocumentLinks(ctx, tx, docID, filePath, content); err != nil {
		return err
	}

	// Chunk the content
	chunks := embeddings.ChunkText(content, s.chunkSize, s.chunkOverlap)
	if len(chunks) == 0 {
		// No chunks, still commit the document
		return tx.Commit()
	}

	logging.Debug("kb add: embedding start", "file_path", filePath, "chunks", len(chunks), "bytes", len(content))
	embedStartedAt := time.Now()
	embedCtx, embedCancel := context.WithTimeout(ctx, kbEmbeddingsTimeout)
	defer embedCancel()

	// Generate embeddings for all chunks
	embedVecs, err := s.embedder.EmbedDocuments(embedCtx, chunks)
	if err != nil {
		logging.Debug("kb add: embedding failed",
			"file_path", filePath,
			"chunks", len(chunks),
			"elapsed", time.Since(embedStartedAt).String(),
			"error", err,
		)
		return fmt.Errorf("kb: embed chunks: %w", err)
	}
	logging.Debug("kb add: embedding completed",
		"file_path", filePath,
		"chunks", len(chunks),
		"vectors", len(embedVecs),
		"elapsed", time.Since(embedStartedAt).String(),
	)

	if len(embedVecs) != len(chunks) {
		return fmt.Errorf("kb: embedding count mismatch: got %d, expected %d", len(embedVecs), len(chunks))
	}

	// Insert chunks with embeddings
	for i, chunk := range chunks {
		var embBlob []byte
		var embModel string
		var embDims int
		if i < len(embedVecs) {
			embBlob = serializeFloat32(embedVecs[i])
			embModel = s.embeddingModel
			embDims = len(embedVecs[i])
		}

		chunkRes, err := tx.ExecContext(ctx, `
			INSERT INTO kb_chunks (document_id, chunk_index, content, embedding, created_at, embedding_model, embedding_dims)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			docID, i, chunk, embBlob, now, embModel, embDims,
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
	doc.Tags = ExtractTagsFromMetadata(doc.Metadata)

	return &doc, nil
}

// getDocumentMetadata retrieves only metadata for a document by file path.
// It avoids loading full content, which is expensive for large KB files.
func (s *KBStore) getDocumentMetadata(ctx context.Context, filePath string) (*documentMetadata, error) {
	var metaJSON string
	var meta documentMetadata

	err := s.db.QueryRowContext(ctx, `
		SELECT file_path, metadata
		FROM kb_documents
		WHERE file_path = ?`,
		filePath,
	).Scan(&meta.FilePath, &metaJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("kb: get document metadata: %w", err)
	}

	if metaJSON != "" && metaJSON != "{}" {
		if err := json.Unmarshal([]byte(metaJSON), &meta.Metadata); err != nil {
			return nil, fmt.Errorf("kb: unmarshal metadata: %w", err)
		}
	}

	return &meta, nil
}

// listDocumentMetadata returns paginated file paths + metadata only.
func (s *KBStore) listDocumentMetadata(ctx context.Context, limit, offset int) ([]documentMetadata, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT file_path, metadata
		FROM kb_documents
		ORDER BY id
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("kb: list document metadata: %w", err)
	}
	defer rows.Close()

	items := make([]documentMetadata, 0, limit)
	for rows.Next() {
		var filePath, metaJSON string
		if err := rows.Scan(&filePath, &metaJSON); err != nil {
			return nil, fmt.Errorf("kb: scan document metadata: %w", err)
		}

		item := documentMetadata{FilePath: filePath}
		if metaJSON != "" && metaJSON != "{}" {
			if err := json.Unmarshal([]byte(metaJSON), &item.Metadata); err != nil {
				return nil, fmt.Errorf("kb: unmarshal metadata: %w", err)
			}
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// DeleteDocument removes a document and all its chunks from the knowledge base.
func (s *KBStore) DeleteDocument(ctx context.Context, filePath string) error {
	if err := s.deleteDocument(ctx, filePath); err != nil {
		return err
	}
	s.publishWrite(ctx, WriteEvent{
		Kind:     WriteKindDocument,
		Op:       WriteDeleted,
		FilePath: filePath,
	})
	return nil
}

func (s *KBStore) deleteDocument(ctx context.Context, filePath string) error {
	if s.proxy != nil {
		return s.proxy.WriteWithRetry(ctx, "KBDeleteDocument", filePath, dbproxy.DefaultWriteTimeouts.Default)
	}

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

	// Same for the document's wiki links.
	if _, err = tx.ExecContext(ctx, `DELETE FROM kb_links WHERE source_document_id = ?`, docID); err != nil {
		return fmt.Errorf("kb: delete links: %w", err)
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
	if err := s.updateDocument(ctx, filePath, content, metadata); err != nil {
		return err
	}
	s.publishWrite(ctx, WriteEvent{
		Kind:     WriteKindDocument,
		Op:       WriteUpdated,
		FilePath: filePath,
		Content:  content,
		Metadata: metadata,
	})
	return nil
}

func (s *KBStore) updateDocument(ctx context.Context, filePath, content string, metadata map[string]interface{}) error {
	if s.proxy != nil {
		chunks := embeddings.ChunkText(content, s.chunkSize, s.chunkOverlap)
		embedVecs := make([][]float32, 0, len(chunks))
		if len(chunks) > 0 {
			embedCtx, embedCancel := context.WithTimeout(ctx, kbEmbeddingsTimeout)
			defer embedCancel()
			var err error
			embedVecs, err = s.embedder.EmbedDocuments(embedCtx, chunks)
			if err != nil {
				return fmt.Errorf("kb: embed chunks: %w", err)
			}
			if len(embedVecs) != len(chunks) {
				return fmt.Errorf("kb: embedding count mismatch: got %d, expected %d", len(embedVecs), len(chunks))
			}
		}
		return s.proxy.WriteWithRetry(ctx, "KBUpdateDocument", kbAddDocumentRequest{
			FilePath:       filePath,
			Content:        content,
			Metadata:       metadata,
			Chunks:         chunks,
			Embeddings:     embedVecs,
			EmbeddingModel: s.embeddingModel,
		}, dbproxy.DefaultWriteTimeouts.Long)
	}

	// Delete existing document (including chunks), then re-add with the new
	// content. Both use the unpublished forms: an update is one write and must
	// reach the observer as one event, not as a delete followed by a create.
	if err := s.deleteDocument(ctx, filePath); err != nil {
		return fmt.Errorf("kb: delete for update: %w", err)
	}

	return s.addDocument(ctx, filePath, content, metadata)
}

type kbAddDocumentRequest struct {
	FilePath   string                 `json:"file_path"`
	Content    string                 `json:"content"`
	Metadata   map[string]interface{} `json:"metadata"`
	Chunks     []string               `json:"chunks"`
	Embeddings [][]float32            `json:"embeddings"`
	// EmbeddingModel is the originating instance's configured document
	// embedder model id, recorded per chunk on the primary (PANDO-US-0029).
	// Dimensions are not forwarded separately: they are derived per chunk
	// from len(Embeddings[i]) on the receiving end.
	EmbeddingModel string `json:"embedding_model,omitempty"`
}

// AddDocumentWithEmbeddings inserts a document using pre-computed chunks and embeddings.
// Called by the primary IPC dispatcher when a secondary forwards a KBAddDocument write.
// No embedding generation is performed; the provided values are stored directly.
// embeddingModel is recorded on every inserted chunk that has an embedding
// (PANDO-US-0029); embedding_dims is derived from each vector's own length.
func (s *KBStore) AddDocumentWithEmbeddings(ctx context.Context, filePath, content string, metadata map[string]interface{}, chunks []string, embeddings [][]float32, embeddingModel string) error {
	if filePath == "" {
		return fmt.Errorf("kb: file_path cannot be empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kb: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	metaJSON := "{}"
	if len(metadata) > 0 {
		b, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("kb: marshal metadata: %w", err)
		}
		metaJSON = string(b)
	}

	now := time.Now().UTC()

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

	// Links are extracted on the primary from the forwarded content: the IPC
	// request carries no link payload, so no protocol change is needed.
	if _, err := s.indexDocumentLinks(ctx, tx, docID, filePath, content); err != nil {
		return err
	}

	if len(chunks) == 0 {
		return tx.Commit()
	}

	for i, chunk := range chunks {
		var embBlob []byte
		var embModel string
		var embDims int
		if i < len(embeddings) {
			embBlob = serializeFloat32(embeddings[i])
			embModel = embeddingModel
			embDims = len(embeddings[i])
		}

		chunkRes, err := tx.ExecContext(ctx, `
			INSERT INTO kb_chunks (document_id, chunk_index, content, embedding, created_at, embedding_model, embedding_dims)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			docID, i, chunk, embBlob, now, embModel, embDims,
		)
		if err != nil {
			return fmt.Errorf("kb: insert chunk %d: %w", i, err)
		}

		chunkID, err := chunkRes.LastInsertId()
		if err != nil {
			return fmt.Errorf("kb: chunk last insert id: %w", err)
		}

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

// SearchDocuments performs hybrid search combining vector similarity and FTS.
// Results are fused using Reciprocal Rank Fusion (RRF).
func (s *KBStore) SearchDocuments(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.SearchDocumentsWithOptions(ctx, query, limit, SearchOptions{})
}

// SearchDocumentsWithOptions performs hybrid search with optional tag filtering
// and chronological ordering. When opts.Tags is non-empty, results are filtered
// to documents whose tags fuzzy-match any of the requested tags. When
// opts.SortByDate is true, results are sorted by updated_at descending.
func (s *KBStore) SearchDocumentsWithOptions(ctx context.Context, query string, limit int, opts SearchOptions) ([]SearchResult, error) {
	if mw := s.middleware(); mw != nil {
		return mw(ctx, query, limit, opts, s.searchDocumentsWithOptions)
	}
	return s.searchDocumentsWithOptions(ctx, query, limit, opts)
}

// SearchStats reports auxiliary counters from a search call that are not
// carried by the ranked results themselves.
type SearchStats struct {
	// SkippedForDimensionMismatch counts chunks the vector leg skipped
	// because their recorded embedding dimension does not match the query
	// embedding's — evidence the document embedding model changed and some
	// chunks have not been re-embedded yet (PANDO-US-0029). Zero on a
	// consistent corpus.
	SkippedForDimensionMismatch int
}

// SearchDocumentsWithOptionsAndStats is SearchDocumentsWithOptions plus
// SearchStats. Callers that need to warn on a stale-embedding skip
// (kb_search_documents, the REST search route) use this instead of
// SearchDocumentsWithOptions; every other caller is unaffected. When a
// search extension middleware is installed (observer.go), stats are not
// tracked through it — the middleware only ever saw the ranked results, the
// same as SearchDocumentsWithOptions, so this returns a zero SearchStats in
// that case rather than misreporting.
func (s *KBStore) SearchDocumentsWithOptionsAndStats(ctx context.Context, query string, limit int, opts SearchOptions) ([]SearchResult, SearchStats, error) {
	if mw := s.middleware(); mw != nil {
		results, err := mw(ctx, query, limit, opts, s.searchDocumentsWithOptions)
		return results, SearchStats{}, err
	}
	return s.searchDocumentsWithOptionsStats(ctx, query, limit, opts)
}

func (s *KBStore) searchDocumentsWithOptions(ctx context.Context, query string, limit int, opts SearchOptions) ([]SearchResult, error) {
	results, _, err := s.searchDocumentsWithOptionsStats(ctx, query, limit, opts)
	return results, err
}

func (s *KBStore) searchDocumentsWithOptionsStats(ctx context.Context, query string, limit int, opts SearchOptions) ([]SearchResult, SearchStats, error) {
	if limit <= 0 {
		limit = 5
	}

	// When filtering by tags, fetch more candidates to compensate for post-filter reduction.
	fetchLimit := limit
	if len(opts.Tags) > 0 {
		fetchLimit = limit * 5
	}

	// Generate query embedding
	queryEmb, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, SearchStats{}, fmt.Errorf("kb: embed query: %w", err)
	}

	// Fetch more candidates for better fusion
	subLimit := fetchLimit * 3

	// Concurrent vector and FTS search
	type result struct {
		items   []SearchResult
		skipped int
		err     error
	}
	vecCh := make(chan result, 1)
	ftsCh := make(chan result, 1)

	go func() {
		items, skipped, err := s.searchVector(ctx, queryEmb, subLimit, opts.PathPrefix)
		vecCh <- result{items, skipped, err}
	}()

	go func() {
		items, err := s.searchFTS(ctx, query, subLimit, opts.PathPrefix)
		ftsCh <- result{items, 0, err}
	}()

	vec := <-vecCh
	fts := <-ftsCh

	if vec.err != nil {
		return nil, SearchStats{}, fmt.Errorf("kb: vector search: %w", vec.err)
	}
	if fts.err != nil {
		return nil, SearchStats{}, fmt.Errorf("kb: fts search: %w", fts.err)
	}
	stats := SearchStats{SkippedForDimensionMismatch: vec.skipped}

	// Fuse results using RRF — fetch more than limit to allow post-filtering.
	fused := rrfFuse(vec.items, fts.items, fetchLimit)

	// Apply tag filtering if requested.
	if len(opts.Tags) > 0 {
		fused = filterByTags(fused, opts.Tags)
	}

	// Exclude outdated memory documents when requested.
	if opts.ExcludeOutdated {
		fused = filterOutdated(fused)
	}

	// Filter by memory scope prefix when requested.
	if opts.Scope != "" {
		fused = filterByScope(fused, opts.Scope)
	}

	// Apply chronological sorting if requested.
	if opts.SortByDate {
		sort.SliceStable(fused, func(i, j int) bool {
			return fused[i].Document.UpdatedAt.After(fused[j].Document.UpdatedAt)
		})
	}

	// Trim to requested limit and re-rank.
	if len(fused) > limit {
		fused = fused[:limit]
	}
	for i := range fused {
		fused[i].Rank = i + 1
	}

	return fused, stats, nil
}

// filterByTags returns results whose document tags fuzzy-match any of the query tags.
func filterByTags(results []SearchResult, queryTags []string) []SearchResult {
	if len(queryTags) == 0 {
		return results
	}

	// Normalize query tags for case-insensitive matching.
	normalizedQuery := make([]string, len(queryTags))
	for i, t := range queryTags {
		normalizedQuery[i] = strings.ToLower(strings.TrimSpace(t))
	}

	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if matchesTags(r.Document.Tags, normalizedQuery) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// filterOutdated drops results whose document has outdated set.
// The Document.Metadata field is checked for the "outdated" key set by UpsertMemory updates.
// Since the vector/FTS scans don't load the new columns, we rely on the flag being absent
// for non-memory documents and correct for memory documents loaded via the new query paths.
func filterOutdated(results []SearchResult) []SearchResult {
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if v, ok := r.Document.Metadata["outdated"]; ok {
			switch vt := v.(type) {
			case bool:
				if vt {
					continue
				}
			case float64:
				if vt != 0 {
					continue
				}
			}
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// filterByScope keeps only results whose MemoryScope starts with the given prefix.
func filterByScope(results []SearchResult, scope string) []SearchResult {
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if strings.HasPrefix(r.Document.MemoryScope, scope) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// matchesTags returns true if any document tag fuzzy-matches any query tag.
// Fuzzy matching: case-insensitive substring containment.
func matchesTags(docTags []string, queryTags []string) bool {
	for _, dt := range docTags {
		dtLower := strings.ToLower(dt)
		for _, qt := range queryTags {
			if strings.Contains(dtLower, qt) || strings.Contains(qt, dtLower) {
				return true
			}
		}
	}
	return false
}

// searchVector performs vector similarity search on chunks.
// searchVector returns the ranked vector-similarity candidates plus the
// number of chunks skipped because their stored embedding's dimension does
// not match queryEmb's — evidence the document embedding model changed since
// those chunks were written (PANDO-US-0029). Skipped chunks are silently
// excluded from results, not an error.
func (s *KBStore) searchVector(ctx context.Context, queryEmb []float32, limit int, pathPrefix string) ([]SearchResult, int, error) {
	queryNorm := l2norm(queryEmb)
	if queryNorm == 0 {
		return nil, 0, fmt.Errorf("kb: query embedding is zero vector")
	}

	// Load all chunks with embeddings. d.content (the full document body) is
	// deliberately NOT selected: it is never read from SearchResult.Document.Content
	// (PANDO-US-0027) and materialising it once per chunk dominated both bytes
	// scanned and bytes allocated on a corpus of any size. A caller that needs
	// the full body backfills it with a keyed query over its own top-k results
	// (see documentContentByID), never by re-adding it here.
	sqlQuery := `
		SELECT c.id, c.document_id, c.content, c.embedding,
		       d.file_path, d.metadata, d.created_at, d.updated_at,
		       COALESCE(d.memory_key,''), COALESCE(d.memory_scope,''),
		       COALESCE(d.importance,0.5), COALESCE(d.hits,0),
		       COALESCE(d.source,''), COALESCE(d.outdated,0), d.expires_at
		FROM kb_chunks c
		JOIN kb_documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL`
	var args []interface{}
	// PathPrefix is pushed into the SQL as a bound parameter, never string
	// concatenation, and the prefix's own '%'/'_' are escaped so they match
	// literally — only the '%' appended in SQL after the bound value is a
	// wildcard (PANDO-US-0028).
	if pathPrefix != "" {
		sqlQuery += ` AND d.file_path LIKE ? || '%' ESCAPE '\'`
		args = append(args, escapeLikePrefix(pathPrefix))
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("kb: load embeddings: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		chunkID      int64
		chunkContent string
		document     Document
		score        float64
	}
	var candidates []candidate
	var skipped int

	for rows.Next() {
		var c candidate
		var blob []byte
		var metaJSON string
		var outdated int
		var expiresAt sql.NullTime

		if err := rows.Scan(
			&c.chunkID, &c.document.ID, &c.chunkContent, &blob,
			&c.document.FilePath, &metaJSON,
			&c.document.CreatedAt, &c.document.UpdatedAt,
			&c.document.MemoryKey, &c.document.MemoryScope, &c.document.Importance, &c.document.Hits,
			&c.document.Source, &outdated, &expiresAt,
		); err != nil {
			return nil, 0, fmt.Errorf("kb: scan chunk: %w", err)
		}
		c.document.Outdated = outdated != 0
		if expiresAt.Valid {
			t := expiresAt.Time
			c.document.ExpiresAt = &t
		}

		// Parse metadata
		if metaJSON != "" && metaJSON != "{}" {
			if err := json.Unmarshal([]byte(metaJSON), &c.document.Metadata); err != nil {
				continue // Skip malformed metadata
			}
		}
		c.document.Tags = ExtractTagsFromMetadata(c.document.Metadata)

		vec := deserializeFloat32(blob)
		if len(vec) != len(queryEmb) {
			skipped++ // Skip dimension mismatch (PANDO-US-0029)
			continue
		}

		c.score = cosine(queryEmb, queryNorm, vec)
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
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

	return results, skipped, nil
}

// searchFTS performs full-text search using SQLite FTS5.
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

// escapeLikePrefix escapes SQLite LIKE metacharacters (the escape character
// itself, '%', and '_') in a literal path prefix so that, when bound as a
// parameter to `d.file_path LIKE ? || '%' ESCAPE '\'`, a prefix containing
// '%' or '_' matches those characters literally instead of as wildcards. The
// '%' appended in SQL after the bound value is the only wildcard the
// resulting pattern carries (PANDO-US-0028).
func escapeLikePrefix(prefix string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(prefix)
}

func (s *KBStore) searchFTS(ctx context.Context, query string, limit int, pathPrefix string) ([]SearchResult, error) {
	escapedQuery := sanitizeFTSQuery(query)
	if escapedQuery == "" {
		return nil, nil
	}

	// d.content is deliberately NOT selected here either — see the matching
	// comment on searchVector's query (PANDO-US-0027).
	sqlQuery := `
		SELECT c.id, c.content,
		       d.id, d.file_path, d.metadata, d.created_at, d.updated_at,
		       -bm25(kb_fts) AS score,
		       COALESCE(d.memory_key,''), COALESCE(d.memory_scope,''),
		       COALESCE(d.importance,0.5), COALESCE(d.hits,0),
		       COALESCE(d.source,''), COALESCE(d.outdated,0), d.expires_at
		FROM kb_fts
		JOIN kb_chunks c ON c.id = kb_fts.rowid
		JOIN kb_documents d ON d.id = c.document_id
		WHERE kb_fts MATCH ?`
	args := []interface{}{escapedQuery}
	// Same bound-parameter, ESCAPE-clause path prefix as searchVector
	// (PANDO-US-0028) — never string concatenation.
	if pathPrefix != "" {
		sqlQuery += ` AND d.file_path LIKE ? || '%' ESCAPE '\'`
		args = append(args, escapeLikePrefix(pathPrefix))
	}
	sqlQuery += `
		ORDER BY score DESC
		LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
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

		var ftsOutdated int
		var ftsExpiresAt sql.NullTime
		if err := rows.Scan(
			&chunkID, &r.ChunkContent,
			&r.Document.ID, &r.Document.FilePath, &metaJSON,
			&r.Document.CreatedAt, &r.Document.UpdatedAt,
			&rawScore,
			&r.Document.MemoryKey, &r.Document.MemoryScope, &r.Document.Importance, &r.Document.Hits,
			&r.Document.Source, &ftsOutdated, &ftsExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("kb: scan fts result: %w", err)
		}
		r.Document.Outdated = ftsOutdated != 0
		if ftsExpiresAt.Valid {
			t := ftsExpiresAt.Time
			r.Document.ExpiresAt = &t
		}

		// Parse metadata
		if metaJSON != "" && metaJSON != "{}" {
			if err := json.Unmarshal([]byte(metaJSON), &r.Document.Metadata); err != nil {
				continue // Skip malformed metadata
			}
		}
		r.Document.Tags = ExtractTagsFromMetadata(r.Document.Metadata)

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

// documentContentByID backfills the full document body for exactly the given
// document IDs in a single keyed query. searchVector and searchFTS never
// select d.content (PANDO-US-0027), so a caller whose SearchResult/MemoryResult
// hits genuinely need the full body — as opposed to the matched ChunkContent
// excerpt — asks for it here, scoped to its own top-k, instead of the query
// scanning every candidate's body up front.
func (s *KBStore) documentContentByID(ctx context.Context, ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, content FROM kb_documents WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("kb: backfill document content: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]string, len(ids))
	for rows.Next() {
		var id int64
		var content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("kb: scan backfilled content: %w", err)
		}
		out[id] = content
	}
	return out, rows.Err()
}

// StaleEmbeddingStats reports chunks whose recorded embedding dimension does
// not match a given (normally the currently configured) embedder dimension
// — evidence the document embedding model changed since those chunks were
// written (PANDO-US-0029).
type StaleEmbeddingStats struct {
	// Count is the number of embedded chunks whose recorded embedding_dims
	// differs from the configured dimension.
	Count int64
	// RecordedModels lists the distinct, non-empty embedding_model values
	// found among the mismatched chunks (sorted). A chunk written before
	// PANDO-US-0029 has an empty embedding_model — treated as "unknown", not
	// listed here — even though it still counts towards Count if its
	// backfilled dimension does not match.
	RecordedModels []string
}

// CountStaleEmbeddings counts chunks whose recorded embedding dimension does
// not match configuredDims. Used by the startup staleness check
// (internal/app/remembrances.go) and by the enrichment status REST route.
func (s *KBStore) CountStaleEmbeddings(ctx context.Context, configuredDims int) (StaleEmbeddingStats, error) {
	var stats StaleEmbeddingStats

	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM kb_chunks
		WHERE embedding IS NOT NULL AND embedding_dims != ?`,
		configuredDims,
	).Scan(&stats.Count); err != nil {
		return stats, fmt.Errorf("kb: count stale embeddings: %w", err)
	}
	if stats.Count == 0 {
		return stats, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT embedding_model FROM kb_chunks
		WHERE embedding IS NOT NULL AND embedding_dims != ? AND embedding_model != ''
		ORDER BY embedding_model`,
		configuredDims,
	)
	if err != nil {
		return stats, fmt.Errorf("kb: list stale embedding models: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return stats, fmt.Errorf("kb: scan stale embedding model: %w", err)
		}
		stats.RecordedModels = append(stats.RecordedModels, model)
	}
	return stats, rows.Err()
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
		doc.Tags = ExtractTagsFromMetadata(doc.Metadata)

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
