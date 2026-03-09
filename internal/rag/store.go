package rag

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

// Store manages RAG chunks, embeddings, and full-text search on a SQLite database.
//
// Vector embeddings are stored as little-endian float32 BLOBs in the rag_chunks
// table and similarity search is performed in Go – no SQLite extension is required.
// Full-text search uses SQLite's built-in FTS5 engine.
//
// The Store does NOT own the *sql.DB – callers are responsible for opening and
// closing the database via internal/db.Connect().
//
// Usage:
//
//	db, err := db.Connect()
//	store := rag.New(db, rag.StoreOptions{EmbeddingDim: 1536})
//	if err := store.Init(ctx); err != nil { … }
type Store struct {
	db  *sql.DB
	dim int
}

// New creates a Store backed by db. Call Init before any other method.
func New(db *sql.DB, opts StoreOptions) *Store {
	dim := opts.EmbeddingDim
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	return &Store{db: db, dim: dim}
}

// Dim returns the configured embedding dimension.
func (s *Store) Dim() int { return s.dim }

// Init validates the configured embedding dimension against the stored one and
// persists it on first use. It is idempotent.
func (s *Store) Init(ctx context.Context) error {
	// rag_meta is created by migration; belt-and-suspenders guard.
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS rag_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	); err != nil {
		return fmt.Errorf("rag: ensure rag_meta: %w", err)
	}

	var storedDim int
	err := s.db.QueryRowContext(ctx,
		`SELECT CAST(value AS INTEGER) FROM rag_meta WHERE key = 'embedding_dim'`,
	).Scan(&storedDim)

	switch {
	case err == sql.ErrNoRows:
		// First initialisation: record the dimension.
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO rag_meta(key, value) VALUES ('embedding_dim', ?)`,
			fmt.Sprintf("%d", s.dim),
		); err != nil {
			return fmt.Errorf("rag: persist embedding_dim: %w", err)
		}

	case err != nil:
		return fmt.Errorf("rag: read embedding_dim: %w", err)

	case storedDim != s.dim:
		return fmt.Errorf(
			"rag: dimension mismatch: store configured with %d but database has %d; "+
				"use rag.New(db, rag.StoreOptions{EmbeddingDim: %d}) to match",
			s.dim, storedDim, storedDim,
		)
	}

	return nil
}

// serializeFloat32 encodes a []float32 as a little-endian byte blob.
// This format is compatible with sqlite-vec should the extension be integrated later.
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
