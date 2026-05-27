// Package code provides tree-sitter based code indexing with semantic search.
package code

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/logging"
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

var (
	errCodeIgnorePath = errors.New("code: ignore path")
	osReadFileForTest = os.ReadFile
)

const codeSymbolSelectColumns = `id, project_id, file_path, language, symbol_type, name, name_path,
	start_line, end_line, start_byte, end_byte, source_code, signature, doc_string, parent_id, metadata, created_at, updated_at`

var codeSearchStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "but": {}, "by": {},
	"can": {}, "could": {}, "did": {}, "do": {}, "does": {}, "for": {}, "from": {}, "had": {},
	"has": {}, "have": {}, "how": {}, "i": {}, "if": {}, "in": {}, "into": {}, "is": {},
	"it": {}, "like": {}, "may": {}, "me": {}, "my": {}, "not": {}, "of": {}, "on": {},
	"or": {}, "our": {}, "please": {}, "should": {}, "show": {}, "so": {}, "than": {},
	"that": {}, "the": {}, "their": {}, "them": {}, "these": {}, "this": {}, "those": {},
	"to": {}, "try": {}, "use": {}, "using": {}, "what": {}, "when": {}, "where": {},
	"which": {}, "who": {}, "why": {}, "with": {}, "would": {}, "you": {}, "your": {},
}

// CodeIndexer manages code indexing projects using tree-sitter for parsing
// and embeddings for semantic search.
type CodeIndexer struct {
	db       *sql.DB
	embedder embeddings.Embedder
	proxy    *dbproxy.DBProxy
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

// SetWriteProxy configures a DB proxy for mutating operations.
func (c *CodeIndexer) SetWriteProxy(proxy *dbproxy.DBProxy) {
	c.proxy = proxy
}

// IndexProject indexes all supported source files in a project directory.
// It runs asynchronously and updates the job status in the jobs map.
// Returns the job ID immediately.
func (c *CodeIndexer) IndexProject(ctx context.Context, projectID, projectPath string, languages []Language) (string, error) {
	jobID := uuid.New().String()
	// Upsert project record
	now := time.Now().UTC()
	projectName := filepath.Base(projectPath)
	if c.proxy != nil {
		if err := c.proxy.WriteWithRetry(ctx, "CodeUpsertProject", codeUpsertProjectRequest{
			ProjectID:  projectID,
			Name:       projectName,
			RootPath:   projectPath,
			Status:     string(IndexingStatusInProgress),
			CreatedAt:  now,
			UpdatedAt:  now,
			JobID:      jobID,
			Languages:  languages,
		}, dbproxy.DefaultWriteTimeouts.Default); err != nil {
			return "", fmt.Errorf("code: upsert project: %w", err)
		}
	} else {
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
	}

	// Create job
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

			updatedAt := time.Now().UTC()
			if c.proxy != nil {
				_ = c.proxy.WriteWithRetry(bgCtx, "CodeSetProjectStatus", codeSetProjectStatusRequest{
					ProjectID: projectID,
					Status:    string(IndexingStatusFailed),
					UpdatedAt: updatedAt,
				}, dbproxy.DefaultWriteTimeouts.Default)
			} else {
				c.db.ExecContext(bgCtx, `UPDATE code_projects SET indexing_status='failed', updated_at=? WHERE project_id=?`, updatedAt, projectID)
			}
		} else {
			c.jobsMu.Lock()
			job.Status = IndexingStatusCompleted
			completedAt := time.Now().UTC()
			job.CompletedAt = &completedAt
			c.jobsMu.Unlock()

			lastIndexed := time.Now().UTC()
			updatedAt := time.Now().UTC()
			if c.proxy != nil {
				_ = c.proxy.WriteWithRetry(bgCtx, "CodeSetProjectStatus", codeSetProjectStatusRequest{
					ProjectID:     projectID,
					Status:        string(IndexingStatusCompleted),
					LastIndexedAt: &lastIndexed,
					UpdatedAt:     updatedAt,
				}, dbproxy.DefaultWriteTimeouts.Default)
			} else {
				c.db.ExecContext(bgCtx, `UPDATE code_projects SET indexing_status='completed', last_indexed_at=?, updated_at=? WHERE project_id=?`,
					lastIndexed, updatedAt, projectID)
			}
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
			if shouldIgnoreCodePathError(err) {
				c.appendJobWarning(job, pathWarning(path, fmt.Errorf("walk skipped: %w", err)))
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
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
			job.Warnings = append(job.Warnings, pathWarning(relPath, result.err))
		}
		c.jobsMu.Unlock()
	}

	// Update language stats
	return c.updateLanguageStats(ctx, job.ProjectID)
}

func (c *CodeIndexer) appendJobWarning(job *IndexingJob, warning string) {
	if job == nil || strings.TrimSpace(warning) == "" {
		return
	}

	c.jobsMu.Lock()
	defer c.jobsMu.Unlock()
	if len(job.Warnings) >= maxJobWarnings {
		return
	}
	job.Warnings = append(job.Warnings, warning)
}

func shouldIgnoreCodePathError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, os.ErrPermission)
}

func pathWarning(path string, err error) string {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return fmt.Sprintf("index warning: %v", err)
	}
	return fmt.Sprintf("index warning (%s): %v", trimmedPath, err)
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
	content, err := osReadFileForTest(filePath)
	if err != nil {
		if shouldIgnoreCodePathError(err) {
			return fmt.Errorf("%w: read file %s: %w", errCodeIgnorePath, filePath, err)
		}
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

	if c.proxy != nil {
		return c.proxy.WriteWithRetry(ctx, "CodeIndexFile", codeIndexFileRequest{
			ProjectID: projectID,
			RootPath:  rootPath,
			FilePath:  relPath,
			Language:  string(lang),
			FileHash:  hash,
			Symbols:   symbols,
		}, dbproxy.DefaultWriteTimeouts.Long)
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

// HasProject reports whether a project is already registered for code indexing
// and whether the stored root path matches the requested one.
func (c *CodeIndexer) HasProject(ctx context.Context, projectID, rootPath string) (bool, error) {
	if strings.TrimSpace(projectID) == "" {
		return false, fmt.Errorf("code: project_id is required")
	}

	var existingRoot string
	err := c.db.QueryRowContext(ctx, `SELECT root_path FROM code_projects WHERE project_id = ?`, projectID).Scan(&existingRoot)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("code: get project: %w", err)
	}

	return filepath.Clean(existingRoot) == filepath.Clean(rootPath), nil
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
	err = c.indexFile(ctx, projectID, rootPath, absPath)
	if shouldIgnoreCodePathError(err) || errors.Is(err, errCodeIgnorePath) {
		return nil
	}
	return err
}

// DeleteFile removes a single indexed file and all related indexed symbols.
func (c *CodeIndexer) DeleteFile(ctx context.Context, projectID, filePath string) error {
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("code: project_id is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("code: file_path is required")
	}

	res, err := c.db.ExecContext(ctx, `DELETE FROM code_files WHERE project_id = ? AND file_path = ?`, projectID, filePath)
	if err != nil {
		return fmt.Errorf("code: delete file: %w", err)
	}

	if rows, rowsErr := res.RowsAffected(); rowsErr == nil && rows == 0 {
		return os.ErrNotExist
	}

	if err := c.updateLanguageStats(ctx, projectID); err != nil {
		return fmt.Errorf("code: update language stats after delete: %w", err)
	}
	return nil
}

// DeleteProject removes an indexed project and all related indexed data.
func (c *CodeIndexer) DeleteProject(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("code: project_id is required")
	}
	if c.proxy != nil {
		if err := c.proxy.WriteWithRetry(ctx, "CodeDeleteProject", projectID, dbproxy.DefaultWriteTimeouts.Default); err != nil {
			return err
		}
		c.jobsMu.Lock()
		for id, job := range c.jobs {
			if job.ProjectID == projectID {
				delete(c.jobs, id)
			}
		}
		c.jobsMu.Unlock()
		return nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("code: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM code_projects WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("code: delete project: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("code: commit delete project: %w", err)
	}

	c.jobsMu.Lock()
	for id, job := range c.jobs {
		if job.ProjectID == projectID {
			delete(c.jobs, id)
		}
	}
	c.jobsMu.Unlock()

	return nil
}

type codeUpsertProjectRequest struct {
	ProjectID string     `json:"project_id"`
	Name      string     `json:"name"`
	RootPath  string     `json:"root_path"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	JobID     string     `json:"job_id,omitempty"`
	Languages []Language `json:"languages,omitempty"`
}

type codeSetProjectStatusRequest struct {
	ProjectID     string     `json:"project_id"`
	Status        string     `json:"status"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type codeIndexFileRequest struct {
	ProjectID string                    `json:"project_id"`
	RootPath  string                    `json:"root_path"`
	FilePath  string                    `json:"file_path"`
	Language  string                    `json:"language"`
	FileHash  string                    `json:"file_hash"`
	Symbols   []*treesitter.CodeSymbol  `json:"symbols"`
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

func selectCodeSymbolColumns(alias string) string {
	prefix := alias + "."
	return prefix + "id, " + prefix + "project_id, " + prefix + "file_path, " + prefix + "language, " + prefix + "symbol_type, " +
		prefix + "name, " + prefix + "name_path, " + prefix + "start_line, " + prefix + "end_line, " +
		prefix + "start_byte, " + prefix + "end_byte, " + prefix + "source_code, " + prefix + "signature, " +
		prefix + "doc_string, " + prefix + "parent_id, " + prefix + "metadata, " + prefix + "created_at, " + prefix + "updated_at"
}

func scanCodeSymbolRow(rows *sql.Rows, sym *CodeSymbol) error {
	var parentID sql.NullString
	var metadataJSON sql.NullString
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	if err := rows.Scan(
		&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language, &sym.SymbolType,
		&sym.Name, &sym.NamePath, &sym.StartLine, &sym.EndLine,
		&sym.StartByte, &sym.EndByte, &sym.SourceCode, &sym.Signature, &sym.DocString,
		&parentID, &metadataJSON, &createdAt, &updatedAt,
	); err != nil {
		return err
	}
	if parentID.Valid {
		sym.ParentID = &parentID.String
	}
	if metadataJSON.Valid && metadataJSON.String != "" && metadataJSON.String != "{}" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &sym.Metadata); err != nil {
			sym.Metadata = nil
		}
	}
	if createdAt.Valid {
		sym.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		sym.UpdatedAt = updatedAt.Time
	}
	return nil
}

func scanCodeSymbolRowWithEmbedding(rows *sql.Rows, sym *CodeSymbol, blob *[]byte) error {
	var parentID sql.NullString
	var metadataJSON sql.NullString
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	if err := rows.Scan(
		&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language, &sym.SymbolType,
		&sym.Name, &sym.NamePath, &sym.StartLine, &sym.EndLine,
		&sym.StartByte, &sym.EndByte, &sym.SourceCode, &sym.Signature, &sym.DocString,
		&parentID, &metadataJSON, &createdAt, &updatedAt, blob,
	); err != nil {
		return err
	}
	if parentID.Valid {
		sym.ParentID = &parentID.String
	}
	if metadataJSON.Valid && metadataJSON.String != "" && metadataJSON.String != "{}" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &sym.Metadata); err != nil {
			sym.Metadata = nil
		}
	}
	if createdAt.Valid {
		sym.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		sym.UpdatedAt = updatedAt.Time
	}
	return nil
}

func scanCodeSymbolRowWithScore(rows *sql.Rows, sym *CodeSymbol, score *float64) error {
	var parentID sql.NullString
	var metadataJSON sql.NullString
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	if err := rows.Scan(
		&sym.ID, &sym.ProjectID, &sym.FilePath, &sym.Language, &sym.SymbolType,
		&sym.Name, &sym.NamePath, &sym.StartLine, &sym.EndLine,
		&sym.StartByte, &sym.EndByte, &sym.SourceCode, &sym.Signature, &sym.DocString,
		&parentID, &metadataJSON, &createdAt, &updatedAt, score,
	); err != nil {
		return err
	}
	if parentID.Valid {
		sym.ParentID = &parentID.String
	}
	if metadataJSON.Valid && metadataJSON.String != "" && metadataJSON.String != "{}" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &sym.Metadata); err != nil {
			sym.Metadata = nil
		}
	}
	if createdAt.Valid {
		sym.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		sym.UpdatedAt = updatedAt.Time
	}
	return nil
}

func buildCodeSearchTerms(query string) []string {
	tokens := regexp.MustCompile(`[A-Za-z0-9_]+`).FindAllString(query, -1)
	if len(tokens) == 0 {
		return nil
	}

	terms := make([]string, 0, len(tokens)*2)
	seen := make(map[string]struct{}, len(tokens)*2)
	for _, token := range tokens {
		lower := strings.ToLower(token)
		if lower == "" {
			continue
		}
		if _, stop := codeSearchStopwords[lower]; stop {
			continue
		}

		add := func(term string) {
			term = strings.TrimSpace(strings.ToLower(term))
			if term == "" {
				return
			}
			if _, ok := seen[term]; ok {
				return
			}
			seen[term] = struct{}{}
			terms = append(terms, term)
		}

		add(lower)

		if strings.Contains(lower, "_") {
			for _, part := range strings.Split(lower, "_") {
				add(part)
			}
		}
	}

	if len(terms) > 8 {
		terms = terms[:8]
	}
	return terms
}

func countCodeTermMatches(text string, terms []string) int {
	if text == "" || len(terms) == 0 {
		return 0
	}

	lower := strings.ToLower(text)
	matches := 0
	for _, term := range terms {
		if term != "" && strings.Contains(lower, term) {
			matches++
		}
	}
	return matches
}

func buildCodeFTSQuery(query string) string {
	terms := buildCodeSearchTerms(query)
	if len(terms) == 0 {
		return sanitizeFTSQuery(query)
	}

	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, term)
	}
	return strings.Join(parts, " OR ")
}

func lexicalBoost(symbol *CodeSymbol, terms []string) float64 {
	if symbol == nil || len(terms) == 0 {
		return 0
	}

	fields := []struct {
		text  string
		boost float64
	}{
		{text: strings.ToLower(symbol.Name), boost: 0.35},
		{text: strings.ToLower(symbol.NamePath), boost: 0.5},
		{text: strings.ToLower(symbol.FilePath), boost: 0.2},
		{text: strings.ToLower(symbol.DocString), boost: 0.1},
		{text: strings.ToLower(symbol.Signature), boost: 0.15},
		{text: strings.ToLower(symbol.SourceCode), boost: 0.18},
	}

	score := 0.0
	for _, term := range terms {
		for _, field := range fields {
			if field.text != "" && strings.Contains(field.text, term) {
				score += field.boost
			}
		}
	}

	nameMatches := countCodeTermMatches(symbol.Name, terms)
	namePathMatches := countCodeTermMatches(symbol.NamePath, terms)
	signatureMatches := countCodeTermMatches(symbol.Signature, terms)
	sourceMatches := countCodeTermMatches(symbol.SourceCode, terms)

	if nameMatches > 0 {
		score += 0.18 * float64(nameMatches)
	}
	if namePathMatches > 0 {
		score += 0.28 * float64(namePathMatches)
	}
	if signatureMatches > 0 {
		score += 0.08 * float64(signatureMatches)
	}
	if sourceMatches >= 2 {
		score += 0.06 * float64(sourceMatches-1)
	}

	if nameMatches+namePathMatches+signatureMatches >= 2 {
		score += 0.25
	}
	if nameMatches == 0 && namePathMatches == 0 && signatureMatches == 0 && sourceMatches > 0 {
		score -= 0.12
	}
	if strings.HasSuffix(strings.ToLower(symbol.FilePath), ".md") {
		score -= 0.2
	}
	if symbol.SymbolType == SymbolType("namespace") {
		score -= 0.1
	}
	return score
}

func boostHybridResults(results []HybridSearchResult, terms []string) {
	for i := range results {
		results[i].Score += lexicalBoost(results[i].Symbol, terms)
		if results[i].FTSScore > 0 {
			results[i].Score += 0.35 + (results[i].FTSScore * 0.35)
		}
		if results[i].VectorScore > 0 && results[i].FTSScore == 0 {
			results[i].Score += results[i].VectorScore * 0.05
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Rank < results[j].Rank
		}
		return results[i].Score > results[j].Score
	})
	for i := range results {
		results[i].Rank = i + 1
	}
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
	sqlStr := `SELECT ` + selectCodeSymbolColumns("code_symbols") + `
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
		if err := scanCodeSymbolRow(rows, sym); err != nil {
			return nil, fmt.Errorf("code: scan symbol: %w", err)
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
		SELECT `+selectCodeSymbolColumns("code_symbols")+`
		FROM code_symbols WHERE parent_id = ? ORDER BY start_line`, parentID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var children []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		if err := scanCodeSymbolRow(rows, sym); err != nil {
			continue
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
		SELECT `+selectCodeSymbolColumns("code_symbols")+`
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
		if err := scanCodeSymbolRow(rows, sym); err != nil {
			return nil, fmt.Errorf("code: scan symbol: %w", err)
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

	fused := rrfFuseCode(vec.items, fts.items, subLimit)
	boostHybridResults(fused, buildCodeSearchTerms(query))
	if len(fused) > limit {
		fused = fused[:limit]
	}
	return fused, nil
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
		SELECT `+selectCodeSymbolColumns("code_symbols")+`, embedding
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
		var blob []byte
		if err := scanCodeSymbolRowWithEmbedding(rows, sym, &blob); err != nil {
			return nil, fmt.Errorf("code: scan vector: %w", err)
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

// sanitizeFTSQuery converts a natural language query to a safe FTS5 MATCH expression.
// Each word is wrapped in double quotes to prevent FTS5 syntax errors from
// special characters such as (, ), *, ^, AND, OR, NOT.
func sanitizeFTSQuery(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return ""
	}
	parts := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ReplaceAll(w, `"`, `""`)
		if w != "" {
			parts = append(parts, `"`+w+`"`)
		}
	}
	return strings.Join(parts, " ")
}

// ftsSearch performs full-text search over code symbols.
func (c *CodeIndexer) ftsSearch(ctx context.Context, projectID, query string, limit int, langs []Language, symbolTypes []SymbolType) ([]HybridSearchResult, error) {
	escapedQuery := buildCodeFTSQuery(query)
	if escapedQuery == "" {
		return nil, nil
	}

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
		SELECT ` + selectCodeSymbolColumns("s") + `,
		       -bm25(code_symbols_fts, 5.0, 4.0, 2.0, 1.0) AS score
		FROM code_symbols_fts
		JOIN code_symbols s ON s.rowid = code_symbols_fts.rowid
		WHERE code_symbols_fts MATCH ? AND s.project_id = ?` + langFilter + typeFilter + `
		ORDER BY score DESC LIMIT ?`

	rows, err := c.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		logging.Debug("code: fts search query failed (non-fatal)", "error", err)
		return nil, nil
	}
	defer rows.Close()

	var results []HybridSearchResult
	for rows.Next() {
		sym := &CodeSymbol{}
		var rawScore float64
		if err := scanCodeSymbolRowWithScore(rows, sym, &rawScore); err != nil {
			continue
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
		SELECT `+selectCodeSymbolColumns("code_symbols")+`
		FROM code_symbols
		WHERE project_id = ? AND id != ? AND (
			source_code LIKE ? OR name LIKE ? OR name_path LIKE ? OR doc_string LIKE ? OR signature LIKE ? OR file_path LIKE ?
		)
		ORDER BY file_path, start_line
		LIMIT ?`,
		projectID, symbolID,
		"%"+symbolName+"%", "%"+symbolName+"%", "%"+symbolName+"%", "%"+symbolName+"%", "%"+symbolName+"%", "%"+symbolName+"%",
		limit)
	if err != nil {
		return nil, fmt.Errorf("code: find references: %w", err)
	}
	defer rows.Close()

	var symbols []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		if err := scanCodeSymbolRow(rows, sym); err != nil {
			continue
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

	sqlStr := `SELECT ` + selectCodeSymbolColumns("code_symbols") + `
		FROM code_symbols WHERE ` + strings.Join(filters, " AND ") + ` ORDER BY file_path, start_line`

	rows, err := c.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("code: pattern search: %w", err)
	}
	defer rows.Close()

	var matcher *regexp.Regexp
	if isRegex {
		expr := pattern
		if !caseSensitive && !strings.HasPrefix(expr, "(?i)") {
			expr = "(?i)" + expr
		}
		matcher, err = regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("code: compile regex: %w", err)
		}
	}

	var symbols []*CodeSymbol
	for rows.Next() {
		sym := &CodeSymbol{}
		if err := scanCodeSymbolRow(rows, sym); err != nil {
			continue
		}
		matched := false
		if isRegex {
			haystack := strings.Join([]string{
				sym.FilePath, sym.Name, sym.NamePath, sym.Signature, sym.DocString, sym.SourceCode,
			}, "\n")
			if len(sym.Metadata) > 0 {
				if b, err := json.Marshal(sym.Metadata); err == nil {
					haystack += "\n" + string(b)
				}
			}
			matched = matcher.MatchString(haystack)
		} else {
			needle := pattern
			if !caseSensitive {
				needle = strings.ToLower(needle)
			}
			fields := []string{sym.FilePath, sym.Name, sym.NamePath, sym.Signature, sym.DocString, sym.SourceCode}
			if len(sym.Metadata) > 0 {
				if b, err := json.Marshal(sym.Metadata); err == nil {
					fields = append(fields, string(b))
				}
			}
			for _, field := range fields {
				if field == "" {
					continue
				}
				if !caseSensitive {
					field = strings.ToLower(field)
				}
				if strings.Contains(field, needle) {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}
		symbols = append(symbols, sym)
		if len(symbols) >= limit {
			break
		}
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

// ---- Primary-side direct write methods (called by IPC dispatcher) ----

// UpsertProjectDirect inserts or updates a code project record.
// Called by the primary IPC dispatcher when a secondary forwards a CodeUpsertProject write.
func (c *CodeIndexer) UpsertProjectDirect(ctx context.Context, projectID, name, rootPath, status string, createdAt, updatedAt time.Time) error {
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO code_projects (project_id, name, root_path, indexing_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			name = excluded.name,
			root_path = excluded.root_path,
			indexing_status = excluded.indexing_status,
			updated_at = excluded.updated_at`,
		projectID, name, rootPath, status, createdAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("code: upsert project direct: %w", err)
	}
	return nil
}

// SetProjectStatusDirect updates the indexing status of an existing project.
// Called by the primary IPC dispatcher when a secondary forwards a CodeSetProjectStatus write.
func (c *CodeIndexer) SetProjectStatusDirect(ctx context.Context, projectID, status string, lastIndexedAt *time.Time, updatedAt time.Time) error {
	var err error
	if lastIndexedAt != nil {
		_, err = c.db.ExecContext(ctx,
			`UPDATE code_projects SET indexing_status=?, last_indexed_at=?, updated_at=? WHERE project_id=?`,
			status, lastIndexedAt, updatedAt, projectID,
		)
	} else {
		_, err = c.db.ExecContext(ctx,
			`UPDATE code_projects SET indexing_status=?, updated_at=? WHERE project_id=?`,
			status, updatedAt, projectID,
		)
	}
	if err != nil {
		return fmt.Errorf("code: set project status direct: %w", err)
	}
	return nil
}

// IndexFileDirect inserts or updates a file and its symbols, then generates embeddings.
// Called by the primary IPC dispatcher when a secondary forwards a CodeIndexFile write.
// symbolsJSON is the JSON-encoded []*treesitter.CodeSymbol payload from the RPC request.
// Symbol embeddings are generated on the primary using its configured embedder.
func (c *CodeIndexer) IndexFileDirect(ctx context.Context, projectID, filePath, language, fileHash string, symbolsJSON json.RawMessage) error {
	var symbols []*treesitter.CodeSymbol
	if len(symbolsJSON) > 0 && string(symbolsJSON) != "null" {
		if err := json.Unmarshal(symbolsJSON, &symbols); err != nil {
			return fmt.Errorf("code: unmarshal symbols direct: %w", err)
		}
	}

	var fileID int64
	var existingHash string
	err := c.db.QueryRowContext(ctx, `
		SELECT id, file_hash FROM code_files
		WHERE project_id = ? AND file_path = ?`,
		projectID, filePath,
	).Scan(&fileID, &existingHash)

	if err == nil && existingHash == fileHash {
		return nil // No change
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("code: begin tx direct: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC()

	if fileID > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM code_symbols WHERE file_id = ?`, fileID); err != nil {
			return fmt.Errorf("code: delete old symbols direct: %w", err)
		}
		if _, err = tx.ExecContext(ctx,
			`UPDATE code_files SET file_hash=?, language=?, symbols_count=?, indexed_at=? WHERE id=?`,
			fileHash, language, len(symbols), now, fileID,
		); err != nil {
			return fmt.Errorf("code: update file direct: %w", err)
		}
	} else {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO code_files (project_id, file_path, language, file_hash, symbols_count, indexed_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			projectID, filePath, language, fileHash, len(symbols), now,
		)
		if err != nil {
			return fmt.Errorf("code: insert file direct: %w", err)
		}
		fileID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("code: file last insert id direct: %w", err)
		}
	}

	for _, sym := range symbols {
		metaJSON := "{}"
		if len(sym.Metadata) > 0 {
			if b, e := json.Marshal(sym.Metadata); e == nil {
				metaJSON = string(b)
			}
		}

		if _, err := tx.ExecContext(ctx, `
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
		); err != nil {
			return fmt.Errorf("code: insert symbol direct %s: %w", sym.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("code: commit direct: %w", err)
	}

	return c.embedSymbols(ctx, projectID, fileID, symbols)
}

// DeleteProjectDirect removes an indexed project and all related data.
// Called by the primary IPC dispatcher when a secondary forwards a CodeDeleteProject write.
func (c *CodeIndexer) DeleteProjectDirect(ctx context.Context, projectID string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("code: begin tx delete project direct: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM code_projects WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("code: delete project direct: %w", err)
	}

	return tx.Commit()
}

// DeleteFileDirect removes a single indexed file and all related symbols.
// Called by the primary IPC dispatcher when a secondary forwards a CodeDeleteFile write.
func (c *CodeIndexer) DeleteFileDirect(ctx context.Context, projectID, filePath string) error {
	if _, err := c.db.ExecContext(ctx,
		`DELETE FROM code_files WHERE project_id = ? AND file_path = ?`,
		projectID, filePath,
	); err != nil {
		return fmt.Errorf("code: delete file direct: %w", err)
	}
	return c.updateLanguageStats(ctx, projectID)
}

// UpdateLanguageStatsDirect recomputes and persists language statistics for a project.
// Called by the primary IPC dispatcher when a secondary forwards a CodeUpdateLanguageStats write.
func (c *CodeIndexer) UpdateLanguageStatsDirect(ctx context.Context, projectID string) error {
	return c.updateLanguageStats(ctx, projectID)
}
