package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// MemoryUpsertOptions configures a memory document upsert operation.
type MemoryUpsertOptions struct {
	FilePath       string
	Key            string
	Scope          string
	Content        string
	Tags           []string
	Metadata       map[string]interface{}
	Importance     float64
	Source         string
	DefaultTTLDays int // 0 → 180
}

// DocumentRef is a minimal reference to a KB document (ID + path).
type DocumentRef struct {
	ID       int64
	FilePath string
}

// MemoryResult wraps a Document with its chunk content and relevance score.
type MemoryResult struct {
	Document     Document
	ChunkContent string
	Score        float64
}

// UpsertMemory stores or updates a memory document, keyed by opts.Key when provided.
// Returns created=true when a new document was inserted, false when an existing one was updated.
//
// It delegates to AddDocument/UpdateDocument on some paths, so the write
// observer is suppressed for the nested call: the memory event published here
// carries the scope and key those events cannot know, and one write must be
// reported once.
func (s *KBStore) UpsertMemory(ctx context.Context, opts MemoryUpsertOptions) (created bool, err error) {
	observing := s.observer() != nil && !observerSuppressed(ctx)
	inner := ctx
	if observing {
		inner = withSuppressedObserver(ctx)
	}

	created, err = s.upsertMemory(inner, opts)
	if err != nil || !observing {
		return created, err
	}

	op := WriteUpdated
	if created {
		op = WriteCreated
	}
	filePath := opts.FilePath
	if opts.Key != "" {
		// An upsert by key lands on the path the memory already had, which is
		// not necessarily the one the caller passed.
		if doc, lookupErr := s.GetMemoryByKey(ctx, opts.Key); lookupErr == nil && doc != nil {
			filePath = doc.FilePath
		}
	}
	s.publishWrite(ctx, WriteEvent{
		Kind:     WriteKindMemory,
		Op:       op,
		FilePath: filePath,
		Key:      opts.Key,
		Scope:    opts.Scope,
		Content:  StripFrontMatter(opts.Content),
		Tags:     opts.Tags,
		Metadata: opts.Metadata,
	})
	return created, nil
}

func (s *KBStore) upsertMemory(ctx context.Context, opts MemoryUpsertOptions) (created bool, err error) {
	if opts.DefaultTTLDays <= 0 {
		opts.DefaultTTLDays = 180
	}
	if strings.TrimSpace(opts.Source) == "" {
		opts.Source = "memory"
	}

	// Ensure "memory" tag is always present.
	hasMemory := false
	for _, t := range opts.Tags {
		if t == "memory" {
			hasMemory = true
			break
		}
	}
	if !hasMemory {
		opts.Tags = append(opts.Tags, "memory")
	}

	opts.Metadata = InjectTagsIntoMetadata(opts.Metadata, opts.Tags)

	bodyContent := StripFrontMatter(opts.Content)

	if opts.Key != "" {
		return s.upsertMemoryByKey(ctx, opts, bodyContent)
	}

	// Keyless path: behave like AddDocument/UpdateDocument.
	existing, err := s.GetDocument(ctx, opts.FilePath)
	if err != nil {
		return false, fmt.Errorf("kb: upsert memory: %w", err)
	}

	if existing != nil {
		existingFM := FrontMatter{
			CreatedAt:  existing.CreatedAt,
			UpdatedAt:  existing.UpdatedAt,
			Tags:       existing.Tags,
			Key:        existing.MemoryKey,
			Scope:      existing.MemoryScope,
			Source:     existing.Source,
			Hits:       existing.Hits,
			Importance: existing.Importance,
			ExpiresAt:  existing.ExpiresAt,
		}
		incomingFM := FrontMatter{
			Tags:       opts.Tags,
			Scope:      opts.Scope,
			Source:     opts.Source,
			Importance: opts.Importance,
		}
		mergedFM := MergeFrontMatter(existingFM, incomingFM)
		if err := s.UpdateDocument(ctx, opts.FilePath, bodyContent, opts.Metadata); err != nil {
			return false, fmt.Errorf("kb: upsert memory update: %w", err)
		}
		mirrorContent := SerializeFrontMatter(mergedFM, bodyContent)
		if err := s.WriteDocumentToFilesystem(opts.FilePath, mirrorContent); err != nil {
			return false, fmt.Errorf("kb: upsert memory mirror: %w", err)
		}
		return false, nil
	}

	memOpts := &MemoryOptions{
		Key:        opts.Key,
		Scope:      opts.Scope,
		Source:     opts.Source,
		Importance: opts.Importance,
		TTLDays:    opts.DefaultTTLDays,
	}
	newFM := NewFrontMatter(opts.Tags, memOpts)
	if err := s.AddDocument(ctx, opts.FilePath, bodyContent, opts.Metadata); err != nil {
		return false, fmt.Errorf("kb: upsert memory add: %w", err)
	}
	mirrorContent := SerializeFrontMatter(newFM, bodyContent)
	if err := s.WriteDocumentToFilesystem(opts.FilePath, mirrorContent); err != nil {
		return false, fmt.Errorf("kb: upsert memory mirror: %w", err)
	}
	return true, nil
}

func (s *KBStore) upsertMemoryByKey(ctx context.Context, opts MemoryUpsertOptions, bodyContent string) (bool, error) {
	if s.proxy != nil {
		// Via proxy: check existence first, then route through proxy's Add/Update.
		existing, err := s.GetMemoryByKey(ctx, opts.Key)
		if err != nil {
			return false, err
		}

		if existing != nil {
			existingFM := FrontMatter{
				CreatedAt:  existing.CreatedAt,
				UpdatedAt:  existing.UpdatedAt,
				Tags:       existing.Tags,
				Key:        existing.MemoryKey,
				Scope:      existing.MemoryScope,
				Source:     existing.Source,
				Hits:       existing.Hits,
				Importance: existing.Importance,
				ExpiresAt:  existing.ExpiresAt,
			}
			incomingFM := FrontMatter{
				Tags:       opts.Tags,
				Scope:      opts.Scope,
				Source:     opts.Source,
				Importance: opts.Importance,
			}
			mergedFM := MergeFrontMatter(existingFM, incomingFM)
			if err := s.UpdateDocument(ctx, existing.FilePath, bodyContent, opts.Metadata); err != nil {
				return false, fmt.Errorf("kb: upsert memory key update: %w", err)
			}
			mirrorContent := SerializeFrontMatter(mergedFM, bodyContent)
			if err := s.WriteDocumentToFilesystem(existing.FilePath, mirrorContent); err != nil {
				return false, fmt.Errorf("kb: upsert memory key mirror: %w", err)
			}
			return false, nil
		}

		memOpts := &MemoryOptions{
			Key:        opts.Key,
			Scope:      opts.Scope,
			Source:     opts.Source,
			Importance: opts.Importance,
			TTLDays:    opts.DefaultTTLDays,
		}
		newFM := NewFrontMatter(opts.Tags, memOpts)
		if err := s.AddDocument(ctx, opts.FilePath, bodyContent, opts.Metadata); err != nil {
			return false, fmt.Errorf("kb: upsert memory key add: %w", err)
		}
		mirrorContent := SerializeFrontMatter(newFM, bodyContent)
		if err := s.WriteDocumentToFilesystem(opts.FilePath, mirrorContent); err != nil {
			return false, fmt.Errorf("kb: upsert memory key mirror: %w", err)
		}
		return true, nil
	}

	// Direct DB path: use INSERT ... ON CONFLICT for atomicity.
	metaJSON := "{}"
	if len(opts.Metadata) > 0 {
		b, err := json.Marshal(opts.Metadata)
		if err != nil {
			return false, fmt.Errorf("kb: marshal metadata: %w", err)
		}
		metaJSON = string(b)
	}

	now := time.Now().UTC()
	ttlStr := fmt.Sprintf("%d", opts.DefaultTTLDays)

	// Determine insert vs update up front: SQLite's RowsAffected() returns 1 for
	// both branches of an INSERT ... ON CONFLICT DO UPDATE, so it cannot be used
	// to tell them apart.
	var existingID int64
	existedErr := s.db.QueryRowContext(ctx,
		`SELECT id FROM kb_documents WHERE memory_key = ?`, opts.Key).Scan(&existingID)
	if existedErr != nil && existedErr != sql.ErrNoRows {
		return false, fmt.Errorf("kb: upsert memory key existence check: %w", existedErr)
	}
	existed := existedErr == nil

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kb_documents (file_path, content, metadata, memory_key, memory_scope,
		    importance, source, expires_at, hits, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now', '+`+ttlStr+` days'), 0, ?, ?)
		ON CONFLICT(memory_key) WHERE memory_key != '' DO UPDATE SET
		    content    = excluded.content,
		    metadata   = excluded.metadata,
		    updated_at = excluded.updated_at,
		    hits       = hits + 1,
		    expires_at = MAX(expires_at, datetime('now', '+`+ttlStr+` days')),
		    outdated   = 0
		WHERE memory_key IS NOT NULL`,
		opts.FilePath, bodyContent, metaJSON, opts.Key,
		opts.Scope, opts.Importance,
		opts.Source, now, now,
	)
	if err != nil {
		return false, fmt.Errorf("kb: upsert memory key db: %w", err)
	}

	wasCreated := !existed

	// Re-read the doc to get current state for front matter (hits, expires_at etc).
	doc, err := s.GetMemoryByKey(ctx, opts.Key)
	if err != nil || doc == nil {
		// Non-fatal: mirror might be skipped but DB is consistent.
		return wasCreated, nil
	}

	// This path writes kb_documents directly (INSERT ... ON CONFLICT) instead of
	// going through AddDocument, so links must be refreshed explicitly. A failure
	// here costs the document its graph edges, not its content — don't fail the upsert.
	if _, linkErr := s.indexDocumentLinks(ctx, s.db, doc.ID, doc.FilePath, bodyContent); linkErr != nil {
		logging.Warn("kb: memory upsert link indexing failed", "file_path", doc.FilePath, "error", linkErr)
	}

	fm := NewFrontMatter(opts.Tags, &MemoryOptions{
		Key:        opts.Key,
		Scope:      opts.Scope,
		Source:     opts.Source,
		Importance: opts.Importance,
		TTLDays:    opts.DefaultTTLDays,
	})
	if !wasCreated {
		// Preserve original creation time on update.
		fm.CreatedAt = doc.CreatedAt
		fm.Hits = doc.Hits
		fm.ExpiresAt = doc.ExpiresAt
	}
	mirrorContent := SerializeFrontMatter(fm, bodyContent)
	_ = s.WriteDocumentToFilesystem(doc.FilePath, mirrorContent)

	return wasCreated, nil
}

// GetMemoryByKey retrieves a document by its memory_key.
// Returns nil, nil when no document with the key exists.
func (s *KBStore) GetMemoryByKey(ctx context.Context, key string) (*Document, error) {
	var doc Document
	var metaJSON string
	var memoryKey, memoryScope, source sql.NullString
	var outdated sql.NullInt64
	var expiresAt sql.NullTime
	var hits sql.NullInt64
	var importance sql.NullFloat64

	err := s.db.QueryRowContext(ctx, `
		SELECT id, file_path, content, metadata, created_at, updated_at,
		       COALESCE(memory_key, ''), COALESCE(memory_scope, ''), COALESCE(outdated, 0),
		       expires_at, COALESCE(hits, 0), COALESCE(importance, 0.5), COALESCE(source, '')
		FROM kb_documents
		WHERE memory_key = ?`,
		key,
	).Scan(&doc.ID, &doc.FilePath, &doc.Content, &metaJSON, &doc.CreatedAt, &doc.UpdatedAt,
		&memoryKey, &memoryScope, &outdated, &expiresAt, &hits, &importance, &source)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("kb: get memory by key: %w", err)
	}

	doc.MemoryKey = memoryKey.String
	doc.MemoryScope = memoryScope.String
	doc.Source = source.String
	doc.Outdated = outdated.Int64 != 0
	if expiresAt.Valid {
		t := expiresAt.Time
		doc.ExpiresAt = &t
	}
	doc.Hits = int(hits.Int64)
	doc.Importance = importance.Float64

	if metaJSON != "" && metaJSON != "{}" {
		if err := json.Unmarshal([]byte(metaJSON), &doc.Metadata); err != nil {
			return nil, fmt.Errorf("kb: unmarshal metadata: %w", err)
		}
	}
	doc.Tags = ExtractTagsFromMetadata(doc.Metadata)

	return &doc, nil
}

// IncrementMemoryHits increments the hit counter for a document and extends its TTL.
func (s *KBStore) IncrementMemoryHits(ctx context.Context, docID int64, defaultTTLDays int) error {
	if defaultTTLDays <= 0 {
		defaultTTLDays = 180
	}
	now := time.Now().UTC()
	ttlStr := fmt.Sprintf("%d", defaultTTLDays)
	_, err := s.db.ExecContext(ctx, `
		UPDATE kb_documents
		SET hits = hits + 1,
		    expires_at = MAX(COALESCE(expires_at, datetime('now')), datetime('now', '+`+ttlStr+` days')),
		    updated_at = ?
		WHERE id = ?`,
		now, docID,
	)
	if err != nil {
		return fmt.Errorf("kb: increment memory hits: %w", err)
	}
	return nil
}

// MarkDocumentOutdated marks a document as outdated and updates its filesystem mirror.
func (s *KBStore) MarkDocumentOutdated(ctx context.Context, filePath string) error {
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE kb_documents SET outdated = 1, updated_at = ?
		WHERE file_path = ?`,
		now, filePath,
	); err != nil {
		return fmt.Errorf("kb: mark outdated db: %w", err)
	}

	doc, err := s.GetDocument(ctx, filePath)
	if err != nil || doc == nil {
		return err
	}

	fm, body, parseErr := ParseFrontMatter(doc.Content)
	if parseErr != nil {
		// If we can't parse, synthesize a minimal front matter.
		fm = FrontMatter{
			CreatedAt: doc.CreatedAt,
		}
		body = doc.Content
	}
	fm.Outdated = true
	fm.UpdatedAt = now

	mirrorContent := SerializeFrontMatter(fm, body)
	_ = s.WriteDocumentToFilesystem(filePath, mirrorContent)

	return nil
}

// GetExpiredDocuments returns all non-outdated documents whose expires_at is in the past.
func (s *KBStore) GetExpiredDocuments(ctx context.Context) ([]DocumentRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, file_path FROM kb_documents
		WHERE expires_at IS NOT NULL
		  AND expires_at < datetime('now')
		  AND outdated = 0`)
	if err != nil {
		return nil, fmt.Errorf("kb: get expired documents: %w", err)
	}
	defer rows.Close()

	var refs []DocumentRef
	for rows.Next() {
		var ref DocumentRef
		if err := rows.Scan(&ref.ID, &ref.FilePath); err != nil {
			return nil, fmt.Errorf("kb: scan expired document: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// GetMemoriesForInjection retrieves memory documents ranked for context injection.
// It combines semantic search results with pinned-scope documents, deduplicates,
// scores, and trims to fit within the maxChars budget.
func (s *KBStore) GetMemoriesForInjection(ctx context.Context, query string, limit int, maxChars int, pinnedScopes []string) ([]MemoryResult, error) {
	if limit <= 0 {
		limit = 5
	}

	var searchResults []MemoryResult
	if query != "" {
		hits, err := s.SearchDocumentsWithOptions(ctx, query, limit*2, SearchOptions{Tags: []string{"memory"}})
		if err != nil {
			return nil, fmt.Errorf("kb: memory injection search: %w", err)
		}
		now := time.Now().UTC()
		for _, r := range hits {
			ageDays := now.Sub(r.Document.UpdatedAt).Hours() / 24.0
			hitNorm := float64(r.Document.Hits) / 100.0
			if hitNorm > 1.0 {
				hitNorm = 1.0
			}
			score := 0.6*r.Score + 0.3*(1.0/(ageDays+1.0)) + 0.1*hitNorm
			searchResults = append(searchResults, MemoryResult{
				Document:     r.Document,
				ChunkContent: r.ChunkContent,
				Score:        score,
			})
		}
	}

	// Load pinned-scope documents.
	var pinnedResults []MemoryResult
	if len(pinnedScopes) > 0 {
		placeholders := strings.Repeat("?,", len(pinnedScopes))
		placeholders = placeholders[:len(placeholders)-1]

		args := make([]interface{}, len(pinnedScopes)+1)
		for i, scope := range pinnedScopes {
			args[i] = scope
		}
		args[len(pinnedScopes)] = limit * 2

		rows, err := s.db.QueryContext(ctx, `
			SELECT id, file_path, content, metadata, created_at, updated_at,
			       COALESCE(memory_key, ''), COALESCE(memory_scope, ''), COALESCE(hits, 0),
			       COALESCE(importance, 0.5), expires_at, COALESCE(outdated, 0), COALESCE(source, '')
			FROM kb_documents
			WHERE memory_scope IN (`+placeholders+`) AND outdated = 0
			ORDER BY importance DESC, hits DESC
			LIMIT ?`, args...)
		if err != nil {
			return nil, fmt.Errorf("kb: memory injection pinned query: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var doc Document
			var metaJSON string
			var memoryKey, memoryScope, source sql.NullString
			var expiresAt sql.NullTime
			var outdated sql.NullInt64
			if err := rows.Scan(
				&doc.ID, &doc.FilePath, &doc.Content, &metaJSON,
				&doc.CreatedAt, &doc.UpdatedAt,
				&memoryKey, &memoryScope, &doc.Hits, &doc.Importance,
				&expiresAt, &outdated, &source,
			); err != nil {
				return nil, fmt.Errorf("kb: scan pinned memory: %w", err)
			}
			doc.MemoryKey = memoryKey.String
			doc.MemoryScope = memoryScope.String
			doc.Source = source.String
			doc.Outdated = outdated.Int64 != 0
			if expiresAt.Valid {
				t := expiresAt.Time
				doc.ExpiresAt = &t
			}
			if metaJSON != "" && metaJSON != "{}" {
				_ = json.Unmarshal([]byte(metaJSON), &doc.Metadata)
			}
			doc.Tags = ExtractTagsFromMetadata(doc.Metadata)
			pinnedResults = append(pinnedResults, MemoryResult{Document: doc, Score: 1.0})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("kb: pinned memory rows: %w", err)
		}
	}

	// Merge and deduplicate by file_path.
	seen := make(map[string]bool)
	merged := make([]MemoryResult, 0, len(pinnedResults)+len(searchResults))
	for _, r := range pinnedResults {
		if !seen[r.Document.FilePath] {
			seen[r.Document.FilePath] = true
			merged = append(merged, r)
		}
	}
	for _, r := range searchResults {
		if !seen[r.Document.FilePath] {
			seen[r.Document.FilePath] = true
			merged = append(merged, r)
		}
	}

	// Sort descending by score.
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	// Trim to limit.
	if len(merged) > limit {
		merged = merged[:limit]
	}

	// Enforce character budget when maxChars > 0.
	if maxChars > 0 {
		total := 0
		cutoff := len(merged)
		for i, r := range merged {
			total += len(r.Document.Content)
			if total > maxChars {
				cutoff = i
				break
			}
		}
		merged = merged[:cutoff]
	}

	return merged, nil
}

// memory_scope and source are NOT NULL DEFAULT '' (20260611000001_add_kb_memory.sql),
// so empty values are written as '' — binding NULL made a scope-less keyed upsert
// (kb_add_document with tag "memory" and a key, but no scope) fail the NOT NULL
// constraint.
