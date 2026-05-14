package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

type SourcegraphParams struct {
	Query         string `json:"query"`
	Count         int    `json:"count,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
	Timeout       int    `json:"timeout,omitempty"`
}

type SourcegraphResponseMetadata struct {
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

type sourcegraphTool struct {
	client *http.Client
}

const (
	SourcegraphToolName        = "sourcegraph"
	sourcegraphGraphQLURL      = "https://sourcegraph.com/.api/graphql"
	sourcegraphStreamURL       = "https://sourcegraph.com/.api/search/stream"
	sourcegraphToolDescription = `Search code across public repositories using Sourcegraph.

WHEN TO USE THIS TOOL:
- Use when you need to find code examples or implementations across public repositories
- Helpful for researching how others have solved similar problems
- Useful for discovering patterns and best practices in open source code

HOW TO USE:
- Provide a search query using Sourcegraph's query syntax
- Optionally specify the number of results to return (default: 10)
- Optionally set a timeout for the request

QUERY SYNTAX:
- Basic search: "fmt.Println" searches for exact matches
- File filters: "file:.go fmt.Println" limits to Go files
- Repository filters: "repo:^github\.com/golang/go$ fmt.Println" limits to specific repos
- Language filters: "lang:go fmt.Println" limits to Go code
- Boolean operators: "fmt.Println AND log.Fatal" for combined terms
- Regular expressions: "fmt\.(Print|Printf|Println)" for pattern matching
- Quoted strings: "\"exact phrase\"" for exact phrase matching
- Exclude filters: "-file:test" or "-repo:forks" to exclude matches

ADVANCED FILTERS:
- Repository filters:
  * "repo:name" - Match repositories with name containing "name"
  * "repo:^github\.com/org/repo$" - Exact repository match
  * "repo:org/repo@branch" - Search specific branch
  * "repo:org/repo rev:branch" - Alternative branch syntax
  * "-repo:name" - Exclude repositories
  * "fork:yes" or "fork:only" - Include or only show forks
  * "archived:yes" or "archived:only" - Include or only show archived repos
  * "visibility:public" or "visibility:private" - Filter by visibility

- File filters:
  * "file:\.js$" - Files with .js extension
  * "file:internal/" - Files in internal directory
  * "-file:test" - Exclude test files
  * "file:has.content(Copyright)" - Files containing "Copyright"
  * "file:has.contributor([email protected])" - Files with specific contributor

- Content filters:
  * "content:\"exact string\"" - Search for exact string
  * "-content:\"unwanted\"" - Exclude files with unwanted content
  * "case:yes" - Case-sensitive search

- Type filters:
  * "type:symbol" - Search for symbols (functions, classes, etc.)
  * "type:file" - Search file content only
  * "type:path" - Search filenames only
  * "type:diff" - Search code changes
  * "type:commit" - Search commit messages

- Commit/diff search:
  * "after:\"1 month ago\"" - Commits after date
  * "before:\"2023-01-01\"" - Commits before date
  * "author:name" - Commits by author
  * "message:\"fix bug\"" - Commits with message

- Result selection:
  * "select:repo" - Show only repository names
  * "select:file" - Show only file paths
  * "select:content" - Show only matching content
  * "select:symbol" - Show only matching symbols

- Result control:
  * "count:100" - Return up to 100 results
  * "count:all" - Return all results
  * "timeout:30s" - Set search timeout

EXAMPLES:
- "file:.go context.WithTimeout" - Find Go code using context.WithTimeout
- "lang:typescript useState type:symbol" - Find TypeScript React useState hooks
- "repo:^github\.com/kubernetes/kubernetes$ pod list type:file" - Find Kubernetes files related to pod listing
- "repo:sourcegraph/sourcegraph$ after:\"3 months ago\" type:diff database" - Recent changes to database code
- "file:Dockerfile (alpine OR ubuntu) -content:alpine:latest" - Dockerfiles with specific base images
- "repo:has.path(\.py) file:requirements.txt tensorflow" - Python projects using TensorFlow

BOOLEAN OPERATORS:
- "term1 AND term2" - Results containing both terms
- "term1 OR term2" - Results containing either term
- "term1 NOT term2" - Results with term1 but not term2
- "term1 and (term2 or term3)" - Grouping with parentheses

LIMITATIONS:
- Only searches public repositories
- Rate limits may apply
- Complex queries may take longer to execute
- Maximum of 20 results per query

TIPS:
- Use specific file extensions to narrow results
- Add repo: filters for more targeted searches
- Use type:symbol to find function/method definitions
- Use type:file to find relevant files`
)

func NewSourcegraphTool() BaseTool {
	return &sourcegraphTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *sourcegraphTool) Info() ToolInfo {
	return ToolInfo{
		Name:        SourcegraphToolName,
		Description: sourcegraphToolDescription,
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The Sourcegraph search query",
			},
			"count": map[string]any{
				"type":        "number",
				"description": "Optional number of results to return (default: 10, max: 20)",
			},
			"context_window": map[string]any{
				"type":        "number",
				"description": "The context around the match to return (default: 10 lines)",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Optional timeout in seconds (max 120)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *sourcegraphTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params SourcegraphParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse sourcegraph parameters: " + err.Error()), nil
	}

	if params.Query == "" {
		return NewTextErrorResponse("Query parameter is required"), nil
	}

	if params.Count <= 0 {
		params.Count = 10
	} else if params.Count > 20 {
		params.Count = 20
	}

	if params.ContextWindow <= 0 {
		params.ContextWindow = 10
	}

	client := t.client
	if params.Timeout > 0 {
		maxTimeout := 120
		if params.Timeout > maxTimeout {
			params.Timeout = maxTimeout
		}
		client = &http.Client{
			Timeout: time.Duration(params.Timeout) * time.Second,
		}
	}

	// Read token from config at call time (hot-reloadable).
	token := ""
	if cfg := config.Get(); cfg != nil {
		token = strings.TrimSpace(cfg.InternalTools.SourcegraphToken)
	}

	if token != "" {
		return t.runGraphQL(ctx, client, params, token)
	}
	return t.runStream(ctx, client, params)
}

// runGraphQL uses the authenticated GraphQL API.
func (t *sourcegraphTool) runGraphQL(ctx context.Context, client *http.Client, params SourcegraphParams, token string) (ToolResponse, error) {
	type graphqlRequest struct {
		Query     string `json:"query"`
		Variables struct {
			Query string `json:"query"`
		} `json:"variables"`
	}

	request := graphqlRequest{
		Query: "query Search($query: String!) { search(query: $query, version: V2, patternType: keyword ) { results { matchCount, limitHit, resultCount, approximateResultCount, missing { name }, timedout { name }, indexUnavailable, results { __typename, ... on FileMatch { repository { name }, file { path, url, content }, lineMatches { preview, lineNumber, offsetAndLengths } } } } } }",
	}
	request.Variables.Query = params.Query

	graphqlQueryBytes, err := json.Marshal(request)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", sourcegraphGraphQLURL, bytes.NewBuffer(graphqlQueryBytes))
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pando/1.0")
	req.Header.Set("Authorization", "token "+token)

	resp, err := client.Do(req)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 {
			return NewTextErrorResponse(fmt.Sprintf("Request failed with status code: %d, response: %s", resp.StatusCode, string(body))), nil
		}
		return NewTextErrorResponse(fmt.Sprintf("Request failed with status code: %d", resp.StatusCode)), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to read response body: %w", err)
	}

	var result map[string]any
	if err = json.Unmarshal(body, &result); err != nil {
		return ToolResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	formattedResults, err := formatSourcegraphResults(result, params.ContextWindow)
	if err != nil {
		return NewTextErrorResponse("Failed to format results: " + err.Error()), nil
	}

	return NewTextResponse(formattedResults), nil
}

// streamMatch represents a content match from the Sourcegraph streaming API.
type streamMatch struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	Repository string `json:"repository"`
	// content matches
	ChunkMatches []struct {
		Content      string `json:"content"`
		ContentStart struct {
			Line int `json:"line"`
		} `json:"contentStart"`
	} `json:"chunkMatches"`
	// symbol matches
	Symbols []struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	} `json:"symbols"`
}

// runStream uses the unauthenticated public streaming search API as fallback.
func (t *sourcegraphTool) runStream(ctx context.Context, client *http.Client, params SourcegraphParams) (ToolResponse, error) {
	u, err := url.Parse(sourcegraphStreamURL)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to build stream URL: %w", err)
	}
	q := u.Query()
	q.Set("q", params.Query)
	q.Set("display", fmt.Sprintf("%d", params.Count))
	q.Set("v", "V2")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "pando/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to fetch stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return NewTextErrorResponse(fmt.Sprintf("Stream request failed with status %d: %s", resp.StatusCode, string(body))), nil
	}

	var matches []streamMatch
	scanner := bufio.NewScanner(resp.Body)

	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if eventType == "matches" && len(matches) < params.Count {
				var batch []streamMatch
				if err := json.Unmarshal([]byte(data), &batch); err == nil {
					for _, m := range batch {
						if len(matches) >= params.Count {
							break
						}
						matches = append(matches, m)
					}
				}
			}
			if eventType == "done" {
				break
			}
			eventType = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return ToolResponse{}, fmt.Errorf("error reading stream: %w", err)
	}

	return NewTextResponse(formatStreamResults(matches, params.ContextWindow)), nil
}

func formatStreamResults(matches []streamMatch, contextWindow int) string {
	var buf strings.Builder
	buf.WriteString("# Sourcegraph Search Results (public API)\n\n")

	if len(matches) == 0 {
		buf.WriteString("No results found. Try a different query.\n")
		return buf.String()
	}

	buf.WriteString(fmt.Sprintf("Found %d matches\n\n", len(matches)))

	for i, m := range matches {
		switch m.Type {
		case "content":
			buf.WriteString(fmt.Sprintf("## Result %d: %s/%s\n\n", i+1, m.Repository, m.Path))
			for _, chunk := range m.ChunkMatches {
				startLine := chunk.ContentStart.Line + 1
				buf.WriteString(fmt.Sprintf("Line %d:\n", startLine))
				buf.WriteString("```\n")
				buf.WriteString(chunk.Content)
				buf.WriteString("\n```\n\n")
			}
		case "path":
			buf.WriteString(fmt.Sprintf("## Result %d: %s/%s\n\n", i+1, m.Repository, m.Path))
		case "symbol":
			buf.WriteString(fmt.Sprintf("## Result %d: %s/%s\n\n", i+1, m.Repository, m.Path))
			for _, sym := range m.Symbols {
				buf.WriteString(fmt.Sprintf("- `%s` (%s)\n", sym.Name, sym.Kind))
			}
			buf.WriteString("\n")
		case "repo":
			buf.WriteString(fmt.Sprintf("## Result %d: %s\n\n", i+1, m.Repository))
		}
	}

	return buf.String()
}

func formatSourcegraphResults(result map[string]any, contextWindow int) (string, error) {
	var buffer strings.Builder

	if errors, ok := result["errors"].([]any); ok && len(errors) > 0 {
		buffer.WriteString("## Sourcegraph API Error\n\n")
		for _, err := range errors {
			if errMap, ok := err.(map[string]any); ok {
				if message, ok := errMap["message"].(string); ok {
					buffer.WriteString(fmt.Sprintf("- %s\n", message))
				}
			}
		}
		return buffer.String(), nil
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid response format: missing data field")
	}

	search, ok := data["search"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid response format: missing search field")
	}

	searchResults, ok := search["results"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid response format: missing results field")
	}

	matchCount, _ := searchResults["matchCount"].(float64)
	resultCount, _ := searchResults["resultCount"].(float64)
	limitHit, _ := searchResults["limitHit"].(bool)

	buffer.WriteString("# Sourcegraph Search Results\n\n")
	buffer.WriteString(fmt.Sprintf("Found %d matches across %d results\n", int(matchCount), int(resultCount)))

	if limitHit {
		buffer.WriteString("(Result limit reached, try a more specific query)\n")
	}

	buffer.WriteString("\n")

	results, ok := searchResults["results"].([]any)
	if !ok || len(results) == 0 {
		buffer.WriteString("No results found. Try a different query.\n")
		return buffer.String(), nil
	}

	maxResults := 10
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	for i, res := range results {
		fileMatch, ok := res.(map[string]any)
		if !ok {
			continue
		}

		typeName, _ := fileMatch["__typename"].(string)
		if typeName != "FileMatch" {
			continue
		}

		repo, _ := fileMatch["repository"].(map[string]any)
		file, _ := fileMatch["file"].(map[string]any)
		lineMatches, _ := fileMatch["lineMatches"].([]any)

		if repo == nil || file == nil {
			continue
		}

		repoName, _ := repo["name"].(string)
		filePath, _ := file["path"].(string)
		fileURL, _ := file["url"].(string)
		fileContent, _ := file["content"].(string)

		buffer.WriteString(fmt.Sprintf("## Result %d: %s/%s\n\n", i+1, repoName, filePath))

		if fileURL != "" {
			buffer.WriteString(fmt.Sprintf("URL: %s\n\n", fileURL))
		}

		if len(lineMatches) > 0 {
			for _, lm := range lineMatches {
				lineMatch, ok := lm.(map[string]any)
				if !ok {
					continue
				}

				lineNumber, _ := lineMatch["lineNumber"].(float64)
				preview, _ := lineMatch["preview"].(string)

				if fileContent != "" {
					lines := strings.Split(fileContent, "\n")

					buffer.WriteString("```\n")

					startLine := max(1, int(lineNumber)-contextWindow)

					for j := startLine - 1; j < int(lineNumber)-1 && j < len(lines); j++ {
						if j >= 0 {
							buffer.WriteString(fmt.Sprintf("%d| %s\n", j+1, lines[j]))
						}
					}

					buffer.WriteString(fmt.Sprintf("%d|  %s\n", int(lineNumber), preview))

					endLine := int(lineNumber) + contextWindow

					for j := int(lineNumber); j < endLine && j < len(lines); j++ {
						if j < len(lines) {
							buffer.WriteString(fmt.Sprintf("%d| %s\n", j+1, lines[j]))
						}
					}

					buffer.WriteString("```\n\n")
				} else {
					buffer.WriteString("```\n")
					buffer.WriteString(fmt.Sprintf("%d| %s\n", int(lineNumber), preview))
					buffer.WriteString("```\n\n")
				}
			}
		}
	}

	return buffer.String(), nil
}
