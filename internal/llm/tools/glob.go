package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/search"
)

const (
	GlobToolName    = "glob"
	globDescription = `Fast file pattern matching tool that finds files by name and pattern, returning matching paths sorted by modification time (newest first).

WHEN TO USE THIS TOOL:
- Use when you need to find files by name patterns or extensions
- Great for finding specific file types across a directory structure
- Useful for discovering files that match certain naming conventions

HOW TO USE:
- Provide a glob pattern to match against file paths
- Optionally specify a starting directory (defaults to current working directory)
- Results are sorted with most recently modified files first

GLOB PATTERN SYNTAX:
- '*' matches any sequence of non-separator characters
- '**' matches any sequence of characters, including separators
- '?' matches any single non-separator character
- '[...]' matches any character in the brackets
- '[!...]' matches any character not in the brackets

COMMON PATTERN EXAMPLES:
- '*.js' - Find all JavaScript files in the current directory
- '**/*.js' - Find all JavaScript files in any subdirectory
- 'src/**/*.{ts,tsx}' - Find all TypeScript files in the src directory
- '*.{html,css,js}' - Find all HTML, CSS, and JS files

PAGINATION:
- head_limit: Maximum files to return (default: 100)
- offset: Skip first N files for pagination
- Example: head_limit=20, offset=20 to get files 21-40
- Check has_more in the response metadata to know if more results exist

LIMITATIONS:
- Results are limited to 100 files by default (newest first)
- Does not search file contents (use Grep tool for that)
- Hidden files (starting with '.') are skipped

TIPS:
- For the most useful results, combine with the Grep tool: first find files with Glob, then search their contents with Grep
- When doing iterative exploration that may require multiple rounds of searching, consider using the Agent tool instead
- Always check if results are truncated and refine your search pattern if needed`
)

type GlobParams struct {
	Pattern   string `json:"pattern"`
	Path      string `json:"path"`
	HeadLimit int    `json:"head_limit"` // Max files to return (default 100)
	Offset    int    `json:"offset"`     // Skip first N files for pagination
}

type GlobResponseMetadata struct {
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
	TotalFound    int  `json:"total_found"` // Total before offset/limit
	Offset        int  `json:"offset"`
	HasMore       bool `json:"has_more"`
}

type globTool struct{}

func NewGlobTool() BaseTool {
	return &globTool{}
}

func (g *globTool) Info() ToolInfo {
	return ToolInfo{
		Name:        GlobToolName,
		Description: globDescription,
		Parameters: map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "The glob pattern to match files against",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "The directory to search in. Defaults to the current working directory.",
			},
			"head_limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of files to return (default: 100). Use with offset for pagination.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Skip the first N results. Use with head_limit for pagination.",
			},
		},
		Required: []string{"pattern"},
	}
}

func (g *globTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params GlobParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Pattern == "" {
		return NewTextErrorResponse("pattern is required"), nil
	}

	logging.Debug("glob tool called", "pattern", params.Pattern, "path", params.Path)

	searchPath := params.Path
	if searchPath == "" {
		searchPath = config.WorkingDirectory()
	}

	limit := params.HeadLimit
	if limit <= 0 {
		limit = 100
	}

	// Get enough files to support offset
	fetchLimit := limit + params.Offset
	if fetchLimit < limit { // overflow protection
		fetchLimit = limit
	}
	files, truncated, err := globFiles(params.Pattern, searchPath, fetchLimit+1) // +1 to detect has_more
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error finding files: %w", err)
	}

	totalFound := len(files)

	// Apply offset
	if params.Offset > 0 && params.Offset < len(files) {
		files = files[params.Offset:]
	} else if params.Offset >= len(files) {
		files = []string{}
	}

	// Apply limit
	hasMore := false
	if len(files) > limit {
		files = files[:limit]
		hasMore = true
	}
	if truncated {
		hasMore = true
	}

	logging.Debug("glob completed", "fileCount", len(files), "truncated", truncated, "totalFound", totalFound, "offset", params.Offset)

	var output string
	if len(files) == 0 {
		output = "No files found"
	} else {
		output = strings.Join(files, "\n")
		if hasMore || truncated {
			output += "\n\n(Results are truncated. Consider using a more specific path or pattern, or use offset/head_limit for pagination.)"
		}
	}

	return WithResponseMetadata(
		NewTextResponse(output),
		GlobResponseMetadata{
			NumberOfFiles: len(files),
			Truncated:     truncated || hasMore,
			TotalFound:    totalFound,
			Offset:        params.Offset,
			HasMore:       hasMore,
		},
	), nil
}

func globFiles(pattern, searchPath string, limit int) ([]string, bool, error) {
	ignoreMatcher, _ := search.LoadIgnoreFiles(searchPath)

	opts := search.WalkOptions{
		RootPath:      searchPath,
		Pattern:       nil, // file listing only, no content search
		IncludeGlob:   pattern,
		IgnoreMatcher: ignoreMatcher,
		MaxResults:    limit,
	}

	fileMatches, truncated, err := search.SearchFiles(context.Background(), opts)
	if err != nil {
		logging.Warn(fmt.Sprintf("Native search failed: %v. Falling back to doublestar.", err))
		return fileutil.GlobWithDoublestar(pattern, searchPath, limit)
	}

	matches := make([]string, len(fileMatches))
	for i, fm := range fileMatches {
		matches[i] = fm.Path
	}
	return matches, truncated, nil
}
