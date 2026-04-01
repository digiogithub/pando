package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/treesitter"
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
)

type codeToolBase struct {
	indexer *code.CodeIndexer
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
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
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

	out, _ := json.MarshalIndent(map[string]any{
		"job_id":       jobID,
		"project_id":   projectID,
		"project_path": req.ProjectPath,
		"status":       "in_progress",
		"message":      "Indexing started. Use code_index_status with the job_id to track progress.",
	}, "", "  ")
	return NewTextResponse(string(out)), nil
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
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
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

	out, _ := json.MarshalIndent(result, "", "  ")
	return NewTextResponse(string(out)), nil
}

// ---- CodeHybridSearchTool ----

func (t *CodeHybridSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeHybridSearchToolName,
		Description: "Searches indexed code symbols using natural language. Combines vector semantic search with full-text search for best results. Use this to find functions, classes, or code related to a concept.",
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
				"description": "Maximum number of results (default: 20, max: 50).",
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
		},
		Required: []string{"project_id", "query"},
	}
}

func (t *CodeHybridSearchTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID   string   `json:"project_id"`
		Query       string   `json:"query"`
		Limit       int      `json:"limit"`
		Languages   []string `json:"languages"`
		SymbolTypes []string `json:"symbol_types"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
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

	var langs []code.Language
	for _, l := range req.Languages {
		langs = append(langs, treesitter.Language(l))
	}
	var symTypes []code.SymbolType
	for _, s := range req.SymbolTypes {
		symTypes = append(symTypes, treesitter.SymbolType(s))
	}

	results, err := t.indexer.HybridSearch(ctx, req.ProjectID, req.Query, req.Limit, langs, symTypes)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("hybrid search error: %v", err)), nil
	}

	if len(results) == 0 {
		return NewTextResponse("No symbols found matching the query."), nil
	}

	type symbolItem struct {
		ID         string  `json:"id"`
		FilePath   string  `json:"file_path"`
		Language   string  `json:"language"`
		SymbolType string  `json:"symbol_type"`
		Name       string  `json:"name"`
		NamePath   string  `json:"name_path"`
		StartLine  int     `json:"start_line"`
		EndLine    int     `json:"end_line"`
		DocString  string  `json:"doc_string,omitempty"`
		Score      float64 `json:"score"`
		Rank       int     `json:"rank"`
	}

	items := make([]symbolItem, len(results))
	for i, r := range results {
		sym := r.Symbol
		items[i] = symbolItem{
			ID:         sym.ID,
			FilePath:   sym.FilePath,
			Language:   string(sym.Language),
			SymbolType: string(sym.SymbolType),
			Name:       sym.Name,
			NamePath:   sym.NamePath,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			DocString:  sym.DocString,
			Score:      r.Score,
			Rank:       r.Rank,
		}
	}

	out, _ := json.MarshalIndent(map[string]any{
		"count":   len(items),
		"results": items,
	}, "", "  ")
	return NewTextResponse(string(out)), nil
}

// ---- CodeFindSymbolTool ----

func (t *CodeFindSymbolTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeFindSymbolToolName,
		Description: "Finds code symbols (functions, classes, methods, etc.) by name pattern. Supports exact match, suffix match, and substring matching.",
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
				"description": "Maximum number of results (default: 50).",
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
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
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
		Limit:           req.Limit,
	}

	symbols, err := t.indexer.FindSymbol(ctx, query)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("find symbol error: %v", err)), nil
	}

	if len(symbols) == 0 {
		return NewTextResponse("No symbols found matching the pattern."), nil
	}

	out, _ := json.MarshalIndent(map[string]any{
		"count":   len(symbols),
		"symbols": symbols,
	}, "", "  ")
	return NewTextResponse(string(out)), nil
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
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
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

	out, _ := json.MarshalIndent(map[string]any{
		"count":   len(symbols),
		"symbols": symbols,
	}, "", "  ")
	return NewTextResponse(string(out)), nil
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
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}

	stats, err := t.indexer.GetProjectStats(ctx, req.ProjectID)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("get stats error: %v", err)), nil
	}

	out, _ := json.MarshalIndent(stats, "", "  ")
	return NewTextResponse(string(out)), nil
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
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.ProjectID == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}

	if err := t.indexer.DeleteProject(ctx, req.ProjectID); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("delete project error: %v", err)), nil
	}

	out, _ := json.MarshalIndent(map[string]any{
		"project_id": req.ProjectID,
		"deleted":    true,
	}, "", "  ")
	return NewTextResponse(string(out)), nil
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
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
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

	out, _ := json.MarshalIndent(map[string]any{
		"count":    len(projects),
		"projects": projects,
	}, "", "  ")
	return NewTextResponse(string(out)), nil
}

// ---- CodeSearchPatternTool ----

func (t *CodeSearchPatternTool) Info() ToolInfo {
	return ToolInfo{
		Name:        codeSearchPatternToolName,
		Description: "Searches for text patterns in indexed code symbols source code. Useful for finding specific identifiers, API calls, or patterns.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to search in.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Text pattern to search for in symbol source code.",
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
				"description": "Maximum number of results (default: 50).",
			},
		},
		Required: []string{"project_id", "pattern"},
	}
}

func (t *CodeSearchPatternTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID     string   `json:"project_id"`
		Pattern       string   `json:"pattern"`
		CaseSensitive bool     `json:"case_sensitive"`
		IsRegex       bool     `json:"is_regex"`
		Languages     []string `json:"languages"`
		SymbolTypes   []string `json:"symbol_types"`
		Limit         int      `json:"limit"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
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

	var langs []code.Language
	for _, l := range req.Languages {
		langs = append(langs, treesitter.Language(l))
	}
	var symTypes []code.SymbolType
	for _, s := range req.SymbolTypes {
		symTypes = append(symTypes, treesitter.SymbolType(s))
	}

	symbols, err := t.indexer.SearchPattern(ctx, req.ProjectID, req.Pattern, req.CaseSensitive, req.IsRegex, req.Limit, langs, symTypes)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("search pattern error: %v", err)), nil
	}

	if len(symbols) == 0 {
		return NewTextResponse(fmt.Sprintf("No symbols found matching pattern: %s", req.Pattern)), nil
	}

	out, _ := json.MarshalIndent(map[string]any{
		"count":   len(symbols),
		"symbols": symbols,
	}, "", "  ")
	return NewTextResponse(string(out)), nil
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
