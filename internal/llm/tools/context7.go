package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

const (
	Context7ResolveToolName = "c7_resolve_library_id"
	Context7DocsToolName    = "c7_get_library_docs"
	context7APIBase         = "https://context7.com/api"

	context7ResolveDescription = `Resolves a library/package name to a Context7-compatible library ID.

WHEN TO USE THIS TOOL:
- Always call this BEFORE 'c7_get_library_docs' to get a valid library ID
- When you need to find documentation for any library or framework
- When the user mentions a library by name and you need its exact Context7 ID

HOW TO USE:
- Provide the library name (e.g., "react", "gin", "mongoose", "nextjs")
- Review the results: each shows title, ID, description, snippet count, and GitHub stars
- Choose the most relevant match and use its ID with 'c7_get_library_docs'`

	context7DocsDescription = `Fetches up-to-date documentation for a library from Context7.

WHEN TO USE THIS TOOL:
- After resolving a library ID with 'c7_resolve_library_id'
- When you need API docs, usage examples, or code snippets for a library
- When the user asks how to use a specific library or framework

HOW TO USE:
- Provide the exact Context7-compatible library ID from 'c7_resolve_library_id'
- Optionally specify a 'topic' to focus the docs (e.g., "hooks", "routing", "authentication")
- Optionally set 'tokens' to control the amount of documentation returned (default: 10000)`
)

type context7ResolveTool struct {
	client *http.Client
}

type context7DocsTool struct {
	client *http.Client
}

// NewContext7Tools returns both Context7 tools.
func NewContext7Tools() []BaseTool {
	client := &http.Client{Timeout: 30 * time.Second}
	return []BaseTool{
		&context7ResolveTool{client: client},
		&context7DocsTool{client: client},
	}
}

func (t *context7ResolveTool) Info() ToolInfo {
	return ToolInfo{
		Name:        Context7ResolveToolName,
		Description: context7ResolveDescription,
		Parameters: map[string]any{
			"library_name": map[string]any{
				"type":        "string",
				"description": "The library or package name to search for (e.g., 'react', 'gin', 'express')",
			},
		},
		Required: []string{"library_name"},
	}
}

func (t *context7ResolveTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		LibraryName string `json:"library_name"`
	}
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.LibraryName) == "" {
		return NewTextErrorResponse("library_name parameter is required"), nil
	}

	logging.Debug("c7_resolve_library_id called", "library_name", params.LibraryName)

	apiURL := fmt.Sprintf("%s/v1/search?query=%s", context7APIBase, url.QueryEscape(params.LibraryName))
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Context7-Source", "pando")

	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return NewTextErrorResponse("Failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return NewTextErrorResponse(fmt.Sprintf("Context7 API error %d: %s", resp.StatusCode, string(body))), nil
	}

	var result struct {
		Results []struct {
			Title         string `json:"title"`
			ID            string `json:"id"`
			Description   string `json:"description"`
			TotalSnippets int    `json:"totalSnippets"`
			Stars         int    `json:"stars"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return NewTextErrorResponse("Failed to parse Context7 response: " + err.Error()), nil
	}

	if len(result.Results) == 0 {
		return NewTextResponse(fmt.Sprintf("No libraries found matching: %s", params.LibraryName)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Context7: Libraries matching \"%s\"\n\n", params.LibraryName))

	for _, r := range result.Results {
		sb.WriteString(fmt.Sprintf("### %s\n", r.Title))
		sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", r.ID))
		if r.Description != "" {
			sb.WriteString(fmt.Sprintf("**Description:** %s\n", r.Description))
		}
		if r.TotalSnippets > 0 || r.Stars > 0 {
			sb.WriteString(fmt.Sprintf("**Code Snippets:** %d | **GitHub Stars:** %d\n", r.TotalSnippets, r.Stars))
		}
		sb.WriteString("\n---\n\n")
	}

	sb.WriteString("> Use the **ID** with `c7_get_library_docs` to fetch up-to-date documentation.\n")

	return NewTextResponse(sb.String()), nil
}

func (t *context7DocsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        Context7DocsToolName,
		Description: context7DocsDescription,
		Parameters: map[string]any{
			"context7_compatible_library_id": map[string]any{
				"type":        "string",
				"description": "Exact Context7-compatible library ID (e.g., 'mongodb/docs', '/vercel/nextjs') from 'c7_resolve_library_id'",
			},
			"topic": map[string]any{
				"type":        "string",
				"description": "Topic to focus documentation on (e.g., 'hooks', 'routing', 'authentication')",
			},
			"tokens": map[string]any{
				"type":        "number",
				"description": "Maximum tokens of documentation to retrieve (default: 10000)",
			},
		},
		Required: []string{"context7_compatible_library_id"},
	}
}

func (t *context7DocsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		LibraryID string `json:"context7_compatible_library_id"`
		Topic     string `json:"topic"`
		Tokens    int    `json:"tokens"`
	}
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.LibraryID) == "" {
		return NewTextErrorResponse("context7_compatible_library_id parameter is required"), nil
	}

	logging.Debug("c7_get_library_docs called", "library_id", params.LibraryID)

	// Extract ?folders= suffix from library ID if present
	originalID := params.LibraryID
	foldersValue := ""
	if idx := strings.Index(originalID, "?folders="); idx != -1 {
		foldersValue = originalID[idx+len("?folders="):]
		originalID = originalID[:idx]
	}

	// Strip leading slash for URL path segment
	pathSegment := strings.TrimPrefix(originalID, "/")

	// Build query params
	q := url.Values{}
	q.Set("context7CompatibleLibraryID", params.LibraryID)
	if foldersValue != "" {
		q.Set("folders", foldersValue)
	}
	if params.Topic != "" {
		q.Set("topic", params.Topic)
	}
	tokens := 10000
	if params.Tokens > 0 {
		tokens = params.Tokens
	}
	q.Set("tokens", fmt.Sprint(tokens))

	apiURL := fmt.Sprintf("%s/v1/%s/?%s", context7APIBase, pathSegment, q.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Context7-Source", "pando")

	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return NewTextErrorResponse("Failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return NewTextErrorResponse(fmt.Sprintf("Context7 API error %d: %s", resp.StatusCode, string(body))), nil
	}

	// Build header
	topicInfo := ""
	if params.Topic != "" {
		topicInfo = fmt.Sprintf(" (topic: %s)", params.Topic)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Context7 Docs: %s%s\n\n", params.LibraryID, topicInfo))
	sb.Write(body)

	return NewTextResponse(sb.String()), nil
}
