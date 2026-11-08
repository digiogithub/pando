package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SearchMode selects the legs a Search runs.
type SearchMode int

const (
	// ModeHybrid fuses vector and full-text results (RRF). Without an
	// embedder it runs full-text only.
	ModeHybrid SearchMode = iota
	// ModeFTS runs full-text search only (chunk text and front-matter fields).
	ModeFTS
	// ModeVector runs vector search only; it needs an embedder.
	ModeVector
)

// AnyStatus in Query.Statuses disables the status filter.
const AnyStatus = "*"

// Query filters a Search. Every filter is applied in SQL, before ranking, so
// a filter never makes a search return fewer hits than exist.
type Query struct {
	// Limit is the maximum number of hits (default 10).
	Limit int
	// PathPrefixes keeps documents under any of these path prefixes.
	PathPrefixes []string
	// ExcludePrefixes drops documents under any of these path prefixes.
	ExcludePrefixes []string
	// Scopes keeps documents whose scope is one of these or nested under one
	// ("project/acme" matches "project/acme" and "project/acme/x", not
	// "project/acme2").
	Scopes []string
	// Kinds keeps documents of these kinds.
	Kinds []string
	// Statuses keeps documents with these statuses. Empty means
	// []string{StatusActive}; include AnyStatus to search every status.
	Statuses []string
	// Tags keeps documents carrying any of these tags (case-insensitive).
	Tags []string
	// Authors keeps documents by any of these authors.
	Authors []string
	// ExcludeOutdated drops documents marked outdated.
	ExcludeOutdated bool
	// Mode selects the search legs (default ModeHybrid).
	Mode SearchMode
}

// Hit is one ranked chunk.
type Hit struct {
	// Project is never set by Search. A caller fusing hits from several
	// stores sets it before Fuse so equal paths in different projects stay
	// distinct.
	Project   string
	Path      string
	Chunk     string
	StartLine int
	EndLine   int
	// Score is the fused RRF score; higher is better.
	Score float64
	// Rank is the 1-based position in the result list.
	Rank    int
	Kind    string
	Status  string
	Scope   string
	Author  string
	Rev     string
	GitBlob string
	Meta    map[string]interface{}
}

// Stats reports counters from a Search that are not carried by the hits.
type Stats struct {
	// VectorCandidates and FTSCandidates are the sizes of the lists fused.
	VectorCandidates int
	FTSCandidates    int
	// SkippedForDimensionMismatch counts chunks whose embedding dimension
	// differs from the query's; SkippedForModelMismatch counts chunks
	// embedded by another model than the store's. Both mean the corpus needs
	// re-embedding (a Put or ApplyChanges of the document does it).
	SkippedForDimensionMismatch int
	SkippedForModelMismatch     int
}

// rrfK is the Reciprocal Rank Fusion constant used by Search and Fuse.
const rrfK = 60.0

// apiCandidate is a chunk ranked by one search leg.
type apiCandidate struct {
	chunkID, docID int64
	text           string
	start, end     int
	score          float64
}

// queryFilter turns q into a WHERE fragment over kb_documents d.
func queryFilter(q Query) sqlFilter {
	var f sqlFilter
	or := func(n int, clause string) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = clause
		}
		return "(" + strings.Join(parts, " OR ") + ")"
	}
	in := func(col string, vals []string) {
		args := make([]interface{}, len(vals))
		for i, v := range vals {
			args[i] = v
		}
		f.add(col+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(vals)), ",")+")", args...)
	}

	if len(q.PathPrefixes) > 0 {
		args := make([]interface{}, len(q.PathPrefixes))
		for i, p := range q.PathPrefixes {
			args[i] = escapeLikePrefix(p)
		}
		f.add(or(len(args), `d.file_path LIKE ? || '%' ESCAPE '\'`), args...)
	}
	for _, p := range q.ExcludePrefixes {
		f.add(`d.file_path NOT LIKE ? || '%' ESCAPE '\'`, escapeLikePrefix(p))
	}
	if len(q.Scopes) > 0 {
		var args []interface{}
		for _, sc := range q.Scopes {
			sc = strings.TrimSuffix(sc, "/")
			args = append(args, sc, escapeLikePrefix(sc))
		}
		f.add(or(len(q.Scopes), `(d.memory_scope = ? OR d.memory_scope LIKE ? || '/%' ESCAPE '\')`), args...)
	}
	if len(q.Kinds) > 0 {
		in("d.kind", q.Kinds)
	}
	statuses := q.Statuses
	if len(statuses) == 0 {
		statuses = []string{StatusActive}
	}
	anyStatus := false
	for _, st := range statuses {
		anyStatus = anyStatus || st == AnyStatus
	}
	if !anyStatus {
		in("d.status", statuses)
	}
	if len(q.Authors) > 0 {
		in("d.author", q.Authors)
	}
	if len(q.Tags) > 0 {
		args := make([]interface{}, len(q.Tags))
		for i, t := range q.Tags {
			args[i] = strings.ToLower(strings.TrimSpace(t))
		}
		f.add(`EXISTS (SELECT 1 FROM json_each(d.metadata, '$.tags') t WHERE lower(t.value) IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`))`, args...)
	}
	if q.ExcludeOutdated {
		f.add(`d.outdated = 0`)
	}
	return f
}

// Search ranks chunks matching text, with every filter of q applied in SQL.
// In hybrid mode the vector list, the chunk full-text list and the
// front-matter fields list (title, labels, aliases; a match there ranks the
// document's first chunk) are fused with RRF.
func (s *KBStore) Search(ctx context.Context, text string, q Query) ([]Hit, Stats, error) {
	var stats Stats
	if strings.TrimSpace(text) == "" {
		return nil, stats, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	sub := max(limit*4, 40)
	filter := queryFilter(q)

	useVec := q.Mode != ModeFTS && s.embedder != nil
	useFTS := q.Mode != ModeVector
	if q.Mode == ModeVector && s.embedder == nil {
		return nil, stats, errors.New("kb: vector search needs an embedder")
	}

	var lists [][]apiCandidate
	if useVec {
		emb, err := s.embedder.EmbedQuery(ctx, text)
		if err != nil {
			return nil, stats, fmt.Errorf("kb: embed query: %w", err)
		}
		vec, err := s.apiSearchVector(ctx, emb, sub, filter, &stats)
		if err != nil {
			return nil, stats, err
		}
		stats.VectorCandidates = len(vec)
		lists = append(lists, vec)
	}
	if useFTS {
		chunks, err := s.apiSearchFTS(ctx, text, sub, filter)
		if err != nil {
			return nil, stats, err
		}
		fields, err := s.apiSearchFields(ctx, text, sub, filter)
		if err != nil {
			return nil, stats, err
		}
		stats.FTSCandidates = len(chunks) + len(fields)
		lists = append(lists, chunks, fields)
	}

	fused := fuseCandidates(limit, lists...)
	hits, err := s.hitsFor(ctx, fused)
	return hits, stats, err
}

func (s *KBStore) apiSearchVector(ctx context.Context, emb []float32, limit int, f sqlFilter, stats *Stats) ([]apiCandidate, error) {
	qnorm := l2norm(emb)
	if qnorm == 0 {
		return nil, errors.New("kb: query embedding is zero vector")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.document_id, c.content, c.start_line, c.end_line, c.embedding, c.embedding_model
		FROM kb_chunks c JOIN kb_documents d ON d.id = c.document_id
		WHERE c.embedding IS NOT NULL`+f.clause, f.args...)
	if err != nil {
		return nil, fmt.Errorf("kb: vector search: %w", err)
	}
	defer rows.Close()
	var out []apiCandidate
	for rows.Next() {
		var c apiCandidate
		var blob []byte
		var model string
		if err := rows.Scan(&c.chunkID, &c.docID, &c.text, &c.start, &c.end, &blob, &model); err != nil {
			return nil, fmt.Errorf("kb: vector search: %w", err)
		}
		v := deserializeFloat32(blob)
		if len(v) != len(emb) {
			stats.SkippedForDimensionMismatch++
			continue
		}
		if model != "" && s.embeddingModel != "" && model != s.embeddingModel {
			stats.SkippedForModelMismatch++
			continue
		}
		c.score = cosine(emb, qnorm, v)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].chunkID < out[j].chunkID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *KBStore) apiSearchFTS(ctx context.Context, text string, limit int, f sqlFilter) ([]apiCandidate, error) {
	match := sanitizeFTSQuery(text)
	if match == "" {
		return nil, nil
	}
	args := append([]interface{}{match}, f.args...)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.document_id, c.content, c.start_line, c.end_line, -bm25(kb_fts) AS score
		FROM kb_fts
		JOIN kb_chunks c ON c.id = kb_fts.rowid
		JOIN kb_documents d ON d.id = c.document_id
		WHERE kb_fts MATCH ?`+f.clause+`
		ORDER BY score DESC, c.id
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("kb: fts search: %w", err)
	}
	defer rows.Close()
	var out []apiCandidate
	for rows.Next() {
		var c apiCandidate
		if err := rows.Scan(&c.chunkID, &c.docID, &c.text, &c.start, &c.end, &c.score); err != nil {
			return nil, fmt.Errorf("kb: fts search: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// apiSearchFields matches the front-matter fields index and returns, for each
// matching document, its first chunk.
func (s *KBStore) apiSearchFields(ctx context.Context, text string, limit int, f sqlFilter) ([]apiCandidate, error) {
	match := sanitizeFTSQuery(text)
	if match == "" {
		return nil, nil
	}
	args := append([]interface{}{match}, f.args...)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.document_id, c.content, c.start_line, c.end_line, -bm25(kb_doc_fields_fts, 3.0, 2.0, 2.0) AS score
		FROM kb_doc_fields_fts ff
		JOIN kb_documents d ON d.id = ff.rowid
		JOIN kb_chunks c ON c.document_id = d.id
		  AND c.chunk_index = (SELECT min(chunk_index) FROM kb_chunks WHERE document_id = d.id)
		WHERE kb_doc_fields_fts MATCH ?`+f.clause+`
		ORDER BY score DESC, c.id
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("kb: fields search: %w", err)
	}
	defer rows.Close()
	var out []apiCandidate
	for rows.Next() {
		var c apiCandidate
		if err := rows.Scan(&c.chunkID, &c.docID, &c.text, &c.start, &c.end, &c.score); err != nil {
			return nil, fmt.Errorf("kb: fields search: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// fuseCandidates merges ranked lists by chunk with RRF and keeps the top
// limit; ties break on chunk id so results are reproducible.
func fuseCandidates(limit int, lists ...[]apiCandidate) []apiCandidate {
	byChunk := map[int64]*apiCandidate{}
	var order []*apiCandidate
	for _, l := range lists {
		for rank, c := range l {
			e, ok := byChunk[c.chunkID]
			if !ok {
				cc := c
				cc.score = 0
				e = &cc
				byChunk[c.chunkID] = e
				order = append(order, e)
			}
			e.score += 1 / (rrfK + float64(rank+1))
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].score != order[j].score {
			return order[i].score > order[j].score
		}
		return order[i].chunkID < order[j].chunkID
	})
	if len(order) > limit {
		order = order[:limit]
	}
	out := make([]apiCandidate, len(order))
	for i, e := range order {
		out[i] = *e
	}
	return out
}

// hitsFor loads the document fields of the fused candidates in one query.
func (s *KBStore) hitsFor(ctx context.Context, cands []apiCandidate) ([]Hit, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	ids := make([]interface{}, 0, len(cands))
	seen := map[int64]bool{}
	for _, c := range cands {
		if !seen[c.docID] {
			seen[c.docID] = true
			ids = append(ids, c.docID)
		}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, file_path, metadata, memory_scope, kind, status, author, rev, git_blob
		FROM kb_documents WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, ids...)
	if err != nil {
		return nil, fmt.Errorf("kb: load hit documents: %w", err)
	}
	defer rows.Close()
	docs := map[int64]Hit{}
	for rows.Next() {
		var id int64
		var h Hit
		var meta sql.NullString
		if err := rows.Scan(&id, &h.Path, &meta, &h.Scope, &h.Kind, &h.Status, &h.Author, &h.Rev, &h.GitBlob); err != nil {
			return nil, fmt.Errorf("kb: load hit documents: %w", err)
		}
		if meta.Valid && meta.String != "" && meta.String != "{}" {
			_ = json.Unmarshal([]byte(meta.String), &h.Meta) // malformed metadata: no Meta
		}
		docs[id] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(cands))
	for _, c := range cands {
		h, ok := docs[c.docID]
		if !ok {
			continue // deleted between ranking and loading
		}
		h.Chunk, h.StartLine, h.EndLine = c.text, c.start, c.end
		h.Score = c.score
		h.Rank = len(hits) + 1
		hits = append(hits, h)
	}
	return hits, nil
}

// Fuse merges hit lists ranked independently (typically one per project,
// whose scores are not comparable and whose embedding dimensions may differ)
// with Reciprocal Rank Fusion on each hit's rank in its own list, and keeps
// the top limit. Hits are the same when Project, Path and line range match;
// set Project before fusing lists from different stores.
func Fuse(limit int, lists ...[]Hit) []Hit {
	type key struct {
		project, path string
		start, end    int
		chunk         string
	}
	byKey := map[key]*Hit{}
	var order []*Hit
	for _, l := range lists {
		for rank, h := range l {
			k := key{h.Project, h.Path, h.StartLine, h.EndLine, h.Chunk}
			e, ok := byKey[k]
			if !ok {
				hh := h
				hh.Score = 0
				e = &hh
				byKey[k] = e
				order = append(order, e)
			}
			e.Score += 1 / (rrfK + float64(rank+1))
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.StartLine < b.StartLine
	})
	if limit > 0 && len(order) > limit {
		order = order[:limit]
	}
	out := make([]Hit, len(order))
	for i, h := range order {
		out[i] = *h
		out[i].Rank = i + 1
	}
	return out
}
