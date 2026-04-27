package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const TodoWriteToolName = "TodoWrite"

// TodoItem represents a single entry in the todo list.
type TodoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`   // pending | in_progress | completed
	Priority string `json:"priority"` // high | medium | low
}

// TodoWriteParams holds the input for the TodoWrite tool.
type TodoWriteParams struct {
	Todos []TodoItem `json:"todos"`
}

// todoStore holds the current todo list per session (in-memory).
var todoStore sync.Map // map[sessionID string] []TodoItem

// todoCallbacks holds registered callbacks per session that are called on update.
var todoCallbacks sync.Map // map[sessionID string] []func([]TodoItem)
var todoCallbacksMu sync.RWMutex

// RegisterTodoCallback registers a function to be called whenever the todo list
// for sessionID is updated. Multiple callbacks can be registered per session.
func RegisterTodoCallback(sessionID string, cb func([]TodoItem)) {
	todoCallbacksMu.Lock()
	defer todoCallbacksMu.Unlock()
	var cbs []func([]TodoItem)
	if existing, ok := todoCallbacks.Load(sessionID); ok {
		cbs = existing.([]func([]TodoItem))
	}
	cbs = append(cbs, cb)
	todoCallbacks.Store(sessionID, cbs)
}

// UnregisterTodoCallbacks removes all callbacks registered for sessionID.
func UnregisterTodoCallbacks(sessionID string) {
	todoCallbacks.Delete(sessionID)
	todoStore.Delete(sessionID)
}

// GetSessionTodos returns the current todo list for a session, or nil if none.
func GetSessionTodos(sessionID string) []TodoItem {
	if val, ok := todoStore.Load(sessionID); ok {
		return val.([]TodoItem)
	}
	return nil
}

func updateSessionTodos(sessionID string, todos []TodoItem) {
	todoStore.Store(sessionID, todos)

	todoCallbacksMu.RLock()
	defer todoCallbacksMu.RUnlock()
	if val, ok := todoCallbacks.Load(sessionID); ok {
		for _, cb := range val.([]func([]TodoItem)) {
			cb(todos)
		}
	}
}

// todoWriteTool implements the TodoWrite internal tool.
type todoWriteTool struct{}

// NewTodoWriteTool creates a new TodoWrite tool instance.
func NewTodoWriteTool() BaseTool {
	return &todoWriteTool{}
}

func (t *todoWriteTool) Info() ToolInfo {
	return ToolInfo{
		Name: TodoWriteToolName,
		Description: `Write and maintain a structured todo list to track tasks and their progress.
Use this tool to plan multi-step tasks, update task statuses as work progresses,
and keep both yourself and the user informed about what remains to be done.

Guidelines:
- Add tasks at the start of multi-step work to establish a clear plan.
- Update statuses as tasks complete: pending → in_progress → completed.
- Provide clear, actionable task descriptions.
- Prioritise tasks using high/medium/low to reflect urgency and importance.`,
		Parameters: map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "The complete, updated list of todo items. Always send the full list.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "Short description of the task.",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed"},
							"description": "Current status of the task.",
						},
						"priority": map[string]any{
							"type":        "string",
							"enum":        []string{"high", "medium", "low"},
							"description": "Relative priority of the task.",
						},
					},
					"required": []string{"content", "status", "priority"},
				},
			},
		},
		Required: []string{"todos"},
	}
}

func (t *todoWriteTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params TodoWriteParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("invalid input: " + err.Error()), nil
	}

	// Filter out empty entries.
	clean := make([]TodoItem, 0, len(params.Todos))
	for _, item := range params.Todos {
		if strings.TrimSpace(item.Content) != "" {
			clean = append(clean, item)
		}
	}

	sessionID, _ := GetContextValues(ctx)
	if sessionID != "" {
		updateSessionTodos(sessionID, clean)
	}

	return NewTextResponse(formatTodoResponse(clean)), nil
}

// formatTodoResponse builds a human-readable confirmation for the LLM.
func formatTodoResponse(todos []TodoItem) string {
	if len(todos) == 0 {
		return "TODO list cleared."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("TODO list updated (%d items):\n", len(todos)))
	for i, item := range todos {
		icon := todoStatusIcon(item.Status)
		sb.WriteString(fmt.Sprintf("%d. %s [%s] %s\n", i+1, icon, item.Priority, item.Content))
	}
	return sb.String()
}

// TodoWriteSummary returns a short single-line summary of the todo list, capped
// to maxWidth runes, suitable for TUI parameter display.
func TodoWriteSummary(todos []TodoItem, maxWidth int) string {
	if len(todos) == 0 {
		return "cleared"
	}
	parts := make([]string, 0, len(todos))
	for _, t := range todos {
		parts = append(parts, strings.TrimSpace(t.Content))
	}
	summary := strings.Join(parts, ", ")
	if maxWidth > 0 && len([]rune(summary)) > maxWidth {
		runes := []rune(summary)
		if maxWidth > 3 {
			summary = string(runes[:maxWidth-3]) + "..."
		} else {
			summary = string(runes[:maxWidth])
		}
	}
	return summary
}

func todoStatusIcon(status string) string {
	switch strings.ToLower(status) {
	case "completed":
		return "✓"
	case "in_progress":
		return "→"
	default:
		return "○"
	}
}
