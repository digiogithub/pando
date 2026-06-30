package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/rag/code"
)

// Code property-graph tool names (lean-ctx Phase 4).
const (
	codeImpactAnalysisToolName = "code_impact_analysis"
	codeRelatedFilesToolName   = "code_related_files"
)

// CodeImpactAnalysisTool reports which symbols depend on a given symbol.
type CodeImpactAnalysisTool struct{ codeToolBase }

// CodeRelatedFilesTool reports files related to a given file via the code graph.
type CodeRelatedFilesTool struct{ codeToolBase }

// NewCodeImpactAnalysisTool constructs the code_impact_analysis tool.
func NewCodeImpactAnalysisTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeImpactAnalysisTool{codeToolBase{indexer}}
}

// NewCodeRelatedFilesTool constructs the code_related_files tool.
func NewCodeRelatedFilesTool(indexer *code.CodeIndexer) BaseTool {
	return &CodeRelatedFilesTool{codeToolBase{indexer}}
}

// ---- CodeImpactAnalysisTool ----

func (t *CodeImpactAnalysisTool) Info() ToolInfo {
	return ToolInfo{
		Name: codeImpactAnalysisToolName,
		Description: "Reverse-dependency (impact) analysis over the INDEXED code property graph: given a symbol name, returns the functions/methods that call it directly or transitively — i.e. \"what could break if I change this\".\n\n" +
			"Resolution is name-based via call edges: cheap and cross-file/cross-package, but approximate when a name is shared by unrelated symbols. Requires the project to be indexed with the code graph enabled (default). Pair with code_find_symbol to pin down the exact definition.",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to analyze.",
			},
			"symbol": map[string]any{
				"type":        "string",
				"description": "The symbol name to analyze (e.g. a function or method name). Not a path.",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": "Maximum transitive caller depth to traverse (default 2).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of callers to return (default 50).",
			},
		},
		Required: []string{"project_id", "symbol"},
	}
}

func (t *CodeImpactAnalysisTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID string `json:"project_id"`
		Symbol    string `json:"symbol"`
		Depth     int    `json:"depth"`
		Limit     int    `json:"limit"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if strings.TrimSpace(req.Symbol) == "" {
		return NewTextErrorResponse("symbol is required"), nil
	}

	res, err := t.indexer.ImpactAnalysis(ctx, req.ProjectID, req.Symbol, req.Depth, req.Limit)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("impact analysis error: %v", err)), nil
	}
	if len(res.Callers) == 0 {
		return NewTextResponse(fmt.Sprintf("No callers found for symbol %q (nothing in the indexed graph depends on it, or the project lacks call edges for its language).", req.Symbol)), nil
	}
	return NewStructuredResponse(map[string]any{
		"symbol":    res.Symbol,
		"count":     len(res.Callers),
		"truncated": res.Truncated,
		"callers":   res.Callers,
	}), nil
}

// ---- CodeRelatedFilesTool ----

func (t *CodeRelatedFilesTool) Info() ToolInfo {
	return ToolInfo{
		Name: codeRelatedFilesToolName,
		Description: "Ranks files related to a given file using the INDEXED code property graph: resolved imports plus bidirectional call coupling (this file calls symbols defined elsewhere, and other files call symbols defined here). Useful to pull the right neighboring files into context before an edit.\n\n" +
			"Scores blend imports (weight 1.0) and call coupling (weight 0.8). Relative ESM imports are resolved to project files; bare/package import paths rely on call coupling. Requires the project to be indexed with the code graph enabled (default).",
		Parameters: map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "The project ID to analyze.",
			},
			"file": map[string]any{
				"type":        "string",
				"description": "Relative file path within the project (as indexed).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of related files to return (default 20).",
			},
		},
		Required: []string{"project_id", "file"},
	}
}

func (t *CodeRelatedFilesTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		ProjectID string `json:"project_id"`
		File      string `json:"file"`
		Limit     int    `json:"limit"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return NewTextErrorResponse("project_id is required"), nil
	}
	if strings.TrimSpace(req.File) == "" {
		return NewTextErrorResponse("file is required"), nil
	}

	res, err := t.indexer.RelatedFiles(ctx, req.ProjectID, req.File, req.Limit)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("related files error: %v", err)), nil
	}
	if len(res.Related) == 0 {
		return NewTextResponse(fmt.Sprintf("No related files found for %q (no resolved imports or call coupling in the indexed graph).", req.File)), nil
	}
	return NewStructuredResponse(map[string]any{
		"file":      res.File,
		"count":     len(res.Related),
		"truncated": res.Truncated,
		"related":   res.Related,
	}), nil
}
