package code

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

// Re-export edge types for convenience.
type (
	CodeEdge = treesitter.CodeEdge
	EdgeType = treesitter.EdgeType
)

// Re-export edge-type constants.
const (
	EdgeImports = treesitter.EdgeImports
	EdgeCalls   = treesitter.EdgeCalls
	EdgeTypeRef = treesitter.EdgeTypeRef
)

// definitionSymbolTypes lists the symbol kinds treated as "definitions" when
// resolving a callee/type name back to the file that declares it. Kept in sync
// with the kinds languages actually emit for top-level declarations.
var definitionSymbolTypes = []string{
	string(treesitter.SymbolTypeFunction),
	string(treesitter.SymbolTypeMethod),
	string(treesitter.SymbolTypeConstructor),
	string(treesitter.SymbolTypeStruct),
	string(treesitter.SymbolTypeClass),
	string(treesitter.SymbolTypeInterface),
	string(treesitter.SymbolTypeTypeAlias),
	string(treesitter.SymbolTypeEnum),
	string(treesitter.SymbolTypeConstant),
}

// insertEdges writes the given edges for a file inside an open transaction.
func insertEdges(ctx context.Context, tx *sql.Tx, projectID string, fileID int64, edges []*treesitter.CodeEdge) error {
	for _, e := range edges {
		if e == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO code_edges
				(project_id, file_id, edge_type, src_file, src_symbol, dst_name, dst_path, start_line)
			VALUES (?,?,?,?,?,?,?,?)`,
			projectID, fileID, string(e.EdgeType), e.FilePath, e.SrcSymbolID, e.DstName, e.DstPath, e.StartLine,
		); err != nil {
			return fmt.Errorf("code: insert edge: %w", err)
		}
	}
	return nil
}

// ImpactCaller is a symbol that (transitively) depends on the analyzed symbol.
type ImpactCaller struct {
	Name       string `json:"name"`
	NamePath   string `json:"name_path,omitempty"`
	FilePath   string `json:"file_path"`
	StartLine  int    `json:"start_line"`
	SymbolType string `json:"symbol_type"`
	Depth      int    `json:"depth"`
}

// ImpactResult is the outcome of a reverse-call impact analysis.
type ImpactResult struct {
	Symbol    string         `json:"symbol"`
	Callers   []ImpactCaller `json:"callers"`
	Truncated bool           `json:"truncated"`
}

// ImpactAnalysis returns the symbols that call (directly or transitively, up to
// maxDepth) a symbol named symbolName — i.e. "what breaks if I change this".
//
// Resolution is name-based via the `calls` edges, which is cheap and works
// cross-file/cross-package but is approximate when a name is shared by unrelated
// symbols. Results are deduplicated by caller symbol and capped at limit.
func (c *CodeIndexer) ImpactAnalysis(ctx context.Context, projectID, symbolName string, maxDepth, limit int) (*ImpactResult, error) {
	if strings.TrimSpace(symbolName) == "" {
		return nil, fmt.Errorf("code: impact analysis requires a symbol name")
	}
	if maxDepth <= 0 {
		maxDepth = 2
	}
	if limit <= 0 {
		limit = 50
	}

	res := &ImpactResult{Symbol: symbolName}
	seenSym := map[string]bool{}    // caller symbol IDs already reported
	frontier := []string{symbolName} // names whose callers we still need
	seenName := map[string]bool{symbolName: true}

	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, name := range frontier {
			rows, err := c.db.QueryContext(ctx, `
				SELECT s.id, s.name, s.name_path, s.file_path, s.symbol_type, s.start_line
				FROM code_edges e
				JOIN code_symbols s ON s.id = e.src_symbol
				WHERE e.project_id = ? AND e.edge_type = 'calls'
				  AND e.dst_name = ? AND e.src_symbol != ''`,
				projectID, name,
			)
			if err != nil {
				return nil, fmt.Errorf("code: impact query: %w", err)
			}
			for rows.Next() {
				var id, sname, namePath, filePath, symType string
				var startLine int
				if err := rows.Scan(&id, &sname, &namePath, &filePath, &symType, &startLine); err != nil {
					rows.Close()
					return nil, fmt.Errorf("code: impact scan: %w", err)
				}
				if seenSym[id] {
					continue
				}
				seenSym[id] = true
				if len(res.Callers) >= limit {
					res.Truncated = true
					continue
				}
				res.Callers = append(res.Callers, ImpactCaller{
					Name:       sname,
					NamePath:   namePath,
					FilePath:   filePath,
					StartLine:  startLine,
					SymbolType: symType,
					Depth:      depth,
				})
				if !seenName[sname] {
					seenName[sname] = true
					next = append(next, sname)
				}
			}
			rows.Close()
		}
		frontier = next
	}
	return res, nil
}

// RelatedFile is a file related to the queried file with a weighted score.
type RelatedFile struct {
	FilePath string   `json:"file_path"`
	Score    float64  `json:"score"`
	Reasons  []string `json:"reasons"`
}

// RelatedResult is the outcome of a related-files query.
type RelatedResult struct {
	File      string        `json:"file"`
	Related   []RelatedFile `json:"related"`
	Truncated bool          `json:"truncated"`
}

// edge weights (mirroring the lean-ctx plan): imports dominate, call coupling is
// strong, type references are weaker.
const (
	weightImports = 1.0
	weightCalls   = 0.8
)

type relAcc struct {
	score   float64
	reasons map[string]bool
}

// RelatedFiles returns files related to filePath, ranked by a weighted blend of
// resolved imports and bidirectional call coupling. Import resolution is
// best-effort: relative ESM specifiers are resolved against the project's file
// list; bare/package import paths rely on call coupling instead.
func (c *CodeIndexer) RelatedFiles(ctx context.Context, projectID, filePath string, limit int) (*RelatedResult, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("code: related files requires a file path")
	}
	if limit <= 0 {
		limit = 20
	}

	acc := map[string]*relAcc{}
	add := func(file string, w float64, reason string) {
		if file == "" || file == filePath {
			return
		}
		a := acc[file]
		if a == nil {
			a = &relAcc{reasons: map[string]bool{}}
			acc[file] = a
		}
		a.score += w
		a.reasons[reason] = true
	}

	// 1. Outgoing imports → resolve relative specifiers to project files.
	projectFiles, err := c.projectFilePaths(ctx, projectID)
	if err != nil {
		return nil, err
	}
	importRows, err := c.db.QueryContext(ctx, `
		SELECT dst_path FROM code_edges
		WHERE project_id = ? AND src_file = ? AND edge_type = 'imports' AND dst_path != ''`,
		projectID, filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("code: related imports query: %w", err)
	}
	for importRows.Next() {
		var dstPath string
		if err := importRows.Scan(&dstPath); err != nil {
			importRows.Close()
			return nil, fmt.Errorf("code: related imports scan: %w", err)
		}
		if resolved, ok := resolveRelativeImport(projectFiles, filePath, dstPath); ok {
			add(resolved, weightImports, "imports")
		}
	}
	importRows.Close()

	defTypesIn := sqlInClause(len(definitionSymbolTypes))
	defArgs := make([]any, len(definitionSymbolTypes))
	for i, t := range definitionSymbolTypes {
		defArgs[i] = t
	}

	// 2. Outgoing call coupling: this file calls symbols defined elsewhere.
	outArgs := append([]any{projectID, filePath}, defArgs...)
	outArgs = append(outArgs, projectID, filePath)
	outRows, err := c.db.QueryContext(ctx, `
		SELECT s.file_path, COUNT(*) AS n
		FROM code_edges e
		JOIN code_symbols s ON s.project_id = e.project_id AND s.name = e.dst_name
		WHERE e.project_id = ? AND e.src_file = ? AND e.edge_type = 'calls'
		  AND s.symbol_type IN (`+defTypesIn+`)
		  AND s.project_id = ? AND s.file_path != ?
		GROUP BY s.file_path`,
		outArgs...,
	)
	if err != nil {
		return nil, fmt.Errorf("code: related out-calls query: %w", err)
	}
	for outRows.Next() {
		var file string
		var n int
		if err := outRows.Scan(&file, &n); err != nil {
			outRows.Close()
			return nil, fmt.Errorf("code: related out-calls scan: %w", err)
		}
		add(file, weightCalls+countBonus(n), "calls")
	}
	outRows.Close()

	// 3. Incoming call coupling: other files call symbols defined in this file.
	inArgs := append([]any{projectID, filePath, projectID, filePath}, defArgs...)
	inRows, err := c.db.QueryContext(ctx, `
		SELECT e.src_file, COUNT(*) AS n
		FROM code_edges e
		WHERE e.project_id = ? AND e.edge_type = 'calls' AND e.src_file != ?
		  AND e.dst_name IN (
			SELECT name FROM code_symbols
			WHERE project_id = ? AND file_path = ? AND symbol_type IN (`+defTypesIn+`)
		  )
		GROUP BY e.src_file`,
		inArgs...,
	)
	if err != nil {
		return nil, fmt.Errorf("code: related in-calls query: %w", err)
	}
	for inRows.Next() {
		var file string
		var n int
		if err := inRows.Scan(&file, &n); err != nil {
			inRows.Close()
			return nil, fmt.Errorf("code: related in-calls scan: %w", err)
		}
		add(file, weightCalls+countBonus(n), "calls")
	}
	inRows.Close()

	res := &RelatedResult{File: filePath}
	for file, a := range acc {
		res.Related = append(res.Related, RelatedFile{
			FilePath: file,
			Score:    round2(a.score),
			Reasons:  sortedKeys(a.reasons),
		})
	}
	sort.Slice(res.Related, func(i, j int) bool {
		if res.Related[i].Score != res.Related[j].Score {
			return res.Related[i].Score > res.Related[j].Score
		}
		return res.Related[i].FilePath < res.Related[j].FilePath
	})
	if len(res.Related) > limit {
		res.Related = res.Related[:limit]
		res.Truncated = true
	}
	return res, nil
}

// projectFilePaths returns all indexed file paths for a project.
func (c *CodeIndexer) projectFilePaths(ctx context.Context, projectID string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT file_path FROM code_files WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("code: project files query: %w", err)
	}
	defer rows.Close()
	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("code: project files scan: %w", err)
		}
		files = append(files, f)
	}
	return files, nil
}

// resolveRelativeImport resolves a relative ESM specifier (e.g. "./foo",
// "../bar/baz") written in srcFile against the set of indexed project files,
// trying common extensions and index files. Non-relative specifiers (bare module
// or package paths) are not resolved here and return ok=false.
func resolveRelativeImport(projectFiles []string, srcFile, dstPath string) (string, bool) {
	if !strings.HasPrefix(dstPath, ".") {
		return "", false
	}
	base := path.Dir(filepathToSlash(srcFile))
	joined := path.Clean(path.Join(base, dstPath))

	candidates := []string{joined}
	exts := []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
	for _, e := range exts {
		candidates = append(candidates, joined+e)
	}
	for _, e := range exts {
		candidates = append(candidates, path.Join(joined, "index"+e))
	}

	fileSet := make(map[string]bool, len(projectFiles))
	for _, f := range projectFiles {
		fileSet[filepathToSlash(f)] = true
	}
	for _, cand := range candidates {
		if fileSet[cand] {
			// Return the original (OS-style) path as stored.
			for _, f := range projectFiles {
				if filepathToSlash(f) == cand {
					return f, true
				}
			}
		}
	}
	return "", false
}

// filepathToSlash normalizes a path to forward slashes for cross-platform
// comparison of stored relative paths.
func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// countBonus turns a coupling edge count into a small, capped score bonus so
// more-coupled files rank above barely-coupled ones without dominating imports.
func countBonus(n int) float64 {
	if n <= 1 {
		return 0
	}
	b := 0.02 * float64(n-1)
	if b > 0.18 {
		b = 0.18
	}
	return b
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sqlInClause returns "?,?,..." with n placeholders for an IN (...) clause.
func sqlInClause(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
