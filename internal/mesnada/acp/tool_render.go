package acp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// parseJSONInput attempts to decode a JSON string into a native map or slice.
// ACP clients expect rawInput to be a JSON object, not an encoded string.
// If decoding fails the original string is returned as-is.
func parseJSONInput(s string) interface{} {
	if s == "" {
		return map[string]interface{}{}
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

func toDisplayPath(path string, cwd string) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	if strings.TrimSpace(cwd) == "" {
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

// mapToolKind maps a Pando tool name to the corresponding ACP ToolKind.
func mapToolKind(toolName string) acpsdk.ToolKind {
	switch strings.ToLower(toolName) {
	case "bash", "execute_command":
		return acpsdk.ToolKindExecute
	case "edit", "write", "multiedit", "patch":
		return acpsdk.ToolKindEdit
	case "read", "view", "ls":
		return acpsdk.ToolKindRead
	case "glob", "grep",
		"c7_resolve_library_id", "c7_get_library_docs",
		"brave_search", "google_search", "perplexity_search", "exa_search":
		return acpsdk.ToolKindSearch
	case "web_search", "web_fetch", "fetch":
		return acpsdk.ToolKindFetch
	case "agent", "task", "todowrite":
		return acpsdk.ToolKindThink
	case "exitplanmode":
		return acpsdk.ToolKindSwitchMode
	default:
		return acpsdk.ToolKindOther
	}
}

func toolDisplayTitle(toolName string, rawInput interface{}, cwd string) string {
	switch strings.ToLower(toolName) {
	case "bash", "execute_command":
		if m, ok := rawInput.(map[string]interface{}); ok {
			if command, ok := m["command"].(string); ok && strings.TrimSpace(command) != "" {
				return command
			}
		}
		return toolName
	case "read", "view":
		if path := toolInputString(rawInput, "file_path"); path != "" {
			displayPath := toDisplayPath(path, cwd)
			if limit := readRangeLabel(rawInput); limit != "" {
				return "Read " + displayPath + limit
			}
			return "Read " + displayPath
		}
		return "Read"
	case "write":
		if path := toolInputString(rawInput, "file_path"); path != "" {
			return "Write " + toDisplayPath(path, cwd)
		}
		return "Write"
	case "edit", "multiedit", "patch":
		if path := toolInputString(rawInput, "file_path"); path != "" {
			return "Edit " + toDisplayPath(path, cwd)
		}
		return "Edit"
	case "glob":
		path := toolInputString(rawInput, "path")
		pattern := toolInputString(rawInput, "pattern")
		displayPath := toDisplayPath(path, cwd)
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
		if url := toolInputString(rawInput, "url"); url != "" {
			return "Fetch " + url
		}
		return "Fetch"
	case "web_search":
		if query := toolInputString(rawInput, "query"); query != "" {
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

func readRangeLabel(rawInput interface{}) string {
	limit := toolInputInt(rawInput, "limit")
	offset := toolInputInt(rawInput, "offset")
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
		if toolInputBool(rawInput, key) {
			parts = append(parts, key)
		}
	}
	for _, key := range []string{"-A", "-B", "-C"} {
		if v := toolInputInt(rawInput, key); v > 0 {
			parts = append(parts, key, fmt.Sprintf("%d", v))
		}
	}
	switch toolInputString(rawInput, "output_mode") {
	case "files_with_matches":
		parts = append(parts, "-l")
	case "count":
		parts = append(parts, "-c")
	}
	if headLimit := toolInputInt(rawInput, "head_limit"); headLimit > 0 {
		parts = append(parts, fmt.Sprintf("| head -%d", headLimit))
	}
	if glob := toolInputString(rawInput, "glob"); glob != "" {
		parts = append(parts, fmt.Sprintf("--include=%q", glob))
	}
	if include := toolInputString(rawInput, "include"); include != "" {
		parts = append(parts, fmt.Sprintf("--include=%q", include))
	}
	if fileType := toolInputString(rawInput, "type"); fileType != "" {
		parts = append(parts, "--type="+fileType)
	}
	if toolInputBool(rawInput, "multiline") {
		parts = append(parts, "-P")
	}
	if pattern := toolInputString(rawInput, "pattern"); pattern != "" {
		parts = append(parts, fmt.Sprintf("%q", pattern))
	}
	if path := toolInputString(rawInput, "path"); path != "" {
		parts = append(parts, toDisplayPath(path, cwd))
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

func toolCallContent(toolName string, rawInput interface{}) []acpsdk.ToolCallContent {
	switch strings.ToLower(toolName) {
	case "write":
		path := toolInputString(rawInput, "file_path")
		content := toolInputString(rawInput, "content")
		if path != "" && content != "" {
			return []acpsdk.ToolCallContent{acpsdk.ToolDiffContent(path, content)}
		}
	case "edit", "multiedit", "patch":
		path := toolInputString(rawInput, "file_path")
		oldString := toolInputString(rawInput, "old_string")
		newString := toolInputString(rawInput, "new_string")
		if path != "" && (oldString != "" || newString != "") {
			return []acpsdk.ToolCallContent{acpsdk.ToolDiffContent(path, newString, oldString)}
		}
	case "bash", "execute_command":
		if description := toolInputString(rawInput, "description"); description != "" {
			return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(description))}
		}
	case "agent", "task":
		if prompt := toolInputString(rawInput, "prompt"); prompt != "" {
			return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(prompt))}
		}
	case "web_fetch", "fetch":
		if prompt := toolInputString(rawInput, "prompt"); prompt != "" {
			return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(prompt))}
		}
	}
	return nil
}

func toolResultContent(toolName, content string, isError bool) []acpsdk.ToolCallContent {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	if isError {
		return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(
			"```\n" + content + "\n```",
		))}
	}

	// Format output by tool type to match claude-agent-acp behaviour.
	switch strings.ToLower(toolName) {
	case "read", "view":
		// Wrap file content in fenced code block (markdown escape).
		return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(
			markdownEscapeCodeBlock(content),
		))}

	case "bash", "execute_command":
		// Bash output is handled separately via terminal_output _meta.
		// Return a console code block as fallback for clients that don't support terminals.
		trimmed := strings.TrimSpace(content)
		if trimmed == "" {
			return nil
		}
		return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(
			"```console\n" + trimmed + "\n```",
		))}

	case "glob", "grep", "ls":
		// Search results: wrap in code block for readability.
		return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(
			"```\n" + content + "\n```",
		))}

	default:
		return []acpsdk.ToolCallContent{acpsdk.ToolContent(acpsdk.TextBlock(content))}
	}
}

// markdownEscapeCodeBlock wraps text in a fenced code block, choosing a fence
// string that doesn't collide with any backtick sequences inside the text.
// This mirrors claude-agent-acp's markdownEscape() function.
func markdownEscapeCodeBlock(text string) string {
	fence := "```"
	for {
		if !strings.Contains(text, fence) {
			break
		}
		fence += "`"
	}
	suffix := ""
	if !strings.HasSuffix(text, "\n") {
		suffix = "\n"
	}
	return fence + "\n" + text + suffix + fence
}

func toolInputString(rawInput interface{}, key string) string {
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

func toolInputInt(rawInput interface{}, key string) int {
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

func toolInputBool(rawInput interface{}, key string) bool {
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

// toolLocationInput holds fields used to extract file/directory path from a tool
// call input for different tool types. Each field corresponds to the JSON key used
// by the respective tool.
type toolLocationInput struct {
	FilePath string `json:"file_path"` // edit, write, view, read
	Path     string `json:"path"`      // glob, grep, ls
}

// toLocations extracts file/directory locations from a tool call input JSON string.
// This mirrors opencode's toLocations() function and is used so ACP clients (VS Code,
// Zed, JetBrains) can show which files are being accessed while a tool runs.
func toLocations(toolName, inputJSON string) []acpsdk.ToolCallLocation {
	if inputJSON == "" {
		return nil
	}
	var inp toolLocationInput
	if err := json.Unmarshal([]byte(inputJSON), &inp); err != nil {
		return nil
	}

	switch toolName {
	case "edit", "write", "patch", "multiedit", "view", "read":
		if inp.FilePath != "" {
			return []acpsdk.ToolCallLocation{{Path: inp.FilePath}}
		}
	case "glob", "grep", "ls":
		if inp.Path != "" {
			return []acpsdk.ToolCallLocation{{Path: inp.Path}}
		}
	}
	return nil
}

// parseTodoWritePlan converts the JSON input of a TodoWrite tool call into a slice
// of ACP PlanEntry values. The input schema is:
//
//	{"todos": [{"content": "...", "status": "pending|in_progress|completed", "priority": "high|medium|low"}, ...]}
//
// Unknown status/priority values fall back to "pending" / "medium".
func parseTodoWritePlan(inputJSON string) []acpsdk.PlanEntry {
	if inputJSON == "" {
		return nil
	}
	var raw struct {
		Todos []struct {
			Content  string `json:"content"`
			Status   string `json:"status"`
			Priority string `json:"priority"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &raw); err != nil {
		return nil
	}
	if len(raw.Todos) == 0 {
		return nil
	}
	entries := make([]acpsdk.PlanEntry, 0, len(raw.Todos))
	for _, t := range raw.Todos {
		content := strings.TrimSpace(t.Content)
		if content == "" {
			continue
		}
		entries = append(entries, acpsdk.NewPlanEntry(
			content,
			acpsdk.ParsePlanEntryStatus(t.Status),
			acpsdk.ParsePlanEntryPriority(t.Priority),
		))
	}
	return entries
}
