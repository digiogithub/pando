package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/search"
)

type GrepParams struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	Include     string `json:"include"`
	LiteralText bool   `json:"literal_text"`
	// Extended parameters
	OutputMode string `json:"output_mode"` // "content" | "files_with_matches" | "count" (default: "files_with_matches")
	Context    int    `json:"context"`     // lines of context before AND after (-C)
	Before     int    `json:"before"`      // lines before match (-B), overrides Context
	After      int    `json:"after"`       // lines after match (-A), overrides Context
	Type       string `json:"type"`        // file type filter: "go", "js", "py", "ts", "rust", etc.
	Multiline  bool   `json:"multiline"`   // enable multiline matching
	HeadLimit  int    `json:"head_limit"`  // cap total results (0 = use default 100)
	Offset     int    `json:"offset"`      // skip first N results for pagination
}

type grepMatch struct {
	path      string
	modTime   time.Time
	lineNum   int
	lineText  string
	isContext bool
}

type GrepResponseMetadata struct {
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

type grepTool struct{}

const (
	GrepToolName    = "grep"
	grepDescription = `Fast content search tool that finds files containing specific text or patterns.

WHEN TO USE THIS TOOL:
- Use when you need to find files containing specific text or patterns
- Great for searching codebases for function names, variable declarations, or error messages
- Useful for finding all files that use a particular API or pattern

HOW TO USE:
- Provide a regex pattern to search for within file contents
- Set literal_text=true to search for exact text with special characters escaped
- Use output_mode to control what is returned:
  - "files_with_matches" (default): returns file paths sorted by modification time (newest first)
  - "content": returns matching lines with optional context lines around them
  - "count": returns the number of matching lines per file
- Use type to restrict to a specific language/file type (e.g. "go", "ts", "py", "rust")
- Use context (or before/after) to show surrounding lines with output_mode=content
- Use head_limit and offset for pagination of large result sets

REGEX PATTERN SYNTAX (when literal_text=false):
- 'function' searches for the literal text "function"
- 'log\..*Error' finds text starting with "log." and ending with "Error"
- 'import\s+.*\s+from' finds import statements in JavaScript/TypeScript

COMMON TYPE VALUES:
- go, js, ts, py, rust, c, cpp, java, json, yaml, toml, md, sh, sql, html, css, docker

COMMON INCLUDE PATTERN EXAMPLES (alternative to type):
- '*.js' - Only search JavaScript files
- '*.{ts,tsx}' - Only search TypeScript files
- '*.go' - Only search Go files

LIMITATIONS:
- Results are limited to 100 by default (use head_limit to change)
- Binary files are automatically skipped
- Hidden files and directories (starting with '.') are skipped

TIPS:
- For faster, more targeted searches, first use Glob to find relevant files, then use Grep
- Use output_mode=content with context=3 to get surrounding code for better understanding
- Use output_mode=count to quickly gauge how widespread a pattern is
- Use type instead of include when searching by language for cleaner syntax
- Responses with more than 300 lines are automatically cached in session memory
- Use cache_read with the returned cache_id to access additional pages of cached results`
)

func NewGrepTool() BaseTool {
	return &grepTool{}
}

func (g *grepTool) Info() ToolInfo {
	return ToolInfo{
		Name:        GrepToolName,
		Description: grepDescription,
		Parameters: map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "The regex pattern to search for in file contents",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "The directory to search in. Defaults to the current working directory.",
			},
			"include": map[string]any{
				"type":        "string",
				"description": "File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\"). Superseded by type if both are set.",
			},
			"literal_text": map[string]any{
				"type":        "boolean",
				"description": "If true, the pattern will be treated as literal text with special regex characters escaped. Default is false.",
			},
			"output_mode": map[string]any{
				"type":        "string",
				"description": "Output mode: \"files_with_matches\" (default, returns file paths), \"content\" (returns matching lines with context), \"count\" (returns match count per file)",
			},
			"context": map[string]any{
				"type":        "integer",
				"description": "Number of context lines to show before and after each match (like -C in grep). Only used when output_mode is \"content\".",
			},
			"before": map[string]any{
				"type":        "integer",
				"description": "Number of context lines to show before each match (like -B in grep). Overrides context. Only used when output_mode is \"content\".",
			},
			"after": map[string]any{
				"type":        "integer",
				"description": "Number of context lines to show after each match (like -A in grep). Overrides context. Only used when output_mode is \"content\".",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "File type filter. Restricts search to files matching the type's extensions. Common values: go, js, ts, py, rust, c, cpp, java, json, yaml, toml, md, sh, sql, html, css, docker. Takes precedence over include pattern.",
			},
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "Enable multiline matching mode. When true, the pattern can match across multiple lines.",
			},
			"head_limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return. Defaults to 100. Use with offset for pagination.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Skip the first N results. Used with head_limit for pagination.",
			},
		},
		Required: []string{"pattern"},
	}
}

// escapeRegexPattern escapes special regex characters so they're treated as literal characters
func escapeRegexPattern(pattern string) string {
	specialChars := []string{"\\", ".", "+", "*", "?", "(", ")", "[", "]", "{", "}", "^", "$", "|"}
	escaped := pattern

	for _, char := range specialChars {
		escaped = strings.ReplaceAll(escaped, char, "\\"+char)
	}

	return escaped
}

func (g *grepTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params GrepParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Pattern == "" {
		return NewTextErrorResponse("pattern is required"), nil
	}

	logging.Debug("grep tool called",
		"pattern", params.Pattern,
		"path", params.Path,
		"include", params.Include,
		"literalText", params.LiteralText,
		"outputMode", params.OutputMode,
		"type", params.Type,
		"headLimit", params.HeadLimit,
		"offset", params.Offset,
	)

	// If literal_text is true, escape the pattern
	searchPattern := params.Pattern
	if params.LiteralText {
		searchPattern = escapeRegexPattern(params.Pattern)
	}

	searchPath := params.Path
	if searchPath == "" {
		searchPath = config.WorkingDirectory()
	} else {
		searchPath = resolveToolPath(searchPath)
	}

	// Resolve context lines: Before/After override Context
	beforeCtx := params.Before
	afterCtx := params.After
	if params.Context > 0 && beforeCtx == 0 {
		beforeCtx = params.Context
	}
	if params.Context > 0 && afterCtx == 0 {
		afterCtx = params.Context
	}

	// Resolve output mode
	outputMode := search.OutputModeFiles
	switch params.OutputMode {
	case "content":
		outputMode = search.OutputModeContent
	case "count":
		outputMode = search.OutputModeCount
	}

	// Resolve limit
	limit := 100
	if params.HeadLimit > 0 {
		limit = params.HeadLimit
	}

	// Compile the regex pattern (cached for session reuse)
	regex, err := getOrCompileRegex(searchPattern)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid regex pattern: %s", err)), nil
	}

	ignoreMatcher, _ := search.LoadIgnoreFiles(searchPath)

	// Collect extra results to support offset slicing after the walk
	collectLimit := limit + params.Offset

	opts := search.WalkOptions{
		RootPath:      searchPath,
		Pattern:       regex,
		IncludeGlob:   params.Include,
		TypeFilter:    params.Type,
		IgnoreMatcher: ignoreMatcher,
		MaxResults:    collectLimit,
		SearchOpts: search.SearchOptions{
			Pattern:    regex,
			OutputMode: outputMode,
			BeforeCtx:  beforeCtx,
			AfterCtx:   afterCtx,
			Multiline:  params.Multiline,
		},
	}

	fileMatches, truncated, err := search.SearchFiles(ctx, opts)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error searching files: %w", err)
	}

	// Apply offset and re-cap to limit
	if params.Offset > 0 {
		if params.Offset >= len(fileMatches) {
			fileMatches = nil
		} else {
			fileMatches = fileMatches[params.Offset:]
		}
	}
	if len(fileMatches) > limit {
		fileMatches = fileMatches[:limit]
		truncated = true
	}

	logging.Debug("grep completed", "fileCount", len(fileMatches), "truncated", truncated)

	output := formatGrepOutput(fileMatches, outputMode, truncated)

	// Count total match lines for metadata
	totalMatches := 0
	for _, fm := range fileMatches {
		switch outputMode {
		case search.OutputModeCount:
			totalMatches += fm.Count
		case search.OutputModeContent:
			for _, lm := range fm.Lines {
				if !lm.IsContext {
					totalMatches++
				}
			}
		default:
			if len(fm.Lines) > 0 {
				totalMatches++
			} else {
				totalMatches++
			}
		}
	}

	return WithResponseMetadata(
		NewTextResponse(output),
		GrepResponseMetadata{
			NumberOfMatches: totalMatches,
			Truncated:       truncated,
		},
	), nil
}

// formatGrepOutput formats the search results depending on output mode.
func formatGrepOutput(fileMatches []search.FileMatch, mode search.OutputMode, truncated bool) string {
	if len(fileMatches) == 0 {
		return "No files found"
	}

	var sb strings.Builder

	switch mode {
	case search.OutputModeContent:
		sb.WriteString(fmt.Sprintf("Found %d file(s) with matches\n\n", len(fileMatches)))
		for _, fm := range fileMatches {
			sb.WriteString(fmt.Sprintf("%s:\n", fm.Path))
			for _, lm := range fm.Lines {
				if lm.IsContext {
					sb.WriteString(fmt.Sprintf("  %d-  %s\n", lm.LineNum, lm.Text))
				} else {
					sb.WriteString(fmt.Sprintf("  %d: >%s\n", lm.LineNum, lm.Text))
				}
			}
			sb.WriteString("\n")
		}

	case search.OutputModeCount:
		total := 0
		for _, fm := range fileMatches {
			sb.WriteString(fmt.Sprintf("%s: %d matches\n", fm.Path, fm.Count))
			total += fm.Count
		}
		sb.WriteString(fmt.Sprintf("\nTotal: %d matches in %d files\n", total, len(fileMatches)))

	default: // OutputModeFiles
		sb.WriteString(fmt.Sprintf("Found %d file(s)\n", len(fileMatches)))
		for _, fm := range fileMatches {
			sb.WriteString(fmt.Sprintf("%s\n", fm.Path))
		}
	}

	if truncated {
		sb.WriteString("\n(Results are truncated. Consider using a more specific path or pattern, or use head_limit/offset for pagination.)")
	}

	return sb.String()
}
