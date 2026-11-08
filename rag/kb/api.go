package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digiogithub/pando/rag/sqlx"
)

// Document kinds and statuses the library itself knows about. Callers may use
// any other values: the library does not impose a vocabulary.
const (
	KindDocument = "document"
	KindMemory   = "memory"
	StatusActive = "active"
)

// ErrNotFound is returned by Get when no document has the given path.
var ErrNotFound = errors.New("kb: document not found")

// Doc is a document as stored by Put and returned by Get. Path is its id.
//
// Fields left empty are taken from the content's YAML front matter when it
// has them (kind, else type; status, author, scope; tags, else labels), and otherwise
// default to KindDocument, StatusActive and no author/scope. Rev defaults to
// the sha256 of Content. Meta is stored as JSON: the front-matter keys, with
// Meta's own entries on top.
type Doc struct {
	Path    string
	Content string
	Meta    map[string]interface{}
	Tags    []string
	Scope   string
	Author  string
	Kind    string
	Status  string
	// Rev is an opaque revision given by the caller (e.g. a content hash of
	// its own). A Put whose Rev equals the stored one does not re-embed.
	Rev string
	// GitBlob is the git blob id the content was read from, when known.
	GitBlob   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PutResult reports what Put did.
type PutResult struct {
	// Created is true when no document had this path before.
	Created bool
	// Embedded is true when the content was (re)chunked and (re)embedded;
	// false when only the document's fields changed because its Rev did not.
	Embedded bool
}

// preparedDoc is a write computed outside any transaction: front matter
// parsed, defaults applied, chunks cut and embedded. reuse means the stored
// chunks are still valid and only the document row changes.
type preparedDoc struct {
	doc      Doc
	metaJSON string
	body     string
	bodyLine int
	fields   docFields
	chunks   []lineChunk
	vecs     [][]float32
	reuse    bool
}

// ContentRev is the default Rev of a document: the hex sha256 of its content.
func ContentRev(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// normalizeDoc applies the front-matter fallbacks and defaults documented on
// Doc and builds its metadata.
func normalizeDoc(d Doc) (preparedDoc, error) {
	if d.Path == "" {
		return preparedDoc{}, errors.New("kb: document path cannot be empty")
	}
	p := parseDocument(d.Content)
	raw := p.Raw
	pick := func(v string, key string) string {
		if v != "" {
			return v
		}
		return firstString(raw[key])
	}
	// gintrack files name their kind "type" (story, spec, memory...).
	d.Kind = pick(pick(d.Kind, "kind"), "type")
	d.Status = pick(d.Status, "status")
	d.Author = pick(d.Author, "author")
	d.Scope = pick(d.Scope, "scope")
	if d.Kind == "" {
		d.Kind = KindDocument
	}
	if d.Status == "" {
		d.Status = StatusActive
	}
	if d.Rev == "" {
		d.Rev = ContentRev(d.Content)
	}
	if d.Tags == nil {
		d.Tags = p.Tags
		if len(d.Tags) == 0 {
			d.Tags = stringList(raw["labels"])
		}
	}

	meta := make(map[string]interface{}, len(raw)+len(d.Meta)+2)
	for k, v := range raw {
		meta[k] = jsonSafeValue(v)
	}
	for k, v := range d.Meta {
		meta[k] = v
	}
	meta = InjectTagsIntoMetadata(meta, d.Tags)
	if len(p.Aliases) > 0 {
		meta = InjectAliasesIntoMetadata(meta, p.Aliases)
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return preparedDoc{}, fmt.Errorf("kb: marshal metadata of %s: %w", d.Path, err)
	}
	d.Meta = meta
	return preparedDoc{doc: d, metaJSON: string(b), body: p.Body, bodyLine: p.BodyLine, fields: p.Fields}, nil
}

// storedState is what the store holds for a path: its rev and git blob, and
// whether its chunks need re-embedding regardless (stale: a chunk has no
// embedding, or one from another model, while an embedder is configured).
type storedState struct {
	exists    bool
	rev, blob string
	stale     bool
}

// sameContent reports whether a write with rev/blob can reuse the stored
// chunks.
func (st storedState) sameContent(rev, blob string) bool {
	return st.exists && !st.stale && (st.rev == rev || (st.blob != "" && st.blob == blob))
}

func (s *KBStore) storedStateOf(ctx context.Context, path string) (storedState, error) {
	st := storedState{exists: true}
	checkEmb := s.embedder != nil
	err := s.db.QueryRowContext(ctx, `
		SELECT d.rev, d.git_blob,
		       ? AND EXISTS (SELECT 1 FROM kb_chunks c WHERE c.document_id = d.id
		                     AND (c.embedding IS NULL OR (? != '' AND c.embedding_model != ?)))
		FROM kb_documents d WHERE d.file_path = ?`,
		checkEmb, s.embeddingModel, s.embeddingModel, path).Scan(&st.rev, &st.blob, &st.stale)
	if err == sql.ErrNoRows {
		return storedState{}, nil
	}
	if err != nil {
		return storedState{}, fmt.Errorf("kb: read state of %s: %w", path, err)
	}
	return st, nil
}

// chunkAndEmbed cuts the document body and embeds the chunks. It runs outside
// any transaction: embedding is a network call and RunTx may retry.
func (s *KBStore) chunkAndEmbed(ctx context.Context, p *preparedDoc) error {
	p.reuse = false
	p.chunks = chunkLines(p.body, p.bodyLine, s.chunkSize, s.chunkOverlap)
	if len(p.chunks) == 0 || s.embedder == nil {
		return nil
	}
	texts := make([]string, len(p.chunks))
	for i, c := range p.chunks {
		texts[i] = c.Text
	}
	embedCtx, cancel := context.WithTimeout(ctx, kbEmbeddingsTimeout)
	defer cancel()
	vecs, err := s.embedder.EmbedDocuments(embedCtx, texts)
	if err != nil {
		return fmt.Errorf("kb: embed %s: %w", p.doc.Path, err)
	}
	if len(vecs) != len(texts) {
		return fmt.Errorf("kb: embed %s: got %d vectors for %d chunks", p.doc.Path, len(vecs), len(texts))
	}
	p.vecs = vecs
	return nil
}

// prepare builds the write for d. When the stored document has the same Rev
// (or the same non-empty GitBlob) its chunks are reused and nothing is
// embedded.
func (s *KBStore) prepare(ctx context.Context, d Doc) (preparedDoc, error) {
	p, err := normalizeDoc(d)
	if err != nil {
		return p, err
	}
	st, err := s.storedStateOf(ctx, p.doc.Path)
	if err != nil {
		return p, err
	}
	if st.sameContent(p.doc.Rev, p.doc.GitBlob) {
		p.reuse = true
		return p, nil
	}
	return p, s.chunkAndEmbed(ctx, &p)
}

// writeDocTx stores a prepared document inside tx, updating it in place when
// the path exists (its id and created_at are kept). It reports whether the
// document was created.
func (s *KBStore) writeDocTx(ctx context.Context, tx *sql.Tx, p *preparedDoc) (bool, error) {
	d := p.doc
	now := time.Now().UTC()

	var docID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM kb_documents WHERE file_path = ?`, d.Path).Scan(&docID)
	created := err == sql.ErrNoRows
	if created && p.reuse {
		// The document was removed between prepare and this transaction:
		// there are no chunks to reuse.
		return false, errReuseGone
	}
	switch {
	case created:
		res, err := tx.ExecContext(ctx, `
			INSERT INTO kb_documents (file_path, content, metadata, memory_scope, kind, status, author, rev, git_blob, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.Path, d.Content, p.metaJSON, d.Scope, d.Kind, d.Status, d.Author, d.Rev, d.GitBlob, now, now)
		if err != nil {
			return false, fmt.Errorf("kb: insert %s: %w", d.Path, err)
		}
		if docID, err = res.LastInsertId(); err != nil {
			return false, fmt.Errorf("kb: insert %s: %w", d.Path, err)
		}
	case err != nil:
		return false, fmt.Errorf("kb: find %s: %w", d.Path, err)
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE kb_documents
			SET content = ?, metadata = ?, memory_scope = ?, kind = ?, status = ?, author = ?, rev = ?, git_blob = ?, updated_at = ?
			WHERE id = ?`,
			d.Content, p.metaJSON, d.Scope, d.Kind, d.Status, d.Author, d.Rev, d.GitBlob, now, docID); err != nil {
			return false, fmt.Errorf("kb: update %s: %w", d.Path, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM kb_doc_fields_fts WHERE rowid = ?`, docID); err != nil {
		return false, fmt.Errorf("kb: clear fields of %s: %w", d.Path, err)
	}
	if !p.fields.empty() {
		if _, err := tx.ExecContext(ctx, `INSERT INTO kb_doc_fields_fts(rowid, title, labels, aliases) VALUES (?, ?, ?, ?)`,
			docID, p.fields.Title, p.fields.Labels, p.fields.Aliases); err != nil {
			return false, fmt.Errorf("kb: index fields of %s: %w", d.Path, err)
		}
	}
	if p.reuse && !created {
		return false, nil
	}

	if !created {
		if err := deleteChunksTx(ctx, tx, docID); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM kb_links WHERE source_document_id = ?`, docID); err != nil {
			return false, fmt.Errorf("kb: delete links: %w", err)
		}
	}
	if s.WikiLinksEnabled() {
		if _, err := insertDocumentLinks(ctx, tx, docID, d.Path, ExtractWikiLinks(p.body)); err != nil {
			return false, err
		}
	}
	for i, c := range p.chunks {
		var blob []byte
		var model string
		var dims int
		if i < len(p.vecs) && len(p.vecs[i]) > 0 {
			blob, model, dims = serializeFloat32(p.vecs[i]), s.embeddingModel, len(p.vecs[i])
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO kb_chunks (document_id, chunk_index, content, embedding, created_at, embedding_model, embedding_dims, start_line, end_line)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			docID, i, c.Text, blob, now, model, dims, c.StartLine, c.EndLine)
		if err != nil {
			return false, fmt.Errorf("kb: insert chunk %d of %s: %w", i, d.Path, err)
		}
		chunkID, err := res.LastInsertId()
		if err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO kb_fts(rowid, content) VALUES (?, ?)`, chunkID, c.Text); err != nil {
			return false, fmt.Errorf("kb: index chunk %d of %s: %w", i, d.Path, err)
		}
	}
	return created, nil
}

// errReuseGone makes the caller re-embed a write whose stored chunks
// disappeared after it was prepared.
var errReuseGone = errors.New("kb: stored document vanished before write")

// Put stores d, replacing any document with the same path. Content is
// chunked without its front matter and embedded before the write; when the
// stored document has the same Rev (or the same non-empty GitBlob) only its
// fields are updated and nothing is re-embedded.
func (s *KBStore) Put(ctx context.Context, d Doc) (PutResult, error) {
	p, err := s.prepare(ctx, d)
	if err != nil {
		return PutResult{}, err
	}
	var created bool
	write := func(tx *sql.Tx) error {
		var err error
		created, err = s.writeDocTx(ctx, tx, &p)
		return err
	}
	err = sqlx.RunTx(ctx, s.db, s.retryable, write)
	if errors.Is(err, errReuseGone) {
		if err = s.chunkAndEmbed(ctx, &p); err == nil {
			err = sqlx.RunTx(ctx, s.db, s.retryable, write)
		}
	}
	if err != nil {
		return PutResult{}, err
	}
	return PutResult{Created: created, Embedded: !p.reuse}, nil
}

// Get returns the document stored at path, or ErrNotFound.
func (s *KBStore) Get(ctx context.Context, path string) (*Doc, error) {
	var d Doc
	var metaJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT file_path, content, metadata, memory_scope, author, kind, status, rev, git_blob, created_at, updated_at
		FROM kb_documents WHERE file_path = ?`, path).
		Scan(&d.Path, &d.Content, &metaJSON, &d.Scope, &d.Author, &d.Kind, &d.Status, &d.Rev, &d.GitBlob, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("kb: get %s: %w", path, err)
	}
	if metaJSON != "" && metaJSON != "{}" {
		if err := json.Unmarshal([]byte(metaJSON), &d.Meta); err != nil {
			return nil, fmt.Errorf("kb: unmarshal metadata of %s: %w", path, err)
		}
	}
	d.Tags = ExtractTagsFromMetadata(d.Meta)
	return &d, nil
}

// Delete removes the document at path. Deleting a missing path is not an
// error.
func (s *KBStore) Delete(ctx context.Context, path string) error {
	return s.deleteDocument(ctx, path)
}

// DeletePrefix removes every document whose path starts with prefix, in one
// transaction, and returns how many it removed. An empty prefix is refused
// rather than taken to mean "everything".
func (s *KBStore) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if prefix == "" {
		return 0, errors.New("kb: DeletePrefix: empty prefix")
	}
	var n int
	err := sqlx.RunTx(ctx, s.db, s.retryable, func(tx *sql.Tx) error {
		n = 0
		rows, err := tx.QueryContext(ctx, `SELECT file_path FROM kb_documents WHERE file_path LIKE ? || '%' ESCAPE '\'`, escapeLikePrefix(prefix))
		if err != nil {
			return fmt.Errorf("kb: list prefix %s: %w", prefix, err)
		}
		var paths []string
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return err
			}
			paths = append(paths, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, p := range paths {
			ok, err := deleteDocTx(ctx, tx, p)
			if err != nil {
				return err
			}
			if ok {
				n++
			}
		}
		return nil
	})
	return n, err
}

// Close releases the store. The *sql.DB passed to Open stays open: it is
// owned by the caller.
func (s *KBStore) Close() error { return nil }
