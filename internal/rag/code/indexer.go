// Package code provides tree-sitter based code indexing with semantic search.
package code

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/rag/treesitter"
)

const (
	// defaultWorkers is the number of concurrent file processing workers.
	defaultWorkers = 4

	// largeSymbolThreshold is the byte size above which a symbol gets chunked.
	largeSymbolThreshold = 800

	// maxEmbeddingBatch is the max symbols to embed in one batch.
	maxEmbeddingBatch = 50

	// codeEmbeddingsTimeout bounds a single embedding batch request.
	codeEmbeddingsTimeout = 90 * time.Second

	// maxJobWarnings is the maximum number of per-file warnings retained in-memory per indexing job.
	maxJobWarnings = 100
)

// CodeIndexer manages code indexing projects using tree-sitter for parsing
// and embeddings for semantic search.
type CodeIndexer struct {
	db       *sql.DB
	embedder embeddings.Embedder
	parser   *treesitter.Parser
	walker   *treesitter.ASTWalker
	workers  int

	// Active indexing jobs
	jobsMu sync.RWMutex
	jobs   map[string]*IndexingJob
}

// NewCodeIndexer creates a new CodeIndexer with the given number of concurrent workers.
// If workers is <= 0, the defaultWorkers value is used.
func NewCodeIndexer(db *sql.DB, embedder embeddings.Embedder, workers int) *CodeIndexer {
	if workers <= 0 {
		workers = defaultWorkers
	}
	return &CodeIndexer{
		db:       db,
		embedder: embedder,
		parser:   treesitter.NewParser(),
		walker:   treesitter.NewASTWalker(treesitter.DefaultWalkerConfig()),
		workers:  workers,
		jobs:     make(map[string]*IndexingJob),
	}
}

// IndexProject indexes all supported source files in a project directory.
// It runs asynchronously and updates the job status in the jobs map.
// Returns the job ID immediately.
func (c *CodeIndexer) IndexProject(ctx context.Context, projectID, projectPath string, languages []Language) (string, error) {
	// Upsert project record
	now := time.Now().UTC()
	projectName := filepath.Base(projectPath)
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO code_projects (project_id, name, root_path, indexing_status, created_at, updated_at)
		VALUES (?, ?, ?, 'in_progress', ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			name = excluded.name,
			root_path = excluded.root_path,
			indexing_status = 'in_progress',
			updated_at = excluded.updated_at`,
		projectID, projectName, projectPath, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("code: upsert project: %w", err)
	}

	// Create job
	jobID := uuid.New().String()
	job := &IndexingJob{
		ID:          jobID,
		ProjectID:   projectID,
		ProjectPath: projectPath,
		Status:      IndexingStatusInProgress,
		StartedAt:   now,
	}
	c.jobsMu.Lock()
	c.jobs[jobID] = job
	c.jobsMu.Unlock()

	// Run indexing in background
	go func() {
		bgCtx := context.Background()
		if err := c.indexProjectSync(bgCtx, job, languages); err != nil {
			c.jobsMu.Lock()
			job.Status = IndexingStatusFailed
			errStr := err.Error()
			job.Error = &errStr
			c.jobsMu.Unlock()

			c.db.ExecContext(bgCtx, `UPDATE code_projects SET indexing_status='failed', updated_at=? WHERE project_id=?`,
				time.Now().UTC(), projectID)
		} else {
			c.jobsMu.Lock()
			job.Status = IndexingStatusCompleted
			completedAt := time.Now().UTC()
			job.CompletedAt = &completedAt
			c.jobsMu.Unlock()

			c.db.ExecContext(bgCtx, `UPDATE code_projects SET indexing_status='completed', last_indexed_at=?, updated_at=? WHERE project_id=?`,
				time.Now().UTC(), time.Now().UTC(), projectID)
		}
	}()

	return jobID, nil
}

// indexProjectSync does the actual indexing synchronously (called from goroutine).
func (c *CodeIndexer) indexProjectSync(ctx context.Context, job *IndexingJob, languages []Language) error {
	// Collect all supported files
	var files []string
	allowedLangs := make(map[Language]bool)
	for _, lang := range languages {
		allowedLangs[lang] = true
	}

	err := filepath.WalkDir(job.ProjectPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if d.IsDir() {
			// Skip hidden dirs and common non-source dirs
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" ||
				name == "dist" || name == "build" || name == "__pycache__" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if treesitter.IsSupportedFile(path) {
			if len(allowedLangs) > 0 {
				lang, ok := treesitter.DetectLanguage(path)
				if ok && allowedLangs[lang] {
					files = append(files, path)
				}
			} else {
				files = append(files, path)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("code: walk project: %w", err)
	}

	c.jobsMu.Lock()
	job.FilesTotal = len(files)
	c.jobsMu.Unlock()

	// Process files with worker pool
	type workItem struct {
		path string
	}
	type workResult struct {
		path string
		err  error
	}

	workCh := make(chan workItem, len(files))
	resultCh := make(chan workResult, len(files))

	for i := 0; i < c.workers; i++ {
		go func() {
			for item := range workCh {
				err := c.safeIndexFile(ctx, job.ProjectID, job.ProjectPath, item.path)
				resultCh <- workResult{path: item.path, err: err}
			}
		}()
	}

	for _, f := range files {
		workCh <- workItem{f}
	}
	close(workCh)

	indexed := 0
	failed := 0
	processed := 0
	for range files {
		result := <-resultCh
		if result.err == nil {
			indexed++
		} else {
			failed++
		}
		processed++
		c.jobsMu.Lock()
		job.FilesIndexed = indexed
		job.FilesFailed = failed
		if job.FilesTotal > 0 {
			job.Progress = float64(processed) / float64(job.FilesTotal) * 100
		}
		if result.err != nil && len(job.Warnings) < maxJobWarnings {
			relPath, relErr := filepath.Rel(job.ProjectPath, result.path)
			if relErr != nil {
				relPath = result.path
			}
			job.Warnings = append(job.Warnings, fmt.Sprintf("index warning (%s): %v", relPath, result.err))
		}
		c.jobsMu.Unlock()
	}

	// Update language stats
	return c.updateLanguageStats(ctx, job.ProjectID)
}

// safeIndexFile protects project indexing from non-fatal panics in per-file processing.
// NOTE: fatal runtime crashes (e.g. SIGSEGV inside cgo) cannot be recovered here.
func (c *CodeIndexer) safeIndexFile(ctx context.Context, projectID, rootPath, filePath string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while indexing %s: %v", filePath, r)
		}
	}()

	return c.indexFile(ctx, projectID, rootPath, filePath)
}

// indexFile indexes a single file within a project.
func (c *CodeIndexer) indexFile(ctx context.Context, projectID, rootPath, filePath string) error {
	// Compute relative path
	relPath, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	// Read file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("code: read file %s: %w", filePath, err)
	}

	// Compute hash
	hash := fmt.Sprintf("%x", sha256.Sum256(content))

	// Detect language
	lang, ok := treesitter.DetectLanguage(filePath)
	if !ok {
		return nil // Skip unsupported
	}

	// Check if file already indexed with same hash
	var fileID int64
	var existingHash string
	err = c.db.QueryRowContext(ctx, `
		SELECT id, file_hash FROM code_files
		WHERE project_id = ? AND file_path = ?`,
		projectID, relPath,
	).Scan(&fileID, &existingHash)

	if err == nil && existingHash == hash {
		return nil // No change
	}

	// Parse file
	tree, err := c.parser.Parse(ctx, content, lang)
	if err != nil {
		return fmt.Errorf("code: parse %s: %w", relPath, err)
	}
	defer tree.Close()

	// Extract symbols
	symbols, err := c.walker.ExtractSymbols(tree, content, lang, relPath, projectID)
	if err != nil {
		return fmt.Errorf("code: extract symbols %s: %w", relPath, err)
	}

	// Upsert file record in a transaction
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("code: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC()

	if fileID > 0 {
		// Update existing file — delete old symbols first
		if _, err = tx.ExecContext(ctx, `DELETE FROM code_symbols WHERE file_id = ?`, fileID); err != nil {
			return fmt.Errorf("code: delete old symbols: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE code_files SET file_hash=?, language=?, symbols_count=?, indexed_at=? WHERE id=?`,
			hash, string(lang), len(symbols), now, fileID); err != nil {
			return fmt.Errorf("code: update file: %w", err)
		}
	} else {
		// Insert new file
		res, err := tx.ExecContext(ctx, `
			INSERT INTO code_files (project_id, file_path, language, file_hash, symbols_count, indexed_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			projectID, relPath, string(lang), hash, len(symbols), now,
		)
		if err != nil {
			return fmt.Errorf("code: insert file: %w", err)
		}
		fileID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("code: file last insert id: %w", err)
		}
	}

	// Insert symbols
	for _, sym := range symbols {
		metaJSON := "{}"
		if len(sym.Metadata) > 0 {
			if b, e := json.Marshal(sym.Metadata); e == nil {
				metaJSON = string(b)
			}
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO code_symbols
				(id, project_id, file_id, file_path, language, symbol_type, name, name_path,
				 start_line, end_line, start_byte, end_byte,
				 source_code, signature, doc_string, parent_id, metadata, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			sym.ID, sym.ProjectID, fileID, sym.FilePath, string(sym.Language),
			string(sym.SymbolType), sym.Name, sym.NamePath,
			sym.StartLine, sym.EndLine, sym.StartByte, sym.EndByte,
			sym.SourceCode, sym.Signature, sym.DocString, sym.ParentID,
			metaJSON, now, now,
		)
		if err != nil {
			return fmt.Errorf("code: insert symbol %s: %w", sym.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("code: commit: %w", err)
	}

	// Generate embeddings for symbols (outside transaction)
	return c.embedSymbols(ctx, projectID, fileID, symbols)
}

// embedSymbols generates and stores embeddings for all symbols in a file.
func (c *CodeIndexer) embedSymbols(ctx context.Context, projectID string, fileID int64, symbols []*treesitter.CodeSymbol) error {
	if c.embedder == nil || len(symbols) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	_ = projectID
	_ = fileID

	// Build texts for embedding
	texts := make([]string, len(symbols))
	for i, sym := range symbols {
		// Create rich embedding text: name_path + doc_string + signature + source snippet
		var sb strings.Builder
		sb.WriteString(sym.NamePath)
		sb.WriteString("\n")
		if sym.DocString != "" {
			sb.WriteString(sym.DocString)
			sb.WriteString("\n")
		}
		if sym.Signature != "" {
			sb.WriteString(sym.Signature)
			sb.WriteString("\n")
		}
		// Add source code up to threshold
		src := sym.SourceCode
		if len(src) > largeSymbolThreshold {
			src = src[:largeSymbolThreshold]
		}
		sb.WriteString(src)
		texts[i] = sb.String()
	}

	// Process in batches
	for i := 0; i < len(texts); i += maxEmbeddingBatch {
		end := i + maxEmbeddingBatch
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		syms := symbols[i:end]

		embedCtx, cancel := context.WithTimeout(ctx, codeEmbeddingsTimeout)
		vecs, err := c.embedder.EmbedDocuments(embedCtx, batch)
		cancel()
		if err != nil {
			return fmt.Errorf("code: embed symbols batch: %w", err)
		}

		for j, vec := range vecs {
			if j >= len(syms) {
				break
			}
			blob := serializeFloat32(vec)
			if _, err := c.db.ExecContext(ctx, `
				UPDATE code_symbols SET embedding=? WHERE id=?`,
				blob, syms[j].ID,
			); err != nil {
				continue
			}
		}
	}

	return nil
}

// ReindexFile re-indexes a single file in a project.
func (c *CodeIndexer) ReindexFile(ctx context.Context, projectID, filePath string) error {
	// Get project root
	var rootPath string
	err := c.db.QueryRowContext(ctx, `SELECT root_path FROM code_projects WHERE project_id = ?`, projectID).Scan(&rootPath)
	if err != nil {
		return fmt.Errorf("code: get project: %w", err)
	}

	absPath := filepath.Join(rootPath, filePath)
	return c.indexFile(ctx, projectID, rootPath, absPath)
}

// GetJob returns the current status of an indexing job.
func (c *CodeIndexer) GetJob(jobID string) (*IndexingJob, bool) {
	c.jobsMu.RLock()
	defer c.jobsMu.RUnlock()
	job, ok := c.jobs[jobID]
	return job, ok
}

// ListProjects returns all indexed projects.
func (c *CodeIndexer) ListProjects(ctx context.Context) ([]*CodeProject, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT project_id, name, root_path, language_stats, last_indexed_at, indexing_status, created_at, updated_at
		FROM code_projects
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("code: list projects: %w", err)
	}
	defer rows.Close()

	var projects []*CodeProject
	for rows.Next() {
		p := &CodeProject{}
		var statsJSON string
		var lastIndexed sql.NullTime
		if err := rows.Scan(&p.ProjectID, &p.Name, &p.RootPath, &statsJSON,
			&lastIndexed, &p.IndexingStatus, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("code: scan project: %w", err)
		}
		if lastIndexed.Valid {
			p.LastIndexedAt = &lastIndexed.Time
		}
		if statsJSON != "" && statsJSON != "{}" {
			_ = json.Unmarshal([]byte(statsJSON), &p.LanguageStats)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// GetProjectStats returns statistics for a project.
func (c *CodeIndexer) GetProjectStats(ctx context.Context, projectID string) (map[string]interface{}, error) {
	var totalSymbols, totalFiles int64
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_symbols WHERE project_id = ?`, projectID).Scan(&totalSymbols)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_files WHERE project_id = ?`, projectID).Scan(&totalFiles)

	// Language breakdown
	rows, err := c.db.QueryContext(ctx, `
		SELECT language, COUNT(*) AS cnt
		FROM code_files WHERE project_id = ?
		GROUP BY language ORDER BY cnt DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("code: get stats: %w", err)
	}
	defer rows.Close()

	langs := make(map[string]int64)
	for rows.Next() {
		var lang string
		var cnt int64
		if err := rows.Scan(&lang, &cnt); err == nil {
			langs[lang] = cnt
		}
	}

	return map[string]interface{}{
		"project_id":    projectID,
		"total_files":   totalFiles,
		"total_symbols": totalSymbols,
		"languages":     langs,
	}, nil
}

// FindSymbol finds symbols matching the given query criteria.
func (c *CodeIndexer) FindSymbol(ctx context.Context, query SymbolQuery) ([]*CodeSymbol, error) {
	if query.Limit <= 0 {
		query.Limit = 50
	}

	var args []interface{}
	var filters []string
	filters = append(filters, "project_id = ?")
	args = append(args, query.ProjectID)

	if query.RelativePath != "" {
		if strings.HasSuffix(query.RelativePath, "/") {
			filters = append(filters, "file_path LIKE ?")
			args = append(args, query.RelativePath+"%")
		} else {
			filters = append(filters, "(file_path = ? OR file_path LIKE ?)")
			args = append(args, query.RelativePath, query.RelativePath+"/%")
		}
	}

	if query.NamePathPattern != "" {
		pat := query.NamePathPattern
		if strings.HasPrefix(pat, "/") {
			filters = append(filters, "name_path = ?")
			args = append(args, pat)
		} else if query.SubstringMatch {
			filters = append(filters, "name_path LIKE ?")
			args = append(args, "%"+pat+"%")
		} else {
			filters = append(filters, "(name = ? OR name_path LIKE ?)")
			args = append(args, pat, "%/"+pat)
		}
	}

	if len(query.IncludeTypes) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(query.IncludeTypes)), ",")
		filters = append(filters, "symbol_type IN ("+placeholders+")")
		for _, t := range query.IncludeTypes {
			args = append(args, string(t))
		}
	}

	if len(query.ExcludeTypes) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(query.ExcludeTypes)), ",")
		filters = append(filters, "symbol_type NOT IN ("+placeholders+")")
		for _, t := range query.ExcludeTypes {
			args = append(args, string(t))
		}
	}

	if len(query.Languages) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(query.Languages)), ",")
		filters = append(filters, "language IN ("+placeholders+")")
		for _, l := range query.Languages {
			args = append(args, string(l))
		}
	}

	where := strings.Join(filters, " AND ")
	sqlStr := `SELECT id, project_id, file_path, language, symbol_type, name, name_path,
		               start_line, end_line, parent_id, doc_string
		        FROM code_symbols WHERE ` + where + ` ORDER BY name_path LIMIT ?`
	args = append(args, query.Limit)

	rows, err := c.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("code: find symbol: %w", err)
	}
	defer rows.Close()

	var symbols []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		var parentID sql.NullString
		if err := rows.Scan(&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language,
			&sym.SymbolType, &sym.Name, &sym.NamePath,
			&sym.StartLine, &sym.EndLine, &parentID, &sym.DocString); err != nil {
			return nil, fmt.Errorf("code: scan symbol: %w", err)
		}
		if parentID.Valid {
			sym.ParentID = &parentID.String
		}
		if query.IncludeBody {
			_ = c.db.QueryRowContext(ctx, `SELECT source_code FROM code_symbols WHERE id=?`, sym.ID).Scan(&sym.SourceCode)
		}
		symbols = append(symbols, sym)
	}

	if query.Depth > 0 {
		for _, sym := range symbols {
			sym.Children = c.loadChildren(ctx, sym.ID, query.Depth-1, query.IncludeBody)
		}
	}

	return symbols, rows.Err()
}

// loadChildren recursively loads child symbols.
func (c *CodeIndexer) loadChildren(ctx context.Context, parentID string, depth int, includeBody bool) []*CodeSymbol {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, project_id, file_path, language, symbol_type, name, name_path,
		       start_line, end_line, parent_id, doc_string
		FROM code_symbols WHERE parent_id = ? ORDER BY start_line`, parentID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var children []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		var pid sql.NullString
		if err := rows.Scan(&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language,
			&sym.SymbolType, &sym.Name, &sym.NamePath,
			&sym.StartLine, &sym.EndLine, &pid, &sym.DocString); err != nil {
			continue
		}
		if pid.Valid {
			sym.ParentID = &pid.String
		}
		if includeBody {
			_ = c.db.QueryRowContext(ctx, `SELECT source_code FROM code_symbols WHERE id=?`, sym.ID).Scan(&sym.SourceCode)
		}
		if depth > 0 {
			sym.Children = c.loadChildren(ctx, sym.ID, depth-1, includeBody)
		}
		children = append(children, sym)
	}
	return children
}

// GetFileSymbols returns all symbols for a file.
func (c *CodeIndexer) GetFileSymbols(ctx context.Context, projectID, filePath string, includeBody bool) ([]*CodeSymbol, error) {
	return c.FindSymbol(ctx, SymbolQuery{
		ProjectID:    projectID,
		RelativePath: filePath,
		Limit:        10000,
		IncludeBody:  includeBody,
	})
}

// GetSymbolsOverview returns a high-level overview of symbols in a file.
func (c *CodeIndexer) GetSymbolsOverview(ctx context.Context, projectID, filePath string, maxResults int) ([]*CodeSymbol, error) {
	if maxResults <= 0 {
		maxResults = 100
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, project_id, file_path, language, symbol_type, name, name_path,
		       start_line, end_line, parent_id, doc_string
		FROM code_symbols
		WHERE project_id = ? AND file_path = ? AND parent_id IS NULL
		ORDER BY start_line
		LIMIT ?`, projectID, filePath, maxResults)
	if err != nil {
		return nil, fmt.Errorf("code: symbols overview: %w", err)
	}
	defer rows.Close()

	var symbols []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		var pid sql.NullString
		if err := rows.Scan(&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language,
			&sym.SymbolType, &sym.Name, &sym.NamePath,
			&sym.StartLine, &sym.EndLine, &pid, &sym.DocString); err != nil {
			return nil, fmt.Errorf("code: scan symbol: %w", err)
		}
		if pid.Valid {
			sym.ParentID = &pid.String
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// HybridSearch performs hybrid vector + FTS search over code symbols.
func (c *CodeIndexer) HybridSearch(ctx context.Context, projectID, query string, limit int, langs []Language, symbolTypes []SymbolType) ([]HybridSearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	subLimit := limit * 3

	type result struct {
		items []HybridSearchResult
		err   error
	}

	vecCh := make(chan result, 1)
	ftsCh := make(chan result, 1)

	go func() {
		if c.embedder == nil {
			vecCh <- result{}
			return
		}
		qvec, err := c.embedder.EmbedQuery(ctx, query)
		if err != nil {
			vecCh <- result{err: err}
			return
		}
		items, err := c.vectorSearch(ctx, projectID, qvec, subLimit, langs, symbolTypes)
		vecCh <- result{items, err}
	}()

	go func() {
		items, err := c.ftsSearch(ctx, projectID, query, subLimit, langs, symbolTypes)
		ftsCh <- result{items, err}
	}()

	vec := <-vecCh
	fts := <-ftsCh

	if vec.err != nil && fts.err != nil {
		return nil, fmt.Errorf("code: hybrid search both failed: %v, %v", vec.err, fts.err)
	}

	return rrfFuseCode(vec.items, fts.items, limit), nil
}

// vectorSearch performs vector similarity search over code symbols.
func (c *CodeIndexer) vectorSearch(ctx context.Context, projectID string, queryEmb []float32, limit int, langs []Language, symbolTypes []SymbolType) ([]HybridSearchResult, error) {
	queryNorm := l2norm(queryEmb)
	if queryNorm == 0 {
		return nil, nil
	}

	var args []interface{}
	args = append(args, projectID)
	filterClauses := "project_id = ?"

	if len(langs) > 0 {
		ph := strings.Repeat("?,", len(langs))
		filterClauses += " AND language IN (" + strings.TrimSuffix(ph, ",") + ")"
		for _, l := range langs {
			args = append(args, string(l))
		}
	}
	if len(symbolTypes) > 0 {
		ph := strings.Repeat("?,", len(symbolTypes))
		filterClauses += " AND symbol_type IN (" + strings.TrimSuffix(ph, ",") + ")"
		for _, t := range symbolTypes {
			args = append(args, string(t))
		}
	}

	rows, err := c.db.QueryContext(ctx, `
		SELECT id, project_id, file_path, language, symbol_type, name, name_path,
		       start_line, end_line, parent_id, doc_string, embedding
		FROM code_symbols
		WHERE `+filterClauses+` AND embedding IS NOT NULL`, args...)
	if err != nil {
		return nil, fmt.Errorf("code: vector search query: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		sym   *CodeSymbol
		score float64
	}
	var candidates []candidate

	for rows.Next() {
		sym := &CodeSymbol{}
		var pid sql.NullString
		var blob []byte
		if err := rows.Scan(&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language,
			&sym.SymbolType, &sym.Name, &sym.NamePath,
			&sym.StartLine, &sym.EndLine, &pid, &sym.DocString, &blob); err != nil {
			return nil, fmt.Errorf("code: scan vector: %w", err)
		}
		if pid.Valid {
			sym.ParentID = &pid.String
		}
		vec := deserializeFloat32(blob)
		if len(vec) != len(queryEmb) {
			continue
		}
		score := cosine(queryEmb, queryNorm, vec)
		candidates = append(candidates, candidate{sym, score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	results := make([]HybridSearchResult, 0, limit)
	for i, cand := range candidates {
		if i >= limit {
			break
		}
		results = append(results, HybridSearchResult{
			Symbol:      cand.sym,
			Score:       cand.score,
			VectorScore: cand.score,
			Rank:        i + 1,
		})
	}
	return results, nil
}

// ftsSearch performs full-text search over code symbols.
func (c *CodeIndexer) ftsSearch(ctx context.Context, projectID, query string, limit int, langs []Language, symbolTypes []SymbolType) ([]HybridSearchResult, error) {
	escapedQuery := strings.ReplaceAll(query, `"`, `""`)

	var args []interface{}
	args = append(args, escapedQuery)
	args = append(args, projectID)

	langFilter := ""
	if len(langs) > 0 {
		ph := strings.Repeat("?,", len(langs))
		langFilter = " AND s.language IN (" + strings.TrimSuffix(ph, ",") + ")"
		for _, l := range langs {
			args = append(args, string(l))
		}
	}
	typeFilter := ""
	if len(symbolTypes) > 0 {
		ph := strings.Repeat("?,", len(symbolTypes))
		typeFilter = " AND s.symbol_type IN (" + strings.TrimSuffix(ph, ",") + ")"
		for _, t := range symbolTypes {
			args = append(args, string(t))
		}
	}
	args = append(args, limit)

	sqlStr := `
		SELECT s.id, s.project_id, s.file_path, s.language, s.symbol_type, s.name, s.name_path,
		       s.start_line, s.end_line, s.parent_id, s.doc_string,
		       -bm25(code_symbols_fts) AS score
		FROM code_symbols_fts
		JOIN code_symbols s ON s.rowid = code_symbols_fts.rowid
		WHERE code_symbols_fts MATCH ? AND s.project_id = ?` + langFilter + typeFilter + `
		ORDER BY score DESC LIMIT ?`

	rows, err := c.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var results []HybridSearchResult
	for rows.Next() {
		sym := &CodeSymbol{}
		var pid sql.NullString
		var rawScore float64
		if err := rows.Scan(&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language,
			&sym.SymbolType, &sym.Name, &sym.NamePath,
			&sym.StartLine, &sym.EndLine, &pid, &sym.DocString, &rawScore); err != nil {
			continue
		}
		if pid.Valid {
			sym.ParentID = &pid.String
		}
		results = append(results, HybridSearchResult{
			Symbol:   sym,
			Score:    rawScore,
			FTSScore: rawScore,
			Rank:     len(results) + 1,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(results) > 0 && results[0].Score > 0 {
		max := results[0].Score
		for i := range results {
			results[i].Score /= max
			results[i].FTSScore /= max
		}
	}

	return results, nil
}

// FindReferences finds all symbols that reference the given symbol by name.
func (c *CodeIndexer) FindReferences(ctx context.Context, projectID, symbolID, symbolName string, limit int) ([]*CodeSymbol, error) {
	if limit <= 0 {
		limit = 50
	}

	if symbolName == "" && symbolID != "" {
		_ = c.db.QueryRowContext(ctx, `SELECT name FROM code_symbols WHERE id=?`, symbolID).Scan(&symbolName)
	}
	if symbolName == "" {
		return nil, fmt.Errorf("code: symbol name or ID required")
	}

	rows, err := c.db.QueryContext(ctx, `
		SELECT id, project_id, file_path, language, symbol_type, name, name_path,
		       start_line, end_line, parent_id, doc_string
		FROM code_symbols
		WHERE project_id = ? AND id != ? AND source_code LIKE ?
		ORDER BY file_path, start_line
		LIMIT ?`,
		projectID, symbolID, "%"+symbolName+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("code: find references: %w", err)
	}
	defer rows.Close()

	var symbols []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		var pid sql.NullString
		if err := rows.Scan(&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language,
			&sym.SymbolType, &sym.Name, &sym.NamePath,
			&sym.StartLine, &sym.EndLine, &pid, &sym.DocString); err != nil {
			continue
		}
		if pid.Valid {
			sym.ParentID = &pid.String
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// SearchPattern searches for text patterns in code symbols.
func (c *CodeIndexer) SearchPattern(ctx context.Context, projectID, pattern string, caseSensitive bool, isRegex bool, limit int, langs []Language, symbolTypes []SymbolType) ([]*CodeSymbol, error) {
	if limit <= 0 {
		limit = 50
	}

	_ = isRegex

	var args []interface{}
	filters := []string{"project_id = ?"}
	args = append(args, projectID)

	if len(langs) > 0 {
		ph := strings.Repeat("?,", len(langs))
		filters = append(filters, "language IN ("+strings.TrimSuffix(ph, ",")+")")
		for _, l := range langs {
			args = append(args, string(l))
		}
	}
	if len(symbolTypes) > 0 {
		ph := strings.Repeat("?,", len(symbolTypes))
		filters = append(filters, "symbol_type IN ("+strings.TrimSuffix(ph, ",")+")")
		for _, t := range symbolTypes {
			args = append(args, string(t))
		}
	}

	likePattern := "%" + strings.ReplaceAll(pattern, "%", "\\%") + "%"
	if caseSensitive {
		filters = append(filters, "source_code GLOB ?")
		args = append(args, "*"+pattern+"*")
	} else {
		filters = append(filters, "source_code LIKE ?")
		args = append(args, likePattern)
	}
	args = append(args, limit)

	sqlStr := `SELECT id, project_id, file_path, language, symbol_type, name, name_path,
		              start_line, end_line, parent_id, doc_string
		       FROM code_symbols WHERE ` + strings.Join(filters, " AND ") + ` ORDER BY file_path, start_line LIMIT ?`

	rows, err := c.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("code: pattern search: %w", err)
	}
	defer rows.Close()

	var symbols []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		var pid sql.NullString
		if err := rows.Scan(&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language,
			&sym.SymbolType, &sym.Name, &sym.NamePath,
			&sym.StartLine, &sym.EndLine, &pid, &sym.DocString); err != nil {
			continue
		}
		if pid.Valid {
			sym.ParentID = &pid.String
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// updateLanguageStats recomputes and stores language statistics for a project.
func (c *CodeIndexer) updateLanguageStats(ctx context.Context, projectID string) error {
	rows, err := c.db.QueryContext(ctx, `
		SELECT language, COUNT(*) AS cnt FROM code_files
		WHERE project_id = ? GROUP BY language`, projectID)
	if err != nil {
		return fmt.Errorf("code: language stats query: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var lang string
		var cnt int
		if err := rows.Scan(&lang, &cnt); err == nil {
			stats[lang] = cnt
		}
	}

	b, _ := json.Marshal(stats)
	_, err = c.db.ExecContext(ctx, `UPDATE code_projects SET language_stats=?, updated_at=? WHERE project_id=?`,
		string(b), time.Now().UTC(), projectID)
	return err
}

// rrfFuseCode merges two ranked result lists using RRF.
func rrfFuseCode(vecResults, ftsResults []HybridSearchResult, limit int) []HybridSearchResult {
	const rrfK = 60.0

	type entry struct {
		sym HybridSearchResult
		rrf float64
	}

	byID := make(map[string]*entry)

	for rank, r := range vecResults {
		e := &entry{sym: r}
		e.rrf += 1.0 / (rrfK + float64(rank+1))
		byID[r.Symbol.ID] = e
	}

	for rank, r := range ftsResults {
		if e, ok := byID[r.Symbol.ID]; ok {
			e.rrf += 1.0 / (rrfK + float64(rank+1))
		} else {
			e := &entry{sym: r, rrf: 1.0 / (rrfK + float64(rank+1))}
			byID[r.Symbol.ID] = e
		}
	}

	fused := make([]*entry, 0, len(byID))
	for _, e := range byID {
		fused = append(fused, e)
	}
	sort.Slice(fused, func(i, j int) bool { return fused[i].rrf > fused[j].rrf })

	results := make([]HybridSearchResult, 0, limit)
	for i, e := range fused {
		if i >= limit {
			break
		}
		r := e.sym
		r.Score = e.rrf
		r.Rank = i + 1
		results = append(results, r)
	}
	return results
}

// serializeFloat32 encodes []float32 as little-endian bytes.
func serializeFloat32(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// deserializeFloat32 decodes little-endian bytes to []float32.
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

// cosine computes cosine similarity.
func cosine(query []float32, queryNorm float64, v []float32) float64 {
	var dot, vNormSq float64
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

// l2norm computes the L2 norm.
func l2norm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}
