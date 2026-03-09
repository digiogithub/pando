package rag

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
)

// SearchVector performs an exact k-NN search by loading all embeddings from the
// collection and computing cosine similarity in Go.
//
// This approach requires no SQLite extension and is suitable for typical RAG
// workloads (up to ~100k chunks). Chunks without embeddings are skipped.
//
// Results are ordered by descending cosine similarity (most similar first).
func (s *Store) SearchVector(ctx context.Context, embedding []float32, opts SearchOptions) ([]SearchResult, error) {
	if len(embedding) != s.dim {
		return nil, fmt.Errorf("rag: query embedding dim %d != store dim %d", len(embedding), s.dim)
	}
	opts = defaultSearchOptions(opts)

	queryNorm := l2norm(embedding)
	if queryNorm == 0 {
		return nil, fmt.Errorf("rag: query embedding is a zero vector")
	}

	// Load id + embedding for all chunks in the collection (or all collections).
	const sel = `SELECT id, collection, source, content, chunk_index, metadata,
	                    created_at, updated_at, embedding
	             FROM rag_chunks
	             WHERE embedding IS NOT NULL`

	var (
		rows *sql.Rows
		err  error
	)
	if opts.Collection == "" {
		rows, err = s.db.QueryContext(ctx, sel)
	} else {
		rows, err = s.db.QueryContext(ctx, sel+` AND collection = ?`, opts.Collection)
	}
	if err != nil {
		return nil, fmt.Errorf("rag: load embeddings: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		chunk Chunk
		score float64
	}
	var candidates []candidate

	for rows.Next() {
		var c Chunk
		var blob []byte
		if err := rows.Scan(
			&c.ID, &c.Collection, &c.Source, &c.Content,
			&c.ChunkIndex, &c.Metadata, &c.CreatedAt, &c.UpdatedAt,
			&blob,
		); err != nil {
			return nil, fmt.Errorf("rag: scan embedding row: %w", err)
		}
		vec := deserializeFloat32(blob)
		if len(vec) != s.dim {
			continue // skip malformed embeddings
		}
		score := cosine(embedding, queryNorm, vec)
		candidates = append(candidates, candidate{c, score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by descending similarity.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	results := make([]SearchResult, 0, opts.TopK)
	for i, cand := range candidates {
		if i >= opts.TopK {
			break
		}
		if opts.MinScore > 0 && cand.score < opts.MinScore {
			break // sorted descending; no later entry will pass
		}
		results = append(results, SearchResult{
			Chunk:    cand.chunk,
			Distance: 1.0 - cand.score, // cosine distance
			Score:    cand.score,
			Rank:     i + 1,
		})
	}
	return results, nil
}

// SearchFTS performs a full-text search using SQLite FTS5 (BM25 ranking).
//
// query follows FTS5 syntax: plain terms, phrase queries ("…"), prefix queries
// (term*), column filters (content:term), boolean operators (AND / OR / NOT).
//
// Results are ordered by descending relevance with scores normalised to [0, 1].
func (s *Store) SearchFTS(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	opts = defaultSearchOptions(opts)

	// bm25() returns a negative value; -bm25() is a positive relevance weight.
	var q string
	var args []any

	if opts.Collection == "" {
		q = `
			SELECT c.id, c.collection, c.source, c.content,
			       c.chunk_index, c.metadata, c.created_at, c.updated_at,
			       -bm25(rag_fts) AS score
			FROM rag_fts
			JOIN rag_chunks c ON c.id = rag_fts.rowid
			WHERE rag_fts MATCH ?
			ORDER BY score DESC
			LIMIT ?`
		args = []any{query, opts.TopK}
	} else {
		q = `
			SELECT c.id, c.collection, c.source, c.content,
			       c.chunk_index, c.metadata, c.created_at, c.updated_at,
			       -bm25(rag_fts) AS score
			FROM rag_fts
			JOIN rag_chunks c ON c.id = rag_fts.rowid
			WHERE rag_fts MATCH ? AND c.collection = ?
			ORDER BY score DESC
			LIMIT ?`
		args = []any{query, opts.Collection, opts.TopK}
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("rag: fts search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var rawScore float64
		if err := rows.Scan(
			&r.ID, &r.Collection, &r.Source, &r.Content,
			&r.ChunkIndex, &r.Metadata, &r.CreatedAt, &r.UpdatedAt,
			&rawScore,
		); err != nil {
			return nil, fmt.Errorf("rag: scan fts result: %w", err)
		}
		r.Score = rawScore
		r.Rank = len(results) + 1
		if opts.MinScore > 0 && r.Score < opts.MinScore {
			continue
		}
		results = append(results, r)
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

// SearchHybrid combines vector and full-text search using Reciprocal Rank
// Fusion (RRF). Both searches run concurrently.
//
// Pass a nil embedding to skip vector search (equivalent to SearchFTS).
//
// RRF formula: score(d) = Σ  1 / (rrfK + rank(d, list))   where rrfK = 60.
func (s *Store) SearchHybrid(ctx context.Context, query string, embedding []float32, opts SearchOptions) ([]SearchResult, error) {
	if len(embedding) > 0 && len(embedding) != s.dim {
		return nil, fmt.Errorf("rag: query embedding dim %d != store dim %d", len(embedding), s.dim)
	}
	opts = defaultSearchOptions(opts)

	// Fetch more candidates from each sub-search to improve fusion quality.
	subOpts := opts
	subOpts.TopK = opts.TopK * 3
	subOpts.MinScore = 0 // filter after fusion

	type result struct {
		items []SearchResult
		err   error
	}
	vecCh := make(chan result, 1)
	ftsCh := make(chan result, 1)

	go func() {
		if len(embedding) == 0 {
			vecCh <- result{}
			return
		}
		items, err := s.SearchVector(ctx, embedding, subOpts)
		vecCh <- result{items, err}
	}()

	go func() {
		items, err := s.SearchFTS(ctx, query, subOpts)
		ftsCh <- result{items, err}
	}()

	vec := <-vecCh
	fts := <-ftsCh

	if vec.err != nil {
		return nil, fmt.Errorf("rag: hybrid vector: %w", vec.err)
	}
	if fts.err != nil {
		return nil, fmt.Errorf("rag: hybrid fts: %w", fts.err)
	}

	return rrfFuse(vec.items, fts.items, opts), nil
}

// rrfFuse merges two ranked result lists using Reciprocal Rank Fusion.
func rrfFuse(vecResults, ftsResults []SearchResult, opts SearchOptions) []SearchResult {
	const rrfK = 60.0

	type entry struct {
		chunk    Chunk
		distance float64
		rrf      float64
	}
	byID := make(map[int64]*entry)

	for rank, r := range vecResults {
		e := &entry{chunk: r.Chunk, distance: r.Distance}
		e.rrf += 1.0 / (rrfK + float64(rank+1))
		byID[r.ID] = e
	}
	for rank, r := range ftsResults {
		if e, ok := byID[r.ID]; ok {
			e.rrf += 1.0 / (rrfK + float64(rank+1))
		} else {
			byID[r.ID] = &entry{
				chunk: r.Chunk,
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

	results := make([]SearchResult, 0, opts.TopK)
	for i, e := range fused {
		if i >= opts.TopK {
			break
		}
		r := SearchResult{
			Chunk:    e.chunk,
			Distance: e.distance,
			Score:    e.rrf,
			Rank:     i + 1,
		}
		if opts.MinScore > 0 && r.Score < opts.MinScore {
			continue
		}
		results = append(results, r)
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
