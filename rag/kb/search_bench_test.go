package kb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// benchCorpus populates n documents of roughly bodySize characters each
// (chunked and embedded with fakeEmbedder) so the vector-scan benchmarks
// below have a realistic pool of candidate chunks and document bodies to
// scan. It mirrors the shape of the KB the PANDO-US-0027 story measured
// against (many chunks, each document body far larger than its chunks).
func benchCorpus(b *testing.B, n, bodySize int) *KBStore {
	b.Helper()
	db := openTestKBDB(b)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, fakeEmbedder{}, 400, 40)
	ctx := context.Background()
	filler := strings.Repeat("golang knowledge base search benchmark filler text. ", (bodySize/54)+1)
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("document %d marker-term. %s", i, filler)
		if err := store.AddDocument(ctx, fmt.Sprintf("docs/doc-%04d.md", i), body, nil); err != nil {
			b.Fatalf("AddDocument(%d) error = %v", i, err)
		}
	}
	return store
}

// vectorScanRow scans one row of either the pre-PANDO-US-0027 SELECT (which
// included d.content, the full document body, once per chunk) or the fixed
// one (which does not), and returns the number of bytes the scanned column
// values occupy — the direct measure of "bytes scanned" the story asks for.
func vectorScanRow(rows *sql.Rows, includeBody bool) (int64, error) {
	var chunkID, docID int64
	var chunkContent, filePath, metaJSON string
	var blob []byte
	var createdAt, updatedAt time.Time
	var memKey, memScope, source string
	var importance float64
	var hits, outdated int
	var expiresAt sql.NullTime
	var docContent string

	var err error
	if includeBody {
		err = rows.Scan(
			&chunkID, &docID, &chunkContent, &blob,
			&filePath, &docContent, &metaJSON, &createdAt, &updatedAt,
			&memKey, &memScope, &importance, &hits,
			&source, &outdated, &expiresAt,
		)
	} else {
		err = rows.Scan(
			&chunkID, &docID, &chunkContent, &blob,
			&filePath, &metaJSON, &createdAt, &updatedAt,
			&memKey, &memScope, &importance, &hits,
			&source, &outdated, &expiresAt,
		)
	}
	if err != nil {
		return 0, err
	}
	return int64(len(chunkContent) + len(blob) + len(filePath) + len(docContent) +
		len(metaJSON) + len(memKey) + len(memScope) + len(source)), nil
}

const vectorScanQueryWithBody = `
	SELECT c.id, c.document_id, c.content, c.embedding,
	       d.file_path, d.content, d.metadata, d.created_at, d.updated_at,
	       COALESCE(d.memory_key,''), COALESCE(d.memory_scope,''),
	       COALESCE(d.importance,0.5), COALESCE(d.hits,0),
	       COALESCE(d.source,''), COALESCE(d.outdated,0), d.expires_at
	FROM kb_chunks c
	JOIN kb_documents d ON d.id = c.document_id
	WHERE c.embedding IS NOT NULL
`

// vectorScanQueryNoBody must stay textually identical to the SELECT in
// searchVector (kb.go) so this benchmark measures the actual production
// query, not a stand-in for it.
const vectorScanQueryNoBody = `
	SELECT c.id, c.document_id, c.content, c.embedding,
	       d.file_path, d.metadata, d.created_at, d.updated_at,
	       COALESCE(d.memory_key,''), COALESCE(d.memory_scope,''),
	       COALESCE(d.importance,0.5), COALESCE(d.hits,0),
	       COALESCE(d.source,''), COALESCE(d.outdated,0), d.expires_at
	FROM kb_chunks c
	JOIN kb_documents d ON d.id = c.document_id
	WHERE c.embedding IS NOT NULL
`

func runVectorScanBenchmark(b *testing.B, includeBody bool) {
	store := benchCorpus(b, 50, 2000)
	ctx := context.Background()
	query := vectorScanQueryNoBody
	if includeBody {
		query = vectorScanQueryWithBody
	}

	b.ReportAllocs()
	b.ResetTimer()
	var totalBytes int64
	var totalRows int64
	for i := 0; i < b.N; i++ {
		rows, err := store.db.QueryContext(ctx, query)
		if err != nil {
			b.Fatalf("query error = %v", err)
		}
		for rows.Next() {
			n, err := vectorScanRow(rows, includeBody)
			if err != nil {
				rows.Close()
				b.Fatalf("scan error = %v", err)
			}
			totalBytes += n
			totalRows++
		}
		if err := rows.Err(); err != nil {
			b.Fatalf("rows error = %v", err)
		}
		rows.Close()
	}
	b.ReportMetric(float64(totalBytes)/float64(b.N), "bytes_scanned/op")
	b.ReportMetric(float64(totalRows)/float64(b.N), "rows_scanned/op")
}

// BenchmarkSearchVectorScan_WithBody replicates the pre-PANDO-US-0027
// searchVector SELECT, which materialised d.content (the full document body)
// once per scanned chunk. Compare against BenchmarkSearchVectorScan_NoBody,
// which runs the fixed query, to see the bytes-scanned and bytes-allocated
// improvement:
//
//	go test ./internal/rag/kb/... -run '^$' -bench '^BenchmarkSearchVectorScan_' -benchmem
func BenchmarkSearchVectorScan_WithBody(b *testing.B) {
	runVectorScanBenchmark(b, true)
}

// BenchmarkSearchVectorScan_NoBody runs the exact SQL text of the fixed
// production searchVector query (PANDO-US-0027): see the "must stay
// textually identical" comment on vectorScanQueryNoBody.
func BenchmarkSearchVectorScan_NoBody(b *testing.B) {
	runVectorScanBenchmark(b, false)
}

// BenchmarkSearchDocumentsWithOptions exercises the full public search path
// (vector + FTS + RRF fusion) end to end after the fix, so an -benchmem run
// also reports the whole-query allocation cost a caller actually pays.
func BenchmarkSearchDocumentsWithOptions(b *testing.B) {
	store := benchCorpus(b, 50, 2000)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.SearchDocumentsWithOptions(ctx, "marker-term", 10, SearchOptions{}); err != nil {
			b.Fatalf("SearchDocumentsWithOptions() error = %v", err)
		}
	}
}
