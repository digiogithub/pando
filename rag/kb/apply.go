package kb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/digiogithub/pando/rag/sqlx"
)

// ChangeOp is the kind of a Change.
type ChangeOp string

const (
	OpAdd    ChangeOp = "add"
	OpModify ChangeOp = "modify"
	OpDelete ChangeOp = "delete"
	OpRename ChangeOp = "rename"
)

// Change is one file change of a commit, as a git diff reports it.
type Change struct {
	Op ChangeOp
	// Path is the file's path after the change (for a delete, the path
	// removed).
	Path string
	// OldPath is the path before a rename.
	OldPath string
	// Blob is the git blob id of the new content, when known. A change whose
	// Blob equals the indexed one is not re-read nor re-embedded.
	Blob string
	// Content is the new content; nil reads Path from the fsys passed to
	// ApplyChanges.
	Content []byte
}

// watermarkKey is the kb_state key ApplyChanges records its watermark under.
const watermarkKey = "watermark"

// Watermark returns the watermark of the last ApplyChanges (typically a
// commit id), or "" when none ran yet.
func (s *KBStore) Watermark(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kb_state WHERE key = ?`, watermarkKey).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("kb: read watermark: %w", err)
	}
	return v, nil
}

// indexable reports whether a path is indexed by ApplyChanges and, without
// Include patterns, by SyncTree: Markdown files.
func indexable(p string) bool { return isMarkdownFile(p) }

// plannedChange is a change resolved against the store before the write
// transaction: what to remove, what to move, and the prepared document to
// write (nil when nothing is written).
type plannedChange struct {
	deletePath string
	moveFrom   string
	moveTo     string
	write      *preparedDoc
}

// ApplyChanges indexes one commit's changes and records watermark, all in a
// single transaction: either every change and the new watermark are stored,
// or none is. Calling it again with the watermark already recorded does
// nothing, so a job can be retried safely.
//
// Only Markdown files are indexed; other paths are ignored (a rename of one
// into a Markdown path is an add). Content is read from Change.Content or
// from fsys. Everything that needs the embedder runs before the transaction.
// An add or modify whose Blob or content rev matches the indexed document
// is not re-embedded, and a rename with an unchanged blob only moves the
// document.
func (s *KBStore) ApplyChanges(ctx context.Context, fsys fs.FS, changes []Change, watermark string) (SyncStats, error) {
	var stats SyncStats
	if watermark != "" {
		cur, err := s.Watermark(ctx)
		if err != nil {
			return stats, err
		}
		if cur == watermark {
			return stats, nil
		}
	}

	// Plan every change against the stored state, overlaid with the effect
	// of the changes planned before it in this batch.
	overlay := map[string]storedState{}
	stateOf := func(p string) (storedState, error) {
		if st, ok := overlay[p]; ok {
			return st, nil
		}
		return s.storedStateOf(ctx, p)
	}
	read := func(c Change) (string, error) {
		if c.Content != nil {
			return string(c.Content), nil
		}
		if fsys == nil {
			return "", fmt.Errorf("kb: %s: no content and no fsys", c.Path)
		}
		b, err := fs.ReadFile(fsys, c.Path)
		if err != nil {
			return "", fmt.Errorf("kb: read %s: %w", c.Path, err)
		}
		return string(b), nil
	}
	// planWrite prepares a write of c.Path compared against prev.
	planWrite := func(c Change, prev storedState) (*preparedDoc, error) {
		if prev.exists && c.Blob != "" && !prev.stale && prev.blob == c.Blob {
			return nil, nil // same blob: nothing to read or embed
		}
		content, err := read(c)
		if err != nil {
			return nil, err
		}
		p, err := normalizeDoc(Doc{Path: c.Path, Content: content, GitBlob: c.Blob})
		if err != nil {
			return nil, err
		}
		if prev.sameContent(p.doc.Rev, p.doc.GitBlob) {
			p.reuse = true
		} else if err := s.chunkAndEmbed(ctx, &p); err != nil {
			return nil, err
		}
		return &p, nil
	}

	plans := make([]plannedChange, 0, len(changes))
	for _, c := range changes {
		if err := ctx.Err(); err != nil {
			return SyncStats{}, err
		}
		stats.Scanned++
		switch c.Op {
		case OpDelete:
			if indexable(c.Path) {
				plans = append(plans, plannedChange{deletePath: c.Path})
				overlay[c.Path] = storedState{}
			}
		case OpAdd, OpModify:
			if !indexable(c.Path) {
				continue
			}
			prev, err := stateOf(c.Path)
			if err != nil {
				return SyncStats{}, err
			}
			w, err := planWrite(c, prev)
			if err != nil {
				return SyncStats{}, err
			}
			if w == nil {
				stats.Unchanged++
				continue
			}
			plans = append(plans, plannedChange{write: w})
			overlay[c.Path] = storedState{exists: true, rev: w.doc.Rev, blob: w.doc.GitBlob}
		case OpRename:
			if c.OldPath == "" {
				return SyncStats{}, fmt.Errorf("kb: rename to %s without OldPath", c.Path)
			}
			old, err := stateOf(c.OldPath)
			if err != nil {
				return SyncStats{}, err
			}
			pl := plannedChange{}
			prev := storedState{}
			switch {
			case old.exists && indexable(c.Path):
				pl.moveFrom, pl.moveTo, prev = c.OldPath, c.Path, old
			case old.exists:
				pl.deletePath = c.OldPath
			}
			overlay[c.OldPath] = storedState{}
			if indexable(c.Path) {
				w, err := planWrite(c, prev)
				if err != nil {
					return SyncStats{}, err
				}
				pl.write = w
				st := prev
				if w != nil {
					st = storedState{exists: true, rev: w.doc.Rev, blob: w.doc.GitBlob}
				}
				overlay[c.Path] = st
			}
			plans = append(plans, pl)
		default:
			return SyncStats{}, fmt.Errorf("kb: unknown change op %q for %s", c.Op, c.Path)
		}
	}

	err := sqlx.RunTx(ctx, s.db, s.retryable, func(tx *sql.Tx) error {
		st := SyncStats{Scanned: stats.Scanned, Unchanged: stats.Unchanged}
		for i := range plans {
			pl := &plans[i]
			if pl.deletePath != "" {
				ok, err := deleteDocTx(ctx, tx, pl.deletePath)
				if err != nil {
					return err
				}
				if ok {
					st.Deleted++
				}
			}
			if pl.moveFrom != "" {
				if err := renameDocTx(ctx, tx, pl.moveFrom, pl.moveTo); err != nil {
					return err
				}
				st.Renamed++
			}
			if pl.write == nil {
				continue
			}
			created, err := s.writeDocTx(ctx, tx, pl.write)
			if err != nil {
				return err
			}
			switch {
			case created:
				st.Added++
			case pl.write.reuse:
				st.Unchanged++
			default:
				st.Updated++
			}
			if !pl.write.reuse {
				st.LinksIndexed += s.countIndexedLinks(pl.write.body)
			}
		}
		if watermark != "" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO kb_state(key, value, updated_at) VALUES (?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
				watermarkKey, watermark, time.Now().UTC()); err != nil {
				return fmt.Errorf("kb: write watermark: %w", err)
			}
		}
		stats = st
		return nil
	})
	if errors.Is(err, errReuseGone) {
		// Single writer per store: a document vanished under us. Nothing was
		// committed; the caller retries the same batch.
		return SyncStats{}, fmt.Errorf("kb: apply changes: concurrent writer: %w", err)
	}
	if err != nil {
		return SyncStats{}, err
	}
	return stats, nil
}

// renameDocTx moves the document at from to to, keeping its id, chunks and
// embeddings; a document already at to is replaced.
func renameDocTx(ctx context.Context, tx *sql.Tx, from, to string) error {
	if from == to {
		return nil
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM kb_documents WHERE file_path = ?`, from).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("kb: find %s: %w", from, err)
	}
	if _, err := deleteDocTx(ctx, tx, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kb_documents SET file_path = ?, updated_at = ? WHERE id = ?`, to, time.Now().UTC(), id); err != nil {
		return fmt.Errorf("kb: rename %s: %w", from, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kb_links SET source_path = ? WHERE source_document_id = ?`, to, id); err != nil {
		return fmt.Errorf("kb: rename links of %s: %w", from, err)
	}
	return nil
}

// SyncOptions configures SyncTree.
type SyncOptions struct {
	// Prefix is the directory of the fsys to walk ("" = the whole fsys).
	// Document paths are fsys paths, so they keep this prefix.
	Prefix string
	// Include keeps only files matching any of these path.Match patterns,
	// tried against the full path and the base name. Empty means Markdown
	// files.
	Include []string
	// Exclude drops files (or whole directories) matching any of these
	// patterns, tried the same way.
	Exclude []string
	// DeleteMissing removes stored documents under Prefix that the walk did
	// not see.
	DeleteMissing bool
}

func matchAny(patterns []string, p string) bool {
	base := path.Base(p)
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, p); ok {
			return true
		}
		if ok, _ := path.Match(pat, base); ok {
			return true
		}
	}
	return false
}

// SyncTree indexes every file of fsys under opts.Prefix with Put (unchanged
// files are not re-embedded) and, with DeleteMissing, removes the stored
// documents under the prefix that are gone. Hidden files and directories are
// skipped. It stops at the first error or when ctx is cancelled; what was
// stored until then stays. It does not touch the ApplyChanges watermark.
func (s *KBStore) SyncTree(ctx context.Context, fsys fs.FS, opts SyncOptions) (SyncStats, error) {
	var stats SyncStats
	root := strings.Trim(opts.Prefix, "/")
	if root == "" {
		root = "."
	}
	seen := map[string]bool{}
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if matchAny(opts.Exclude, p) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(opts.Include) > 0 {
			if !matchAny(opts.Include, p) {
				return nil
			}
		} else if !indexable(p) {
			return nil
		}
		stats.Scanned++
		seen[p] = true
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("kb: read %s: %w", p, err)
		}
		res, err := s.Put(ctx, Doc{Path: p, Content: string(b)})
		if err != nil {
			return err
		}
		switch {
		case res.Created:
			stats.Added++
		case res.Embedded:
			stats.Updated++
		default:
			stats.Unchanged++
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if !opts.DeleteMissing {
		return stats, nil
	}

	query := `SELECT file_path FROM kb_documents WHERE kind != ?`
	args := []interface{}{KindMemory}
	if root != "." {
		query += ` AND file_path LIKE ? || '%' ESCAPE '\'`
		args = append(args, escapeLikePrefix(root+"/"))
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return stats, fmt.Errorf("kb: list stored documents: %w", err)
	}
	var gone []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return stats, err
		}
		if !seen[p] {
			gone = append(gone, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return stats, err
	}
	for _, p := range gone {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if err := s.Delete(ctx, p); err != nil {
			return stats, err
		}
		stats.Deleted++
	}
	return stats, nil
}
