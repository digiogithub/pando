package kb

import (
	"context"
	"database/sql"
	"fmt"
)

// backfillBatchSize is how many documents are re-linked per transaction. Small
// enough to keep the writer available to the rest of the app, large enough to
// avoid a transaction per document.
const backfillBatchSize = 50

// BackfillStats reports what a link backfill pass did.
type BackfillStats struct {
	// Candidates is how many documents were selected for (re)scanning.
	Candidates int
	// Scanned is how many of them were actually processed (may be lower than
	// Candidates when the context is cancelled mid-pass).
	Scanned int
	// Documents is how many produced at least one link.
	Documents int
	// Links is the number of link rows written.
	Links int
}

// BackfillLinks indexes the [[wiki links]] of documents that were stored before
// the link graph existed. It is the compatibility path for databases upgraded
// from a version without kb_links: those documents keep their content but have
// no link rows, and the filesystem sync will not re-add them because it skips
// files whose mtime has not changed.
//
// No chunking or embedding happens here: the links are extracted from the
// content already stored, so the pass costs local CPU and a few writes, never a
// call to the embedding provider.
//
// Candidates are documents whose content contains "[[" and that have no link
// rows yet — the content itself acts as the marker, so no schema column is
// needed and the pass is idempotent: once a document is indexed it stops being a
// candidate. A document whose only "[[" occurrences sit inside code fences
// yields no links and is re-scanned by later passes, which is harmless.
//
// The pass is a no-op on instances that write through the IPC proxy: the primary
// owns the writer connection and runs the backfill itself.
func (s *KBStore) BackfillLinks(ctx context.Context) (BackfillStats, error) {
	if s.proxy != nil {
		return BackfillStats{}, nil
	}
	return s.backfillLinks(ctx, false)
}

// RelinkAll rebuilds the whole link graph: every stored link row is dropped and
// re-extracted from the documents' content. Unlike BackfillLinks it does not
// skip documents that already have links, so it is the way to pick up a change
// in the extractor (a new syntax, a different slug rule) that made the stored
// rows stale.
func (s *KBStore) RelinkAll(ctx context.Context) (BackfillStats, error) {
	if s.proxy != nil {
		return BackfillStats{}, nil
	}
	return s.backfillLinks(ctx, true)
}

// backfillLinks re-extracts links for the candidate documents. When force is
// true every document that could hold a link is a candidate and its existing
// rows are replaced; otherwise only documents with no rows at all are visited.
func (s *KBStore) backfillLinks(ctx context.Context, force bool) (BackfillStats, error) {
	if !s.WikiLinksEnabled() {
		return BackfillStats{}, nil
	}

	ids, err := s.linkBackfillCandidates(ctx, force)
	if err != nil {
		return BackfillStats{}, err
	}

	stats := BackfillStats{Candidates: len(ids)}
	if len(ids) == 0 {
		return stats, nil
	}

	for start := 0; start < len(ids); start += backfillBatchSize {
		if err := ctx.Err(); err != nil {
			return stats, err
		}

		end := min(start+backfillBatchSize, len(ids))
		batch, err := s.relinkBatch(ctx, ids[start:end])
		stats.Scanned += batch.Scanned
		stats.Documents += batch.Documents
		stats.Links += batch.Links
		if err != nil {
			return stats, err
		}
	}

	return stats, nil
}

// linkBackfillCandidates returns the ids of the documents to re-scan. The ids
// are collected up front (rather than paging over the predicate) so that the
// pass always terminates: a document containing "[[" only inside a code fence
// produces no rows and would otherwise keep matching the predicate forever.
func (s *KBStore) linkBackfillCandidates(ctx context.Context, force bool) ([]int64, error) {
	query := `
		SELECT d.id
		FROM kb_documents d
		WHERE d.content LIKE '%[[%'
		  AND NOT EXISTS (SELECT 1 FROM kb_links l WHERE l.source_document_id = d.id)
		ORDER BY d.id`
	if force {
		query = `
			SELECT d.id
			FROM kb_documents d
			WHERE d.content LIKE '%[[%'
			ORDER BY d.id`
	}

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("kb: select link backfill candidates: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("kb: scan link backfill candidate: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("kb: iterate link backfill candidates: %w", err)
	}

	// A forced pass rebuilds the graph from scratch, so rows belonging to
	// documents that no longer hold any "[[" must go too.
	if force {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM kb_links`); err != nil {
			return nil, fmt.Errorf("kb: clear links: %w", err)
		}
	}

	return ids, nil
}

// relinkBatch re-extracts the links of one batch of documents in a single
// transaction. Documents that yield no links are not written at all: a forced
// pass has already cleared their rows globally, and an unforced one only ever
// visits documents that have none. That keeps repeated passes free of no-op
// writes, which matters because a document mentioning "[[" only inside a code
// fence stays a candidate forever.
func (s *KBStore) relinkBatch(ctx context.Context, ids []int64) (BackfillStats, error) {
	var stats BackfillStats

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("kb: begin link backfill tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	for _, id := range ids {
		var filePath, content string
		err := tx.QueryRowContext(ctx,
			`SELECT file_path, content FROM kb_documents WHERE id = ?`, id,
		).Scan(&filePath, &content)
		if err != nil {
			// The document was deleted between listing and processing: nothing
			// to link, and its rows are gone with it (ON DELETE CASCADE).
			if err == sql.ErrNoRows {
				continue
			}
			return stats, fmt.Errorf("kb: load document %d for backfill: %w", id, err)
		}

		stats.Scanned++

		links := ExtractWikiLinks(content)
		if len(links) == 0 {
			continue
		}

		// Delete first even though the rows should not exist: a concurrent write
		// may have indexed the document between listing and now, and duplicating
		// its links would be worse than the wasted statement.
		if _, err := tx.ExecContext(ctx, `DELETE FROM kb_links WHERE source_document_id = ?`, id); err != nil {
			return stats, fmt.Errorf("kb: delete links: %w", err)
		}
		n, err := insertDocumentLinks(ctx, tx, id, filePath, links)
		if err != nil {
			return stats, err
		}

		stats.Links += n
		stats.Documents++
	}

	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("kb: commit link backfill tx: %w", err)
	}
	return stats, nil
}
