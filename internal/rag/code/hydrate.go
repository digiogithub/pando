package code

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// insertSymbolFTS populates the contentless FTS index for a freshly inserted
// code_symbols row. The source text is tokenized into the inverted index but
// not stored: the FTS rowid is the code_symbols rowid so search can join back.
func insertSymbolFTS(ctx context.Context, tx *sql.Tx, res sql.Result, sym *CodeSymbol) error {
	rowid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO code_symbols_fts(rowid, name, name_path, doc_string, source_code)
		VALUES (?,?,?,?,?)`,
		rowid, sym.Name, sym.NamePath, sym.DocString, sym.SourceCode)
	return err
}

// sourceCache memoizes per-request filesystem reads and project/file metadata so
// hydrating many symbols that share files or projects only hits disk once.
type sourceCache struct {
	roots  map[string]string      // projectID -> root_path ("" = unknown)
	files  map[string]*cachedFile // abs path -> content/hash/err
	hashes map[string]string      // projectID\x00relPath -> indexed file_hash
}

type cachedFile struct {
	content []byte
	hash    string
	err     error
}

func newSourceCache() *sourceCache {
	return &sourceCache{
		roots:  map[string]string{},
		files:  map[string]*cachedFile{},
		hashes: map[string]string{},
	}
}

func (c *CodeIndexer) projectRoot(ctx context.Context, cache *sourceCache, projectID string) string {
	if r, ok := cache.roots[projectID]; ok {
		return r
	}
	var root string
	_ = c.db.QueryRowContext(ctx, `SELECT root_path FROM code_projects WHERE project_id = ?`, projectID).Scan(&root)
	cache.roots[projectID] = root
	return root
}

func (c *CodeIndexer) loadFile(cache *sourceCache, absPath string) *cachedFile {
	if f, ok := cache.files[absPath]; ok {
		return f
	}
	f := &cachedFile{}
	content, err := os.ReadFile(absPath)
	if err != nil {
		f.err = err
	} else {
		f.content = content
		f.hash = fmt.Sprintf("%x", sha256.Sum256(content))
	}
	cache.files[absPath] = f
	return f
}

// indexedHash returns the file_hash recorded at index time for a project file.
func (c *CodeIndexer) indexedHash(ctx context.Context, cache *sourceCache, projectID, relPath string) string {
	key := projectID + "\x00" + relPath
	if h, ok := cache.hashes[key]; ok {
		return h
	}
	var stored string
	_ = c.db.QueryRowContext(ctx,
		`SELECT file_hash FROM code_files WHERE project_id = ? AND file_path = ?`,
		projectID, relPath).Scan(&stored)
	cache.hashes[key] = stored
	return stored
}

// hydrateSource fills sym.SourceCode from the live file on disk.
//
// When the file still matches the indexed hash it slices the exact byte range
// [StartByte:EndByte]; when the file changed (or offsets no longer fit) it falls
// back to a line-based extraction and marks sym.Stale so callers know the text
// may not align with the stored offsets.
func (c *CodeIndexer) hydrateSource(ctx context.Context, sym *CodeSymbol, cache *sourceCache) {
	if sym == nil || sym.FilePath == "" {
		return
	}
	root := c.projectRoot(ctx, cache, sym.ProjectID)
	if root == "" {
		return
	}
	f := c.loadFile(cache, filepath.Join(root, sym.FilePath))
	if f.err != nil {
		sym.Stale = true
		return
	}

	changed := false
	if stored := c.indexedHash(ctx, cache, sym.ProjectID, sym.FilePath); stored != "" && stored != f.hash {
		changed = true
	}

	if !changed && sym.EndByte > sym.StartByte && sym.EndByte <= len(f.content) {
		sym.SourceCode = string(f.content[sym.StartByte:sym.EndByte])
		return
	}

	// Fallback: line-based extraction is more robust to edits than byte offsets.
	sym.SourceCode = extractLines(f.content, sym.StartLine, sym.EndLine)
	sym.Stale = changed
}

// hydrateTree hydrates a symbol and, recursively, its children.
func (c *CodeIndexer) hydrateTree(ctx context.Context, sym *CodeSymbol, cache *sourceCache) {
	c.hydrateSource(ctx, sym, cache)
	for _, child := range sym.Children {
		c.hydrateTree(ctx, child, cache)
	}
}

// hydrateSymbols hydrates a slice of symbol trees using a shared per-call cache.
func (c *CodeIndexer) hydrateSymbols(ctx context.Context, syms []*CodeSymbol) {
	if len(syms) == 0 {
		return
	}
	cache := newSourceCache()
	for _, s := range syms {
		c.hydrateTree(ctx, s, cache)
	}
}

// extractLines returns the inclusive 1-based [start,end] line range of content.
func extractLines(content []byte, start, end int) string {
	if len(content) == 0 {
		return ""
	}
	if start <= 0 {
		start = 1
	}
	lines := strings.Split(string(content), "\n")
	if start > len(lines) {
		return ""
	}
	if end > len(lines) || end < start {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}
