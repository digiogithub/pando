package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/treesitter"
	"github.com/digiogithub/pando/internal/search"
)

// Code indexing tool names
const (
	codeIndexProjectToolName    = "code_index_project"
	codeIndexStatusToolName     = "code_index_status"
	codeHybridSearchToolName    = "code_hybrid_search"
	codeFindSymbolToolName      = "code_find_symbol"
	codeGetSymbolsOverviewName  = "code_get_symbols_overview"
	codeGetProjectStatsToolName = "code_get_project_stats"
	codeDeleteProjectToolName   = "code_delete_project"
	codeReindexFileToolName     = "code_reindex_file"
	codeListProjectsToolName    = "code_list_projects"
	codeSearchPatternToolName   = "code_search_pattern"
	codeFindReferencesToolName  = "code_find_references"
)

type codeToolBase struct {
	indexer *code.CodeIndexer
}

// maxSearchFieldLen caps the length of textual fields emitted in code search
// results. Defense in depth against pathological symbols (e.g. markdown doc
// sections) whose name/name_path/signature would otherwise bloat the context
// window. Rune-aware so it never splits a UTF-8 character.
const maxSearchFieldLen = 200

// truncateField shortens s to at most maxSearchFieldLen runes, appending an
// ellipsis when truncated.
func truncateField(s string) string {
	if len(s) <= maxSearchFieldLen {
		return s
	}
	r := []rune(s)
	if len(r) <= maxSearchFieldLen {
		return s
	}
	return string(r[:maxSearchFieldLen]) + "…"
}

// defaultMinScoreRatio drops hybrid search results whose normalized relevance
// (0..1 relative to the top hit) falls below this fraction, trimming the
// low-signal tail. The top result is always kept. Overridable per call via the
// min_score parameter.
const defaultMinScoreRatio = 0.15

// docFileExts identifies documentation files whose symbols are downranked and,
// by default, excluded from code_hybrid_search results (they bloat context and
// the agent usually already has them).
var docFileExts = map[string]bool{
	".md": true, ".markdown": true, ".mdx": true,
	".rst": true, ".txt": true, ".adoc": true, ".asciidoc": true,
}

// isDocFile reports whether path points to a documentation (non-code) file.
func isDocFile(path string) bool {
	dot := strings.LastIndexByte(path, '.')
	if dot < 0 {
		return false
	}
	return docFileExts[strings.ToLower(path[dot:])]
}

// matchSnippet returns a single short line of context for a pattern match: the
// first source line containing the pattern, falling back to the signature or
// the first non-empty source line. The result is trimmed and length-capped.
func matchSnippet(sym *code.CodeSymbol, pattern string, caseSensitive bool) string {
	needle := pattern
	if !caseSensitive {
		needle = strings.ToLower(pattern)
	}
	var firstNonEmpty string
	for _, line := range strings.Split(sym.SourceCode, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if firstNonEmpty == "" {
			firstNonEmpty = trimmed
		}
		hay := trimmed
		if !caseSensitive {
			hay = strings.ToLower(trimmed)
		}
		if needle != "" && strings.Contains(hay, needle) {
			return truncateField(trimmed)
		}
	}
	if sym.Signature != "" {
		return truncateField(strings.TrimSpace(sym.Signature))
	}
	return truncateField(firstNonEmpty)
}

// CodeIndexProjectTool starts indexing a code project.
type CodeIndexProjectTool struct{ codeToolBase }

// CodeIndexStatusTool returns the status of an indexing job.
type CodeIndexStatusTool struct{ codeToolBase }

// CodeHybridSearchTool performs hybrid semantic + FTS search over code symbols.
type CodeHybridSearchTool struct{ codeToolBase }

// CodeFindSymbolTool finds symbols by name pattern.
type CodeFindSymbolTool struct{ codeToolBase }

// CodeGetSymbolsOverviewTool returns a high-level overview of symbols in a file.
type CodeGetSymbolsOverviewTool struct{ codeToolBase }

// CodeGetProjectStatsTool returns statistics for an indexed project.
type CodeGetProjectStatsTool struct{ codeToolBase }

// CodeDeleteProjectTool deletes an indexed project and its data.
type CodeDeleteProjectTool struct{ codeToolBase }

// CodeReindexFileTool re-indexes a single file.
type CodeReindexFileTool struct{ codeToolBase }

// CodeListProjectsTool lists all indexed projects.
type CodeListProjectsTool struct{ codeToolBase }

// CodeSearchPatternTool searches for text patterns in code symbols.
type CodeSearchPatternTool struct{ codeToolBase }

// CodeFindReferencesTool finds references to a symbol across the indexed codebase.
type CodeFindReferencesTool struct{ codeToolBase }

// Constructors

func NewCodeIndexProjectTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeIndexProjectTool{codeToolBase{indexer}}
}
func NewCodeIndexStatusTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeIndexStatusTool{codeToolBase{indexer}}
}
func NewCodeHybridSearchTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeHybridSearchTool{codeToolBase{indexer}}
}
func NewCodeFindSymbolTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeFindSymbolTool{codeToolBase{indexer}}
}
func NewCodeGetSymbolsOverviewTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeGetSymbolsOverviewTool{codeToolBase{indexer}}
}
func NewCodeGetProjectStatsTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeGetProjectStatsTool{codeToolBase{indexer}}
}
func NewCodeDeleteProjectTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeDeleteProjectTool{codeToolBase{indexer}}
}
func NewCodeReindexFileTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeReindexFileTool{codeToolBase{indexer}}
}
func NewCodeListProjectsTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeListProjectsTool{codeToolBase{indexer}}
}
func NewCodeSearchPatternTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeSearchPatternTool{codeToolBase{indexer}}
}
func NewCodeFindReferencesTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeFindReferencesTool{codeToolBase{indexer}}
}

// ---- CodeIndexProjectTool ----

func (t *CodeIndexProjectTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeIndexProjectToolName,
		Description: "Starts indexing a code project directory using tree-sitter for symbol extraction and embeddings for semantic search. Returns a job ID to track progress with code_index_status.",
		Parameters: map[string]any{
			"project_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the project directory to index.",
			},
			"project_name": map[string]any{
				"type":        "string",
				"description": "Human-readable project name. If omitted, uses the directory name.",
			},
			"languages": map[string]any{
				"type":        "array",
				"description": "Optional list of programming languages to index (e.g. [\"go\", \"typescript\"]). If omitted, indexes all supported languages.",
				"items":       map[string]any{"type": "string"},
			},
		},
		Required: []string{"project_path"},
	}
}

func (t *CodeIndexProjectTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectPath string   `json:"project_path"`
		ProjectName string   `json:"project_name"`
		Languages   []string `json:"languages"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectPath == "" {
		return NewTextErrorResponse("project_path is required"), nil
	}

	// Derive project ID from path (sanitized)
	projectID := sanitizeProjectID(req.ProjectPath)
	if req.ProjectName != "" {
		projectID = sanitizeProjectID(req.ProjectName)
	}

	var langs []code.Language
	for _, l := range req.Languages {
		lang := treesitter.Language(l)
		if treesitter.IsLanguageSupported(lang) {
			langs = append(langs, lang)
		}
	}

	jobID, err := t.indexer.IndexProject(ctx, projectID, req.ProjectPath, langs)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("index project error: %v", err)), nil
	}

	return NewStructuredResponse(map[string]any{
		"job_id":       jobID,
		"project_id":   projectID,
		"project_path": req.ProjectPath,
		"status":       "in_progress",
		"message":      "Indexing started. Use code_index_status with the job_id to track progress.",
	}), nil
}

// ---- CodeIndexStatusTool ----

func (t *CodeIndexStatusTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeIndexStatusToolName,
		Description: "Returns the current status of a code indexing job started by code_index_project.",
		Parameters: map[string]any{
			"job_id": map[string]any{
				"type":        "string",
				"description": "The job ID returned by code_index_project.",
			},
		},
		Required: []string{"job_id"},
	}
}

func (t *CodeIndexStatusTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		JobID string `json:"job_id"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.JobID == "" {
		return NewTextErrorResponse("job_id is required"), nil
	}

	job, ok := t.indexer.GetJob(req.JobID)
	if !ok {
		return NewTextResponse(fmt.Sprintf("Job not found: %s", req.JobID)), nil
	}

	result := map[string]any{
		"job_id":        job.ID,
		"project_id":    job.ProjectID,
		"project_path":  job.ProjectPath,
		"status":        string(job.Status),
		"files_total":   job.FilesTotal,
		"files_indexed": job.FilesIndexed,
		"progress":      job.Progress,
		"started_at":    job.StartedAt,
	}
	if job.CompletedAt != nil {
		result["completed_at"] = job.CompletedAt
	}
	if job.Error != nil {
		result["error"] = *job.Error
	}

	return NewStructuredResponse(result), nil
}

// ---- CodeHybridSearchTool ----

func (t *CodeHybridSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeHybridSearchToolName,
		Description: "Searches indexed code symbols using natural language. Combines vector semantic search with full-text search. Results are ranked with a normalized relevance score (0..1, 1 = best match) and the low-relevance tail is trimmed. Documentation files (.md, .rst, ...) are excluded by default; set include_docs to include them.\n\nWHEN TO USE: conceptual/semantic discovery (\"where is authentication handled\", \"code that parses config\") and finding symbols by meaning across one or more indexed projects. DO NOT use it for exact strings or known identifiers — for a literal substring or regex over the working tree the Grep tool (ripgrep) is faster and returns far less context. Results are paginated (limit/offset; page forward with next_offset); set group_by_file to compact many hits that share a file.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to search in.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language query (e.g. 'function that handles authentication').",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results per page (default: 20, max: 50).",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Number of ranked results to skip before the page (default: 0). Use next_offset from a previous response to page forward without re-querying.",
			},
			"languages": map[string]any{
				"type":        "array",
				"description": "Optional filter by programming languages (e.g. [\"go\", \"typescript\"]).",
				"items":       map[string]any{"type": "string"},
			},
			"symbol_types": map[string]any{
				"type":        "array",
				"description": "Optional filter by symbol types (e.g. [\"function\", \"class\", \"method\"]).",
				"items":       map[string]any{"type": "string"},
			},
			"min_score": map[string]any{
				"type":        "number",
				"description": "Minimum normalized relevance score (0..1) to keep. Higher values return fewer, more relevant results. Default trims results below 15% of the top hit.",
			},
			"include_docs": map[string]any{
				"type":        "boolean",
				"description": "Include documentation files (markdown, rst, txt) in results. Default false.",
			},
			"debug": map[string]any{
				"type":        "boolean",
				"description": "Include raw score breakdown (vector/fts/raw) per result. Default false.",
			},
			"group_by_file": map[string]any{
				"type":        "boolean",
				"description": "Group results under a per-file header instead of repeating the file path on every line. Saves tokens when many hits share a file. Default false.",
			},
		},
		Required: []string{"project_id", "query"},
	}
}

func (t *CodeHybridSearchTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID   string   `json:"project_id"`
		Query       string   `json:"query"`
		Limit       int      `json:"limit"`
		Offset      int      `json:"offset"`
		Languages   []string `json:"languages"`
		SymbolTypes []string `json:"symbol_types"`
		MinScore    float64  `json:"min_score"`
		IncludeDocs bool     `json:"include_docs"`
		Debug       bool     `json:"debug"`
		GroupByFile bool     `json:"group_by_file"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if req.Query == "" {
		return NewTextErrorResponse("query is required"), nil
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 50 {
		req.Limit = 50
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	var langs []code.Language
	for _, l := range req.Languages {
		langs = append(langs, treesitter.Language(l))
	}
	var symTypes []code.SymbolType
	for _, s := range req.SymbolTypes {
		symTypes = append(symTypes, treesitter.SymbolType(s))
	}

	// Over-fetch enough candidates to cover offset+limit even after doc
	// filtering and the relevance cutoff trim the result set. The 3x factor
	// mirrors the original headroom for the filtered tail.
	want := req.Offset + req.Limit
	fetchLimit := want
	if !req.IncludeDocs || req.MinScore > 0 {
		fetchLimit = want * 3
	}
	if fetchLimit > 150 {
		fetchLimit = 150
	}
	if fetchLimit < want {
		fetchLimit = want
	}

	results, err := t.indexer.HybridSearch(ctx, req.ProjectID, req.Query, fetchLimit, langs, symTypes)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("hybrid search error: %v", err)), nil
	}

	if len(results) == 0 {
		return NewTextResponse("No symbols found matching the query."), nil
	}

	// Rank/filter the full candidate set, then page over it. Ranks are
	// assigned across the whole ranked set so they stay stable across pages.
	ranked := rankAndFilterHybrid(results, req.MinScore, req.IncludeDocs, req.Debug)
	if len(ranked) == 0 {
		return NewTextResponse("No code symbols found matching the query (documentation excluded; set include_docs to include it)."), nil
	}

	page, meta := paginate(ranked, req.Offset, req.Limit)
	if len(page) == 0 {
		return NewTextResponse(fmt.Sprintf("No results at offset %d (total %d).", req.Offset, meta.Total)), nil
	}

	rows := make([]compactRow, len(page))
	for i, it := range page {
		rows[i] = compactRow{
			Rank:      it.Rank,
			Score:     it.Score,
			Kind:      it.Kind,
			FilePath:  it.FilePath,
			StartLine: it.StartLine,
			NamePath:  firstNonEmpty(it.NamePath, it.Name),
			Trailer:   firstNonEmpty(it.Signature, it.Snippet, it.Name),
		}
	}

	body := renderCompact(rows, meta, req.GroupByFile)
	logSearchTelemetry(codeHybridSearchToolName, req.Query, len(page), meta, req.GroupByFile, len(body))

	return WithResponseMetadata(NewTextResponse(body), map[string]any{
		"count":       len(page),
		"total":       meta.Total,
		"offset":      meta.Offset,
		"limit":       meta.Limit,
		"has_more":    meta.HasMore,
		"next_offset": meta.NextOffset,
		"results":     page,
	}), nil
}

// hybridResultItem is the slim, ranked projection emitted by code_hybrid_search.
type hybridResultItem struct {
	SymbolType  string  `json:"symbol_type"`
	Name        string  `json:"name"`
	NamePath    string  `json:"name_path,omitempty"`
	FilePath    string  `json:"file_path"`
	StartLine   int     `json:"start_line"`
	Signature   string  `json:"signature,omitempty"`
	Snippet     string  `json:"snippet,omitempty"`
	Kind        string  `json:"kind"`
	Score       float64 `json:"score"`
	Rank        int     `json:"rank"`
	RawScore    float64 `json:"raw_score,omitempty"`
	VectorScore float64 `json:"vector_score,omitempty"`
	FTSScore    float64 `json:"fts_score,omitempty"`
}

// rankAndFilterHybrid turns raw hybrid search results into a ranked, slim
// projection: it drops documentation files (unless includeDocs), normalizes
// scores to 0..1 relative to the top hit and trims the low-relevance tail
// (keeping at least the top result). It returns the FULL ranked set so the
// caller can paginate over it; ranks are assigned across the whole set and
// remain stable across pages. When debug is set, per-result raw/vector/fts
// scores are included.
func rankAndFilterHybrid(results []code.HybridSearchResult, minScore float64, includeDocs, debug bool) []hybridResultItem {
	// Drop documentation files unless explicitly requested. Build a fresh slice
	// so the caller's input is never mutated in place.
	if !includeDocs {
		filtered := make([]code.HybridSearchResult, 0, len(results))
		for _, r := range results {
			if r.Symbol != nil && isDocFile(r.Symbol.FilePath) {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	}
	if len(results) == 0 {
		return nil
	}

	// Normalize scores to 0..1 relative to the top hit (negatives clamped to 0)
	// so the emitted score is interpretable and a relevance cutoff can apply.
	top := 0.0
	for _, r := range results {
		if r.Score > top {
			top = r.Score
		}
	}
	cutoff := minScore
	if cutoff <= 0 {
		cutoff = defaultMinScoreRatio
	}

	items := make([]hybridResultItem, 0, len(results))
	for _, r := range results {
		sym := r.Symbol
		if sym == nil {
			continue
		}
		norm := 0.0
		if top > 0 {
			norm = r.Score / top
			if norm < 0 {
				norm = 0
			}
		}
		// Apply the cutoff but always keep at least the top result.
		if top > 0 && norm < cutoff && len(items) > 0 {
			continue
		}
		kind := "code"
		if isDocFile(sym.FilePath) {
			kind = "doc"
		}
		item := hybridResultItem{
			SymbolType: string(sym.SymbolType),
			Name:       truncateField(sym.Name),
			NamePath:   truncateField(sym.NamePath),
			FilePath:   sym.FilePath,
			StartLine:  sym.StartLine,
			Signature:  truncateField(sym.Signature),
			Kind:       kind,
			Score:      roundScore(norm),
			Rank:       len(items) + 1,
		}
		// A 1-line snippet disambiguates hits that lack a signature (e.g.
		// non-function symbols or headings) without dumping source_code.
		if item.Signature == "" {
			item.Snippet = firstSourceLine(sym)
		}
		if debug {
			item.RawScore = r.Score
			item.VectorScore = r.VectorScore
			item.FTSScore = r.FTSScore
		}
		items = append(items, item)
	}
	return items
}

// roundScore rounds a normalized score to 3 decimal places for compact output.
func roundScore(s float64) float64 {
	return math.Round(s*1000) / 1000
}

// paginationMeta is the per-response pagination envelope attached to code search
// results. total is the number of matches found before the page slice; offset
// and limit describe the requested window; has_more is true when more results
// exist past the window and next_offset (only meaningful when has_more) is the
// offset to request the following page.
type paginationMeta struct {
	Total      int  `json:"total"`
	Offset     int  `json:"offset"`
	Limit      int  `json:"limit"`
	HasMore    bool `json:"has_more"`
	NextOffset int  `json:"next_offset,omitempty"`
	// TotalIsLowerBound is set when the underlying store was queried with a
	// "+1 sentinel" page window rather than the full result set, so Total only
	// counts what was fetched and the real total may be higher.
	TotalIsLowerBound bool `json:"total_is_lower_bound,omitempty"`
}

// paginate slices items to the [offset:offset+limit] window and reports
// pagination metadata. total reflects the full pre-slice count. It is pure and
// never mutates the input. When totalKnown is false the caller fetched at most
// offset+limit+1 candidates (so total may understate the real count); in that
// case has_more is derived from whether a sentinel extra element was present,
// which callers signal by passing the real fetched length as len(items).
func paginate[T any](items []T, offset, limit int) ([]T, paginationMeta) {
	total := len(items)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = total
	}
	meta := paginationMeta{Total: total, Offset: offset, Limit: limit}
	if offset >= total {
		return nil, meta
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := items[offset:end]
	if end < total {
		meta.HasMore = true
		meta.NextOffset = end
	}
	return page, meta
}

// paginateFetched slices a candidate set that was fetched from the store with a
// "+1 sentinel" window — i.e. the caller asked the store for offset+limit+1
// items. If the sentinel element is present there are more results past the
// page, so has_more is forced true and total is reported as a lower bound. This
// keeps has_more accurate for SQL-LIMIT-based tools without a separate COUNT.
func paginateFetched[T any](items []T, offset, limit int) ([]T, paginationMeta) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = len(items)
	}
	hasSentinel := len(items) > offset+limit
	if hasSentinel {
		items = items[:offset+limit]
	}
	page, meta := paginate(items, offset, limit)
	if hasSentinel {
		meta.HasMore = true
		meta.NextOffset = offset + limit
		meta.TotalIsLowerBound = true
	}
	return page, meta
}

// compactPaginationFooter renders a single trailing line summarizing the
// pagination window, suitable for appending to the compact one-line-per-result
// body so the agent can page without re-running the query. shown is the number
// of results actually emitted in this page.
func compactPaginationFooter(meta paginationMeta, shown int) string {
	if meta.Total == 0 || shown == 0 {
		return fmt.Sprintf("[showing 0 of %d]", meta.Total)
	}
	totalStr := strconv.Itoa(meta.Total)
	if meta.TotalIsLowerBound {
		totalStr += "+"
	}
	if meta.HasMore {
		return fmt.Sprintf("[showing %d-%d of %s  has_more=true  next_offset=%d]",
			meta.Offset+1, meta.Offset+shown, totalStr, meta.NextOffset)
	}
	return fmt.Sprintf("[showing %d-%d of %s  has_more=false]", meta.Offset+1, meta.Offset+shown, totalStr)
}

// compactLine renders one search result as a single line:
//
//	#<rank> <score> <kind> <file>:<line>  <name_path>  <signature/snippet>
//
// Empty optional fields are omitted. Keeping each result on exactly one line
// lets the line-based session cache paginate by RESULT (cache_read offset/limit
// map 1:1 to results) instead of by arbitrary serialized lines.
func compactLine(rank int, score float64, kind, file string, line int, namePath, trailer string) string {
	return compactLineCore(rank, score, kind, file, line, namePath, trailer, true)
}

// compactLineCore renders a result line, optionally omitting the file path. When
// withFile is false the location is rendered as "L<line>" only, used in
// group-by-file mode where the file path is printed once as a group header so it
// is not repeated on every line (saving tokens for many hits in one file).
func compactLineCore(rank int, score float64, kind, file string, line int, namePath, trailer string, withFile bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "#%d", rank)
	if score > 0 {
		fmt.Fprintf(&sb, " %.3f", score)
	}
	if kind != "" {
		fmt.Fprintf(&sb, " %s", kind)
	}
	if withFile {
		fmt.Fprintf(&sb, " %s:%d", file, line)
	} else {
		fmt.Fprintf(&sb, " L%d", line)
	}
	if namePath != "" {
		fmt.Fprintf(&sb, "  %s", namePath)
	}
	if trailer != "" {
		fmt.Fprintf(&sb, "  %s", trailer)
	}
	return sb.String()
}

// compactRow is the rendering-agnostic projection of one result, shared by all
// code search tools so they can emit either a flat list or a group-by-file view
// through a single renderer.
type compactRow struct {
	Rank      int
	Score     float64
	Kind      string
	FilePath  string
	StartLine int
	NamePath  string
	Trailer   string
}

// renderCompact renders a page of results plus the pagination footer. When
// groupByFile is true, results are grouped under a per-file header (preserving
// first-appearance order) and each line drops the repeated file path; otherwise
// one self-contained line per result is emitted.
func renderCompact(rows []compactRow, meta paginationMeta, groupByFile bool) string {
	var sb strings.Builder
	if groupByFile {
		var order []string
		groups := make(map[string][]compactRow)
		for _, r := range rows {
			if _, ok := groups[r.FilePath]; !ok {
				order = append(order, r.FilePath)
			}
			groups[r.FilePath] = append(groups[r.FilePath], r)
		}
		for _, f := range order {
			g := groups[f]
			fmt.Fprintf(&sb, "%s (%d)\n", f, len(g))
			for _, r := range g {
				sb.WriteString("  ")
				sb.WriteString(compactLineCore(r.Rank, r.Score, r.Kind, r.FilePath, r.StartLine, r.NamePath, r.Trailer, false))
				sb.WriteByte('\n')
			}
		}
	} else {
		for _, r := range rows {
			sb.WriteString(compactLine(r.Rank, r.Score, r.Kind, r.FilePath, r.StartLine, r.NamePath, r.Trailer))
			sb.WriteByte('\n')
		}
	}
	sb.WriteString(compactPaginationFooter(meta, len(rows)))
	return sb.String()
}

// logSearchTelemetry records a lightweight, structured trace of a code search
// call so token-efficiency and routing can be observed over time (e.g. response
// byte size, page vs. total, whether grouping was used). It is debug-level so it
// never adds to the user-visible output or the model context.
func logSearchTelemetry(tool, query string, shown int, meta paginationMeta, groupByFile bool, respBytes int) {
	logging.Debug("code search",
		"tool", tool,
		"query", query,
		"shown", shown,
		"total", meta.Total,
		"total_is_lower_bound", meta.TotalIsLowerBound,
		"offset", meta.Offset,
		"limit", meta.Limit,
		"has_more", meta.HasMore,
		"group_by_file", groupByFile,
		"resp_bytes", respBytes,
	)
}

// firstNonEmpty returns the first non-empty string among its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstSourceLine returns the first non-empty, trimmed line of a symbol's source
// code, truncated for compact output. It provides a disambiguating snippet when
// no signature is available (e.g. for semantic hits on non-function symbols).
func firstSourceLine(sym *code.CodeSymbol) string {
	if sym == nil || sym.SourceCode == "" {
		return ""
	}
	for _, line := range strings.Split(sym.SourceCode, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return truncateField(trimmed)
		}
	}
	return ""
}

// ---- CodeFindSymbolTool ----

func (t *CodeFindSymbolTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeFindSymbolToolName,
		Description: "Finds code symbols (functions, classes, methods, etc.) by name pattern. Supports exact match, suffix match, and substring matching.\n\nWHEN TO USE: you know (part of) a symbol's NAME and want its definition location and signature, optionally scoped by language/type. For free-text or regex over raw file contents use the Grep tool (ripgrep) instead — it is faster and lighter. Results are paginated (limit/offset, page with next_offset); set group_by_file to compact hits sharing a file.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to search in.",
			},
			"name_path_pattern": map[string]any{
				"type":        "string",
				"description": "Name or path pattern: '/ClassName/method' for exact match, 'ClassName/method' for suffix match, or 'method' for simple name match.",
			},
			"relative_path": map[string]any{
				"type":        "string",
				"description": "Optional file or directory path to restrict search.",
			},
			"symbol_types": map[string]any{
				"type":        "array",
				"description": "Optional filter by symbol types (e.g. [\"function\", \"class\", \"method\", \"interface\"]).",
				"items":       map[string]any{"type": "string"},
			},
			"languages": map[string]any{
				"type":        "array",
				"description": "Optional filter by programming languages.",
				"items":       map[string]any{"type": "string"},
			},
			"include_body": map[string]any{
				"type":        "boolean",
				"description": "Include source code body in results (default: false).",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": "Include children up to this depth level (0=symbol only, 1=direct children).",
			},
			"substring_matching": map[string]any{
				"type":        "boolean",
				"description": "Enable partial name matching.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results per page (default: 50).",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Number of results to skip before the page (default: 0). Use next_offset from a previous response to page forward.",
			},
			"group_by_file": map[string]any{
				"type":        "boolean",
				"description": "Group results under a per-file header instead of repeating the file path on every line. Saves tokens when many hits share a file. Default false.",
			},
		},
		Required: []string{"project_id", "name_path_pattern"},
	}
}

func (t *CodeFindSymbolTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID         string   `json:"project_id"`
		NamePathPattern   string   `json:"name_path_pattern"`
		RelativePath      string   `json:"relative_path"`
		SymbolTypes       []string `json:"symbol_types"`
		Languages         []string `json:"languages"`
		IncludeBody       bool     `json:"include_body"`
		Depth             int      `json:"depth"`
		SubstringMatching bool     `json:"substring_matching"`
		Limit             int      `json:"limit"`
		Offset            int      `json:"offset"`
		GroupByFile       bool     `json:"group_by_file"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if req.NamePathPattern == "" {
		return NewTextErrorResponse("name_path_pattern is required"), nil
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	var symTypes []code.SymbolType
	for _, s := range req.SymbolTypes {
		symTypes = append(symTypes, treesitter.SymbolType(s))
	}
	var langs []code.Language
	for _, l := range req.Languages {
		langs = append(langs, treesitter.Language(l))
	}

	query := code.SymbolQuery{
		ProjectID:       req.ProjectID,
		NamePathPattern: req.NamePathPattern,
		RelativePath:    req.RelativePath,
		IncludeTypes:    symTypes,
		Languages:       langs,
		IncludeBody:     req.IncludeBody,
		Depth:           req.Depth,
		SubstringMatch:  req.SubstringMatching,
		// Over-fetch to cover the requested page plus a +1 sentinel so the tool
		// layer can detect has_more accurately without a separate COUNT query.
		Limit: req.Offset + req.Limit + 1,
	}

	symbols, err := t.indexer.FindSymbol(ctx, query)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("find symbol error: %v", err)), nil
	}

	if len(symbols) == 0 {
		return NewTextResponse("No symbols found matching the pattern."), nil
	}

	type symItem struct {
		SymbolType string `json:"symbol_type"`
		Name       string `json:"name"`
		NamePath   string `json:"name_path,omitempty"`
		FilePath   string `json:"file_path"`
		StartLine  int    `json:"start_line"`
		Signature  string `json:"signature,omitempty"`
	}
	items := make([]symItem, len(symbols))
	for i, s := range symbols {
		items[i] = symItem{
			SymbolType: string(s.SymbolType),
			Name:       truncateField(s.Name),
			NamePath:   truncateField(s.NamePath),
			FilePath:   s.FilePath,
			StartLine:  s.StartLine,
			Signature:  truncateField(s.Signature),
		}
	}

	page, meta := paginateFetched(items, req.Offset, req.Limit)
	if len(page) == 0 {
		return NewTextResponse(fmt.Sprintf("No symbols at offset %d (total %d).", req.Offset, meta.Total)), nil
	}

	rows := make([]compactRow, len(page))
	for i, it := range page {
		rows[i] = compactRow{
			Rank:      meta.Offset + i + 1,
			Kind:      it.SymbolType,
			FilePath:  it.FilePath,
			StartLine: it.StartLine,
			NamePath:  firstNonEmpty(it.NamePath, it.Name),
			Trailer:   firstNonEmpty(it.Signature, it.SymbolType),
		}
	}

	body := renderCompact(rows, meta, req.GroupByFile)
	logSearchTelemetry(codeFindSymbolToolName, req.NamePathPattern, len(page), meta, req.GroupByFile, len(body))

	return WithResponseMetadata(NewTextResponse(body), map[string]any{
		"count":       len(page),
		"total":       meta.Total,
		"offset":      meta.Offset,
		"limit":       meta.Limit,
		"has_more":    meta.HasMore,
		"next_offset": meta.NextOffset,
		"symbols":     page,
	}), nil
}

// ---- CodeGetSymbolsOverviewTool ----

func (t *CodeGetSymbolsOverviewTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeGetSymbolsOverviewName,
		Description: "Returns a high-level overview of top-level symbols (functions, classes, etc.) in a specific file.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID.",
			},
			"relative_path": map[string]any{
				"type":        "string",
				"description": "Relative path to the file within the project.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of symbols to return (default: 100).",
			},
		},
		Required: []string{"project_id", "relative_path"},
	}
}

func (t *CodeGetSymbolsOverviewTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID    string `json:"project_id"`
		RelativePath string `json:"relative_path"`
		MaxResults   int    `json:"max_results"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if req.RelativePath == "" {
		return NewTextErrorResponse("relative_path is required"), nil
	}

	symbols, err := t.indexer.GetSymbolsOverview(ctx, req.ProjectID, req.RelativePath, req.MaxResults)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("symbols overview error: %v", err)), nil
	}

	if len(symbols) == 0 {
		return NewTextResponse(fmt.Sprintf("No symbols found in file: %s", req.RelativePath)), nil
	}

	type symItem struct {
		SymbolType string `json:"symbol_type"`
		Name       string `json:"name"`
		NamePath   string `json:"name_path,omitempty"`
		StartLine  int    `json:"start_line"`
		Signature  string `json:"signature,omitempty"`
	}
	items := make([]symItem, len(symbols))
	for i, s := range symbols {
		items[i] = symItem{
			SymbolType: string(s.SymbolType),
			Name:       truncateField(s.Name),
			NamePath:   truncateField(s.NamePath),
			StartLine:  s.StartLine,
			Signature:  truncateField(s.Signature),
		}
	}

	return NewStructuredResponse(map[string]any{
		"count":   len(items),
		"symbols": items,
	}), nil
}

// ---- CodeGetProjectStatsTool ----

func (t *CodeGetProjectStatsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeGetProjectStatsToolName,
		Description: "Returns statistics for an indexed code project: total files, symbols, and language breakdown.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to get statistics for.",
			},
		},
		Required: []string{"project_id"},
	}
}

func (t *CodeGetProjectStatsTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}

	stats, err := t.indexer.GetProjectStats(ctx, req.ProjectID)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("get stats error: %v", err)), nil
	}

	return NewStructuredResponse(stats), nil
}

// ---- CodeDeleteProjectTool ----

func (t *CodeDeleteProjectTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeDeleteProjectToolName,
		Description: "Deletes an indexed code project and all associated indexed files and symbols.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to delete.",
			},
		},
		Required: []string{"project_id"},
	}
}

func (t *CodeDeleteProjectTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}

	if err := t.indexer.DeleteProject(ctx, req.ProjectID); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("delete project error: %v", err)), nil
	}

	return NewStructuredResponse(map[string]any{
		"project_id": req.ProjectID,
		"deleted":    true,
	}), nil
}

// ---- CodeReindexFileTool ----

func (t *CodeReindexFileTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeReindexFileToolName,
		Description: "Re-indexes a single file in a project. Use this after modifying a file to keep the index up to date.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID.",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Relative path to the file within the project.",
			},
		},
		Required: []string{"project_id", "file_path"},
	}
}

func (t *CodeReindexFileTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID string `json:"project_id"`
		FilePath  string `json:"file_path"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if req.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}

	if err := t.indexer.ReindexFile(ctx, req.ProjectID, req.FilePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("reindex file error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("File re-indexed: %s in project %s", req.FilePath, req.ProjectID)), nil
}

// ---- CodeListProjectsTool ----

func (t *CodeListProjectsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeListProjectsToolName,
		Description: "Lists all indexed code projects with their status and statistics.",
		Parameters:  map[string]any{},
	}
}

func (t *CodeListProjectsTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	projects, err := t.indexer.ListProjects(ctx)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("list projects error: %v", err)), nil
	}

	if len(projects) == 0 {
		return NewTextResponse("No indexed projects found."), nil
	}

	return NewStructuredResponse(map[string]any{
		"count":    len(projects),
		"projects": projects,
	}), nil
}

// ---- CodeSearchPatternTool ----

func (t *CodeSearchPatternTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeSearchPatternToolName,
		Description: "Searches INDEXED code symbols for literal text or regular expressions across symbol source code, names, paths, signatures, and docs.\n\nWHEN TO USE: prefer the Grep tool (ripgrep) for plain literal/regex scans of the working tree — it is faster and returns less context. Use this when you specifically want symbol-scoped matches with structured metadata (symbol type, name path, signature) across indexed, possibly cross-project, code. If nothing is indexed it automatically falls back to grep. Results are paginated (limit/offset, page with next_offset); set group_by_file to compact hits sharing a file.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to search in.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Text pattern or regular expression to search for across indexed symbol fields.",
			},
			"case_sensitive": map[string]any{
				"type":        "boolean",
				"description": "Enable case-sensitive matching (default: false).",
			},
			"is_regex": map[string]any{
				"type":        "boolean",
				"description": "Treat pattern as regular expression (default: false).",
			},
			"languages": map[string]any{
				"type":        "array",
				"description": "Optional filter by programming languages.",
				"items":       map[string]any{"type": "string"},
			},
			"symbol_types": map[string]any{
				"type":        "array",
				"description": "Optional filter by symbol types.",
				"items":       map[string]any{"type": "string"},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results per page (default: 50).",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Number of results to skip before the page (default: 0). Use next_offset from a previous response to page forward.",
			},
			"group_by_file": map[string]any{
				"type":        "boolean",
				"description": "Group results under a per-file header instead of repeating the file path on every line. Saves tokens when many hits share a file. Default false.",
			},
		},
		Required: []string{"project_id", "pattern"},
	}
}

func (t *CodeSearchPatternTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req codeSearchPatternRequest
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if req.Pattern == "" {
		return NewTextErrorResponse("pattern is required"), nil
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	var langs []code.Language
	for _, l := range req.Languages {
		langs = append(langs, treesitter.Language(l))
	}
	var symTypes []code.SymbolType
	for _, s := range req.SymbolTypes {
		symTypes = append(symTypes, treesitter.SymbolType(s))
	}

	// Fetch the requested page plus a +1 sentinel so has_more is accurate.
	fetchLimit := req.Offset + req.Limit + 1
	symbols, err := t.indexer.SearchPattern(ctx, req.ProjectID, req.Pattern, req.CaseSensitive, req.IsRegex, fetchLimit, langs, symTypes)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("search pattern error: %v", err)), nil
	}

	if len(symbols) == 0 {
		fallback, fallbackErr := t.fallbackToGrep(ctx, req)
		if fallbackErr != nil {
			return NewTextErrorResponse(fmt.Sprintf("search pattern fallback error: %v", fallbackErr)), nil
		}
		if fallback != nil {
			return *fallback, nil
		}
		return NewTextResponse(fmt.Sprintf("No symbols found matching pattern: %s", req.Pattern)), nil
	}

	// Project to a slim shape with a single-line match snippet instead of
	// returning the raw CodeSymbol (which carries source_code, doc_string,
	// metadata, byte offsets and timestamps) and bloats the context window.
	type patternItem struct {
		SymbolType string `json:"symbol_type"`
		Name       string `json:"name"`
		NamePath   string `json:"name_path,omitempty"`
		FilePath   string `json:"file_path"`
		StartLine  int    `json:"start_line"`
		Signature  string `json:"signature,omitempty"`
		Snippet    string `json:"snippet,omitempty"`
	}
	items := make([]patternItem, len(symbols))
	for i, s := range symbols {
		items[i] = patternItem{
			SymbolType: string(s.SymbolType),
			Name:       truncateField(s.Name),
			NamePath:   truncateField(s.NamePath),
			FilePath:   s.FilePath,
			StartLine:  s.StartLine,
			Signature:  truncateField(s.Signature),
			Snippet:    matchSnippet(s, req.Pattern, req.CaseSensitive),
		}
	}

	page, meta := paginateFetched(items, req.Offset, req.Limit)
	if len(page) == 0 {
		return NewTextResponse(fmt.Sprintf("No matches at offset %d (total %d).", req.Offset, meta.Total)), nil
	}

	rows := make([]compactRow, len(page))
	for i, it := range page {
		rows[i] = compactRow{
			Rank:      meta.Offset + i + 1,
			Kind:      it.SymbolType,
			FilePath:  it.FilePath,
			StartLine: it.StartLine,
			NamePath:  firstNonEmpty(it.NamePath, it.Name),
			Trailer:   firstNonEmpty(it.Snippet, it.Signature, it.Name),
		}
	}

	body := renderCompact(rows, meta, req.GroupByFile)
	logSearchTelemetry(codeSearchPatternToolName, req.Pattern, len(page), meta, req.GroupByFile, len(body))

	return WithResponseMetadata(NewTextResponse(body), map[string]any{
		"count":       len(page),
		"total":       meta.Total,
		"offset":      meta.Offset,
		"limit":       meta.Limit,
		"has_more":    meta.HasMore,
		"next_offset": meta.NextOffset,
		"symbols":     page,
	}), nil
}

type codeSearchPatternRequest struct {
	ProjectID     string   `json:"project_id"`
	Pattern       string   `json:"pattern"`
	CaseSensitive bool     `json:"case_sensitive"`
	IsRegex       bool     `json:"is_regex"`
	Languages     []string `json:"languages"`
	SymbolTypes   []string `json:"symbol_types"`
	Limit         int      `json:"limit"`
	Offset        int      `json:"offset"`
	GroupByFile   bool     `json:"group_by_file"`
}

func (t *CodeSearchPatternTool) fallbackToGrep(ctx context.Context, req codeSearchPatternRequest) (*ToolResponse, error) {
	projects, err := t.indexer.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	var projectRoot string
	for _, project := range projects {
		if project != nil && project.ProjectID == req.ProjectID {
			projectRoot = project.RootPath
			break
		}
	}
	if projectRoot == "" {
		return nil, nil
	}

	grep := &grepTool{}
	grepParams := map[string]any{
		"pattern":      req.Pattern,
		"path":         projectRoot,
		"literal_text": !req.IsRegex,
		"output_mode":  "content",
		"head_limit":   req.Limit,
		"offset":       0,
	}
	if typeFilter := languageTypeFilter(req.Languages); typeFilter != "" {
		grepParams["type"] = typeFilter
	}
	if req.CaseSensitive {
		grepParams["pattern"] = "(?-i)" + req.Pattern
	}
	grepInput, err := json.Marshal(grepParams)
	if err != nil {
		return nil, fmt.Errorf("marshal grep params: %w", err)
	}
	grepCall := ToolCall{Input: string(grepInput)}

	resp, err := grep.Run(ctx, grepCall)
	if err != nil {
		return nil, err
	}

	metadata := GrepResponseMetadata{}
	if len(resp.Metadata) > 0 {
		_ = json.Unmarshal([]byte(resp.Metadata), &metadata)
	}
	if metadata.NumberOfMatches == 0 {
		return nil, nil
	}

	content := fmt.Sprintf("No indexed symbol matches found for pattern: %s\nFallback grep results in project path %s:\n\n%s", req.Pattern, projectRoot, resp.Content)
	wrapped := WithResponseMetadata(NewTextResponse(content), map[string]any{
		"fallback_tool": "grep",
		"project_id":    req.ProjectID,
		"project_path":  projectRoot,
		"search_type":   "grep_fallback",
		"grep_metadata": metadata,
	})
	return &wrapped, nil
}

func languageTypeFilter(languages []string) string {
	for _, lang := range languages {
		lang = strings.TrimSpace(lang)
		if lang == "" {
			continue
		}
		if _, ok := search.TypeToGlobs(lang); ok {
			return lang
		}
	}
	return ""
}

// ---- CodeFindReferencesTool ----

func (t *CodeFindReferencesTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeFindReferencesToolName,
		Description: "Finds symbols that reference a target symbol by ID or name across the indexed project.\n\nWHEN TO USE: locate call sites/usages of a KNOWN symbol within indexed code. For a quick literal scan of usages in the working tree the Grep tool (ripgrep) may be faster. Results are paginated (limit/offset, page with next_offset); set group_by_file to compact references sharing a file.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to search in.",
			},
			"symbol_id": map[string]any{
				"type":        "string",
				"description": "Optional symbol ID to resolve the target name.",
			},
			"symbol_name": map[string]any{
				"type":        "string",
				"description": "Optional symbol name to search for references.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results per page (default: 50).",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Number of results to skip before the page (default: 0). Use next_offset from a previous response to page forward.",
			},
			"group_by_file": map[string]any{
				"type":        "boolean",
				"description": "Group references under a per-file header instead of repeating the file path on every line. Saves tokens when many references share a file. Default false.",
			},
		},
		Required: []string{"project_id"},
	}
}

func (t *CodeFindReferencesTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID   string `json:"project_id"`
		SymbolID    string `json:"symbol_id"`
		SymbolName  string `json:"symbol_name"`
		Limit       int    `json:"limit"`
		Offset      int    `json:"offset"`
		GroupByFile bool   `json:"group_by_file"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if req.SymbolID == "" && req.SymbolName == "" {
		return NewTextErrorResponse("symbol_id or symbol_name is required"), nil
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// Fetch the requested page plus a +1 sentinel so has_more is accurate.
	fetchLimit := req.Offset + req.Limit + 1
	symbols, err := t.indexer.FindReferences(ctx, req.ProjectID, req.SymbolID, req.SymbolName, fetchLimit)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("find references error: %v", err)), nil
	}
	if len(symbols) == 0 {
		return NewTextResponse("No references found for the requested symbol."), nil
	}

	type refItem struct {
		SymbolType string `json:"symbol_type"`
		Name       string `json:"name"`
		NamePath   string `json:"name_path,omitempty"`
		FilePath   string `json:"file_path"`
		StartLine  int    `json:"start_line"`
	}
	refs := make([]refItem, len(symbols))
	for i, s := range symbols {
		refs[i] = refItem{
			SymbolType: string(s.SymbolType),
			Name:       truncateField(s.Name),
			NamePath:   truncateField(s.NamePath),
			FilePath:   s.FilePath,
			StartLine:  s.StartLine,
		}
	}

	page, meta := paginateFetched(refs, req.Offset, req.Limit)
	if len(page) == 0 {
		return NewTextResponse(fmt.Sprintf("No references at offset %d (total %d).", req.Offset, meta.Total)), nil
	}

	rows := make([]compactRow, len(page))
	for i, it := range page {
		rows[i] = compactRow{
			Rank:      meta.Offset + i + 1,
			Kind:      it.SymbolType,
			FilePath:  it.FilePath,
			StartLine: it.StartLine,
			NamePath:  firstNonEmpty(it.NamePath, it.Name),
		}
	}

	refName := firstNonEmpty(req.SymbolName, req.SymbolID)
	body := renderCompact(rows, meta, req.GroupByFile)
	logSearchTelemetry(codeFindReferencesToolName, refName, len(page), meta, req.GroupByFile, len(body))

	return WithResponseMetadata(NewTextResponse(body), map[string]any{
		"count":       len(page),
		"total":       meta.Total,
		"offset":      meta.Offset,
		"limit":       meta.Limit,
		"has_more":    meta.HasMore,
		"next_offset": meta.NextOffset,
		"references":  page,
	}), nil
}

// sanitizeProjectID converts a path or name to a valid project ID.
func sanitizeProjectID(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			result = append(result, c)
		case c == '/', c == '\\', c == ' ', c == '.':
			if len(result) > 0 && result[len(result)-1] != '_' {
				result = append(result, '_')
			}
		}
	}
	// Trim leading/trailing underscores
	for len(result) > 0 && result[0] == '_' {
		result = result[1:]
	}
	for len(result) > 0 && result[len(result)-1] == '_' {
		result = result[:len(result)-1]
	}
	return string(result)
}
