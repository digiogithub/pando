package rag

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// InsertChunk inserts a new chunk and optionally its embedding vector.
//
// Pass a nil or empty embedding to store a text-only chunk (participates in FTS
// search but not in vector search).
//
// Returns the auto-assigned chunk ID.
func (s *Store) InsertChunk(ctx context.Context, chunk Chunk, embedding []float32) (int64, error) {
	if len(embedding) > 0 && len(embedding) != s.dim {
		return 0, fmt.Errorf("rag: embedding dim %d != store dim %d", len(embedding), s.dim)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("rag: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if chunk.Metadata == "" {
		chunk.Metadata = "{}"
	}
	now := time.Now().UTC()

	var embBlob []byte
	if len(embedding) > 0 {
		embBlob = serializeFloat32(embedding)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO rag_chunks (collection, source, content, chunk_index, metadata, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		chunk.Collection, chunk.Source, chunk.Content,
		chunk.ChunkIndex, chunk.Metadata, embBlob, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("rag: insert chunk: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("rag: last insert id: %w", err)
	}

	// Keep the FTS5 external-content index in sync.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO rag_fts(rowid, content, source, collection)
		VALUES (?, ?, ?, ?)`,
		id, chunk.Content, chunk.Source, chunk.Collection,
	); err != nil {
		return 0, fmt.Errorf("rag: insert fts: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("rag: commit: %w", err)
	}
	return id, nil
}

// UpdateChunk replaces the text, metadata, and optionally the embedding of an
// existing chunk. Pass nil embedding to leave the stored vector unchanged.
func (s *Store) UpdateChunk(ctx context.Context, id int64, chunk Chunk, embedding []float32) error {
	if len(embedding) > 0 && len(embedding) != s.dim {
		return fmt.Errorf("rag: embedding dim %d != store dim %d", len(embedding), s.dim)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rag: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Read the old text values required to delete the FTS index entry.
	var oldContent, oldSource, oldCollection string
	err = tx.QueryRowContext(ctx,
		`SELECT content, source, collection FROM rag_chunks WHERE id = ?`, id,
	).Scan(&oldContent, &oldSource, &oldCollection)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("rag: chunk %d not found", id)
		}
		return fmt.Errorf("rag: read old chunk: %w", err)
	}

	if chunk.Metadata == "" {
		chunk.Metadata = "{}"
	}
	now := time.Now().UTC()

	if len(embedding) > 0 {
		// Update content and embedding.
		if _, err = tx.ExecContext(ctx, `
			UPDATE rag_chunks
			SET collection = ?, source = ?, content = ?, chunk_index = ?,
			    metadata = ?, embedding = ?, updated_at = ?
			WHERE id = ?`,
			chunk.Collection, chunk.Source, chunk.Content,
			chunk.ChunkIndex, chunk.Metadata, serializeFloat32(embedding), now, id,
		); err != nil {
			return fmt.Errorf("rag: update chunk: %w", err)
		}
	} else {
		// Preserve the existing embedding.
		if _, err = tx.ExecContext(ctx, `
			UPDATE rag_chunks
			SET collection = ?, source = ?, content = ?, chunk_index = ?,
			    metadata = ?, updated_at = ?
			WHERE id = ?`,
			chunk.Collection, chunk.Source, chunk.Content,
			chunk.ChunkIndex, chunk.Metadata, now, id,
		); err != nil {
			return fmt.Errorf("rag: update chunk: %w", err)
		}
	}

	// Delete the old FTS entry (must use the OLD content values).
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO rag_fts(rag_fts, rowid, content, source, collection)
		VALUES ('delete', ?, ?, ?, ?)`,
		id, oldContent, oldSource, oldCollection,
	); err != nil {
		return fmt.Errorf("rag: fts delete old: %w", err)
	}

	// Insert the updated FTS entry.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO rag_fts(rowid, content, source, collection)
		VALUES (?, ?, ?, ?)`,
		id, chunk.Content, chunk.Source, chunk.Collection,
	); err != nil {
		return fmt.Errorf("rag: fts insert new: %w", err)
	}

	return tx.Commit()
}

// DeleteChunk removes a chunk and its FTS entry. It is a no-op when the chunk
// does not exist.
func (s *Store) DeleteChunk(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rag: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var content, source, collection string
	err = tx.QueryRowContext(ctx,
		`SELECT content, source, collection FROM rag_chunks WHERE id = ?`, id,
	).Scan(&content, &source, &collection)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // already gone
		}
		return fmt.Errorf("rag: read chunk for delete: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO rag_fts(rag_fts, rowid, content, source, collection)
		VALUES ('delete', ?, ?, ?, ?)`,
		id, content, source, collection,
	); err != nil {
		return fmt.Errorf("rag: fts delete: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM rag_chunks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("rag: chunk delete: %w", err)
	}

	return tx.Commit()
}

// GetChunk retrieves a chunk by ID. Returns (nil, nil) when not found.
func (s *Store) GetChunk(ctx context.Context, id int64) (*Chunk, error) {
	var c Chunk
	err := s.db.QueryRowContext(ctx, `
		SELECT id, collection, source, content, chunk_index, metadata, created_at, updated_at
		FROM rag_chunks WHERE id = ?`, id,
	).Scan(&c.ID, &c.Collection, &c.Source, &c.Content,
		&c.ChunkIndex, &c.Metadata, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("rag: get chunk: %w", err)
	}
	return &c, nil
}

// GetChunkEmbedding retrieves the stored embedding for a chunk.
// Returns (nil, nil) when the chunk has no embedding.
func (s *Store) GetChunkEmbedding(ctx context.Context, id int64) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT embedding FROM rag_chunks WHERE id = ?`, id,
	).Scan(&blob)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("rag: get embedding: %w", err)
	}
	return deserializeFloat32(blob), nil
}

// ListChunks returns paginated chunks for a collection (all when collection="").
func (s *Store) ListChunks(ctx context.Context, collection string, limit, offset int) ([]Chunk, error) {
	const sel = `
		SELECT id, collection, source, content, chunk_index, metadata, created_at, updated_at
		FROM rag_chunks`

	var (
		rows *sql.Rows
		err  error
	)
	if collection == "" {
		rows, err = s.db.QueryContext(ctx, sel+` ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
	} else {
		rows, err = s.db.QueryContext(ctx,
			sel+` WHERE collection = ? ORDER BY id LIMIT ? OFFSET ?`,
			collection, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("rag: list chunks: %w", err)
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.Collection, &c.Source, &c.Content,
			&c.ChunkIndex, &c.Metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("rag: scan chunk: %w", err)
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// DeleteCollection removes all chunks (and their FTS entries) that belong to
// the given collection.
func (s *Store) DeleteCollection(ctx context.Context, collection string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rag: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	type row struct {
		id      int64
		content string
		source  string
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, content, source FROM rag_chunks WHERE collection = ?`, collection)
	if err != nil {
		return fmt.Errorf("rag: list collection: %w", err)
	}
	var items []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.content, &r.source); err != nil {
			rows.Close()
			return fmt.Errorf("rag: scan: %w", err)
		}
		items = append(items, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range items {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO rag_fts(rag_fts, rowid, content, source, collection)
			VALUES ('delete', ?, ?, ?, ?)`,
			r.id, r.content, r.source, collection,
		); err != nil {
			return fmt.Errorf("rag: fts delete: %w", err)
		}
	}

	if _, err = tx.ExecContext(ctx,
		`DELETE FROM rag_chunks WHERE collection = ?`, collection,
	); err != nil {
		return fmt.Errorf("rag: chunk delete collection: %w", err)
	}

	return tx.Commit()
}

// CountChunks returns the total number of chunks (all when collection="").
func (s *Store) CountChunks(ctx context.Context, collection string) (int64, error) {
	var count int64
	var err error
	if collection == "" {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rag_chunks`).Scan(&count)
	} else {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM rag_chunks WHERE collection = ?`, collection,
		).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("rag: count chunks: %w", err)
	}
	return count, nil
}

// RebuildFTS rebuilds the FTS5 index from the rag_chunks content table.
// Use this to recover from index corruption or after bulk inserts that bypassed
// the normal InsertChunk / UpdateChunk path.
func (s *Store) RebuildFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO rag_fts(rag_fts) VALUES ('rebuild')`); err != nil {
		return fmt.Errorf("rag: fts rebuild: %w", err)
	}
	return nil
}
