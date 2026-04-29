// Package toolmeta provides shared tool-call metadata helpers used by both
// the ACP prompt handler and the WebUI SSE handler. These functions map Pando
// tool names and their JSON-encoded inputs into display-friendly titles, kinds,
// statuses, and file locations — without depending on any ACP SDK types.
package toolmeta

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// ToolKind categorises a tool call for UI rendering.
type ToolKind string

const (
	ToolKindExecute    ToolKind = "execute"
	ToolKindEdit       ToolKind = "edit"
	ToolKindRead       ToolKind = "read"
	ToolKindSearch     ToolKind = "search"
	ToolKindFetch      ToolKind = "fetch"
	ToolKindThink      ToolKind = "think"
	ToolKindSwitchMode ToolKind = "switch_mode"
	ToolKindOther      ToolKind = "other"
)

// ToolCallStatus represents the lifecycle state of a tool call.
type ToolCallStatus string

const (
	StatusPending    ToolCallStatus = "pending"
	StatusInProgress ToolCallStatus = "in_progress"
	StatusCompleted  ToolCallStatus = "completed"
	StatusFailed     ToolCallStatus = "failed"
)

// Location represents a file/directory affected by a tool call.
type Location struct {
	Path string `json:"path"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Public API
// ──────────────────────────────────────────────────────────────────────────────

// ParseJSONInput decodes a JSON string into a native map/slice.
// Returns an empty map if the string is blank or cannot be parsed.
func ParseJSONInput(s string) interface{} {
	if s == "" {
		return map[string]interface{}{}
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

// MapToolKind returns the ToolKind for a given Pando tool name.
func MapToolKind(toolName string) ToolKind {
	switch strings.ToLower(toolName) {
	case "bash", "execute_command":
		return ToolKindExecute
	case "edit", "write", "multiedit", "patch":
		return ToolKindEdit
	case "read", "view", "ls":
		return ToolKindRead
	case "glob", "grep",
		"c7_resolve_library_id", "c7_get_library_docs",
		"brave_search", "google_search", "perplexity_search", "exa_search":
		return ToolKindSearch
	case "web_search", "web_fetch", "fetch":
		return ToolKindFetch
	case "agent", "task", "todowrite":
		return ToolKindThink
	case "exitplanmode":
		return ToolKindSwitchMode
	default:
		return ToolKindOther
	}
}

// DisplayTitle returns a human-friendly title for a tool call.
func DisplayTitle(toolName string, rawInput interface{}, cwd string) string {
	switch strings.ToLower(toolName) {
	case "bash", "execute_command":
		if m, ok := rawInput.(map[string]interface{}); ok {
			if command, ok := m["command"].(string); ok && strings.TrimSpace(command) != "" {
				return command
			}
		}
		return toolName
	case "read", "view":
		if path := InputString(rawInput, "file_path"); path != "" {
			displayPath := ToDisplayPath(path, cwd)
			if limit := readRangeLabel(rawInput); limit != "" {
				return "Read " + displayPath + limit
			}
			return "Read " + displayPath
		}
		return "Read"
	case "write":
		if path := InputString(rawInput, "file_path"); path != "" {
			return "Write " + ToDisplayPath(path, cwd)
		}
		return "Write"
	case "edit", "multiedit", "patch":
		if path := InputString(rawInput, "file_path"); path != "" {
			return "Edit " + ToDisplayPath(path, cwd)
		}
		return "Edit"
	case "glob":
		path := InputString(rawInput, "path")
		pattern := InputString(rawInput, "pattern")
		displayPath := ToDisplayPath(path, cwd)
		if path != "" && pattern != "" {
			return fmt.Sprintf("Find `%s` `%s`", displayPath, pattern)
		}
		if pattern != "" {
			return fmt.Sprintf("Find `%s`", pattern)
		}
		return "Find"
	case "grep":
		return grepDisplayTitle(rawInput, cwd)
	case "web_fetch", "fetch":
		if url := InputString(rawInput, "url"); url != "" {
			return "Fetch " + url
		}
		return "Fetch"
	case "web_search":
		if query := InputString(rawInput, "query"); query != "" {
			return fmt.Sprintf("%q", query)
		}
		return "WebSearch"
	case "todowrite":
		if todos := todoSummary(rawInput); todos != "" {
			return "Update TODOs: " + todos
		}
		return "Update TODOs"
	case "exitplanmode":
		return "Ready to code?"
	default:
		return toolName
	}
}

// ToLocations extracts file/directory locations from a tool call input string.
func ToLocations(toolName, inputJSON string) []Location {
	if inputJSON == "" {
		return nil
	}
	var inp struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &inp); err != nil {
		return nil
	}
	switch toolName {
	case "edit", "write", "patch", "multiedit", "view", "read":
		if inp.FilePath != "" {
			return []Location{{Path: inp.FilePath}}
		}
	case "glob", "grep", "ls":
		if inp.Path != "" {
			return []Location{{Path: inp.Path}}
		}
	}
	return nil
}

// IsBashTool returns true for tool names that execute shell commands.
func IsBashTool(name string) bool {
	switch strings.ToLower(name) {
	case "bash", "execute_command":
		return true
	}
	return false
}

// IsEditTool returns true for tool names that modify files on disk.
func IsEditTool(name string) bool {
	switch name {
	case "edit", "write", "patch", "multiedit":
		return true
	}
	return false
}

// IsTodoWriteTool returns true for the TodoWrite tool.
func IsTodoWriteTool(name string) bool {
	return strings.EqualFold(name, "TodoWrite")
}

// ToDisplayPath makes a path relative to cwd when possible.
func ToDisplayPath(path string, cwd string) string {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(cwd) == "" {
		return path
	}
	resolvedCwd, err := filepath.Abs(cwd)
	if err != nil {
		return path
	}
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(resolvedCwd, resolvedPath)
	if err != nil {
		return path
	}
	if rel == "." {
		return rel
	}
	if strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// ──────────────────────────────────────────────────────────────────────────────
// Input helpers (exported for reuse)
// ──────────────────────────────────────────────────────────────────────────────

// InputString extracts a string value from a raw input map.
func InputString(rawInput interface{}, key string) string {
	m, ok := rawInput.(map[string]interface{})
	if !ok {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// InputInt extracts an integer value from a raw input map.
func InputInt(rawInput interface{}, key string) int {
	m, ok := rawInput.(map[string]interface{})
	if !ok {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// InputBool extracts a boolean value from a raw input map.
func InputBool(rawInput interface{}, key string) bool {
	m, ok := rawInput.(map[string]interface{})
	if !ok {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

func readRangeLabel(rawInput interface{}) string {
	limit := InputInt(rawInput, "limit")
	offset := InputInt(rawInput, "offset")
	if limit > 0 {
		start := offset
		if start <= 0 {
			start = 1
		}
		return fmt.Sprintf(" (%d - %d)", start, start+limit-1)
	}
	if offset > 0 {
		return fmt.Sprintf(" (from line %d)", offset)
	}
	return ""
}

func grepDisplayTitle(rawInput interface{}, cwd string) string {
	parts := []string{"grep"}
	for _, key := range []string{"-i", "-n"} {
		if InputBool(rawInput, key) {
			parts = append(parts, key)
		}
	}
	for _, key := range []string{"-A", "-B", "-C"} {
		if v := InputInt(rawInput, key); v > 0 {
			parts = append(parts, key, fmt.Sprintf("%d", v))
		}
	}
	switch InputString(rawInput, "output_mode") {
	case "files_with_matches":
		parts = append(parts, "-l")
	case "count":
		parts = append(parts, "-c")
	}
	if headLimit := InputInt(rawInput, "head_limit"); headLimit > 0 {
		parts = append(parts, fmt.Sprintf("| head -%d", headLimit))
	}
	if glob := InputString(rawInput, "glob"); glob != "" {
		parts = append(parts, fmt.Sprintf("--include=%q", glob))
	}
	if include := InputString(rawInput, "include"); include != "" {
		parts = append(parts, fmt.Sprintf("--include=%q", include))
	}
	if fileType := InputString(rawInput, "type"); fileType != "" {
		parts = append(parts, "--type="+fileType)
	}
	if InputBool(rawInput, "multiline") {
		parts = append(parts, "-P")
	}
	if pattern := InputString(rawInput, "pattern"); pattern != "" {
		parts = append(parts, fmt.Sprintf("%q", pattern))
	}
	if path := InputString(rawInput, "path"); path != "" {
		parts = append(parts, ToDisplayPath(path, cwd))
	}
	return strings.Join(parts, " ")
}

func todoSummary(rawInput interface{}) string {
	m, ok := rawInput.(map[string]interface{})
	if !ok {
		return ""
	}
	rawTodos, ok := m["todos"].([]interface{})
	if !ok || len(rawTodos) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rawTodos))
	for _, rawTodo := range rawTodos {
		todo, ok := rawTodo.(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := todo["content"].(string); ok && strings.TrimSpace(content) != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, ", ")
}
