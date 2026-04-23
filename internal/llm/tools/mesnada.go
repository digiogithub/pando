package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

const (
	mesnadaSpawnToolName     = "mesnada_spawn_agent"
	mesnadaGetTaskToolName   = "mesnada_get_task"
	mesnadaListTasksToolName = "mesnada_list_tasks"
	mesnadaWaitTaskToolName  = "mesnada_wait_task"
	mesnadaCancelToolName    = "mesnada_cancel_task"
	mesnadaOutputToolName    = "mesnada_get_task_output"
)

type mesnadaTool struct {
	orchestrator *orchestrator.Orchestrator
}

// MesnadaSpawnTool creates and runs a Mesnada task directly through the orchestrator.
type MesnadaSpawnTool struct {
	mesnadaTool
}

// MesnadaGetTaskTool returns full task information for a Mesnada task.
type MesnadaGetTaskTool struct {
	mesnadaTool
}

// MesnadaListTasksTool lists Mesnada tasks using optional filters.
type MesnadaListTasksTool struct {
	mesnadaTool
}

// MesnadaWaitTaskTool waits for a Mesnada task to reach a terminal state.
type MesnadaWaitTaskTool struct {
	mesnadaTool
}

// MesnadaCancelTaskTool cancels a running or pending Mesnada task.
type MesnadaCancelTaskTool struct {
	mesnadaTool
}

// MesnadaGetOutputTool returns task output for a Mesnada task.
type MesnadaGetOutputTool struct {
	mesnadaTool
}

func NewMesnadaSpawnTool(orch *orchestrator.Orchestrator) BaseTool {
	return &MesnadaSpawnTool{mesnadaTool: mesnadaTool{orchestrator: orch}}
}

func NewMesnadaGetTaskTool(orch *orchestrator.Orchestrator) BaseTool {
	return &MesnadaGetTaskTool{mesnadaTool: mesnadaTool{orchestrator: orch}}
}

func NewMesnadaListTasksTool(orch *orchestrator.Orchestrator) BaseTool {
	return &MesnadaListTasksTool{mesnadaTool: mesnadaTool{orchestrator: orch}}
}

func NewMesnadaWaitTaskTool(orch *orchestrator.Orchestrator) BaseTool {
	return &MesnadaWaitTaskTool{mesnadaTool: mesnadaTool{orchestrator: orch}}
}

func NewMesnadaCancelTaskTool(orch *orchestrator.Orchestrator) BaseTool {
	return &MesnadaCancelTaskTool{mesnadaTool: mesnadaTool{orchestrator: orch}}
}

func NewMesnadaGetOutputTool(orch *orchestrator.Orchestrator) BaseTool {
	return &MesnadaGetOutputTool{mesnadaTool: mesnadaTool{orchestrator: orch}}
}

func (t *MesnadaSpawnTool) Info() ToolInfo {
	return ToolInfo{
		Name:        mesnadaSpawnToolName,
		Description: "Creates and executes a new Mesnada orchestrator task.",
		Parameters: map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "The prompt or instruction for the spawned task.",
			},
			"work_dir": map[string]any{
				"type":        "string",
				"description": "Working directory for the task.",
			},
			"engine": map[string]any{
				"type":        "string",
				"description": "CLI engine to use for the task. Available engines: pando (default, runs Pando itself as a subagent via CLI), copilot, claude, gemini, opencode, mistral, acp (requires acp_agent), acp-claude, acp-codex, ollama-claude, ollama-opencode.",
				"enum":        []string{"pando", "copilot", "claude", "gemini", "opencode", "mistral", "acp", "acp-claude", "acp-codex", "ollama-claude", "ollama-opencode"},
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model to use for the task.",
			},
			"background": map[string]any{
				"type":        "boolean",
				"description": "Run in background (true) or wait for completion (false).",
			},
			"timeout": map[string]any{
				"type":        "string",
				"description": "Timeout duration such as 30m or 1h.",
			},
			"tags": map[string]any{
				"type":        "array",
				"description": "Optional tags to associate with the task.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Required: []string{"prompt"},
	}
}

func (t *MesnadaSpawnTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	type spawnParams struct {
		Prompt     string   `json:"prompt"`
		WorkDir    string   `json:"work_dir"`
		Engine     string   `json:"engine"`
		Model      string   `json:"model"`
		Background *bool    `json:"background"`
		Timeout    string   `json:"timeout"`
		Tags       []string `json:"tags"`
	}

	var req spawnParams
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if req.Prompt == "" {
		return NewTextErrorResponse("prompt is required"), nil
	}

	background := true
	if req.Background != nil {
		background = *req.Background
	}

	logging.Debug("mesnada spawn called", "prompt_length", len(req.Prompt), "engine", req.Engine, "model", req.Model, "background", background)

	task, err := t.orchestrator.Spawn(ctx, models.SpawnRequest{
		Prompt:     req.Prompt,
		WorkDir:    req.WorkDir,
		Engine:     normalizeMesnadaEngine(req.Engine),
		Model:      req.Model,
		Background: background,
		Timeout:    req.Timeout,
		Tags:       req.Tags,
	})
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	logging.Debug("mesnada task spawned", "taskID", task.ID, "status", string(task.Status))
	result := map[string]any{
		"task_id":    task.ID,
		"status":     task.Status,
		"work_dir":   task.WorkDir,
		"created_at": task.CreatedAt,
	}
	if !background && task.IsTerminal() {
		result["output_tail"] = task.OutputTail
		result["exit_code"] = task.ExitCode
		if task.Error != "" {
			result["error"] = task.Error
		}
	}

	return encodeMesnadaResult(result)
}

func (t *MesnadaGetTaskTool) Info() ToolInfo {
	return ToolInfo{
		Name:        mesnadaGetTaskToolName,
		Description: "Gets detailed information about a Mesnada task.",
		Parameters: map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The Mesnada task ID to retrieve.",
			},
		},
		Required: []string{"task_id"},
	}
}

func (t *MesnadaGetTaskTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if req.TaskID == "" {
		return NewTextErrorResponse("task_id is required"), nil
	}

	logging.Debug("mesnada get task", "taskID", req.TaskID)
	task, err := t.orchestrator.GetTask(req.TaskID)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	return encodeMesnadaResult(map[string]any{
		"task": task,
	})
}

func (t *MesnadaListTasksTool) Info() ToolInfo {
	return ToolInfo{
		Name:        mesnadaListTasksToolName,
		Description: "Lists Mesnada tasks with optional status and tag filters.",
		Parameters: map[string]any{
			"status": map[string]any{
				"type":        "array",
				"description": "Optional task statuses to include.",
				"items": map[string]any{
					"type": "string",
					"enum": []string{"pending", "running", "paused", "completed", "failed", "cancelled"},
				},
			},
			"tags": map[string]any{
				"type":        "array",
				"description": "Optional tags that returned tasks must include.",
				"items": map[string]any{
					"type": "string",
				},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of tasks to return.",
			},
		},
	}
}

func (t *MesnadaListTasksTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Status []string `json:"status"`
		Tags   []string `json:"tags"`
		Limit  int      `json:"limit"`
	}
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	statuses, err := parseMesnadaStatuses(req.Status)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if req.Limit == 0 {
		req.Limit = 20
	}

	logging.Debug("mesnada list tasks", "statusFilter", req.Status, "tags", req.Tags, "limit", req.Limit)
	tasks, err := t.orchestrator.ListTasks(models.ListRequest{
		Status: statuses,
		Tags:   req.Tags,
		Limit:  req.Limit,
	})
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	summaries := make([]models.TaskSummary, len(tasks))
	for i, task := range tasks {
		summaries[i] = task.ToSummary()
	}

	return encodeMesnadaResult(map[string]any{
		"tasks": summaries,
		"total": len(summaries),
	})
}

func (t *MesnadaWaitTaskTool) Info() ToolInfo {
	return ToolInfo{
		Name:        mesnadaWaitTaskToolName,
		Description: "Waits for a Mesnada task to finish or until the timeout expires.",
		Parameters: map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The Mesnada task ID to wait for.",
			},
			"timeout": map[string]any{
				"type":        "string",
				"description": "Optional timeout duration such as 5m or 1h.",
			},
		},
		Required: []string{"task_id"},
	}
}

func (t *MesnadaWaitTaskTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		TaskID  string `json:"task_id"`
		Timeout string `json:"timeout"`
	}
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if req.TaskID == "" {
		return NewTextErrorResponse("task_id is required"), nil
	}

	logging.Debug("mesnada wait task", "taskID", req.TaskID, "timeout", req.Timeout)
	var timeout time.Duration
	if req.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(req.Timeout)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("invalid timeout: %v", err)), nil
		}
	}

	task, err := t.orchestrator.Wait(ctx, req.TaskID, timeout)
	if err != nil {
		if task != nil {
			return encodeMesnadaResult(map[string]any{
				"task":    task,
				"error":   err.Error(),
				"timeout": true,
			})
		}
		return NewTextErrorResponse(err.Error()), nil
	}

	return encodeMesnadaResult(map[string]any{
		"task":        task,
		"output_tail": task.OutputTail,
	})
}

func (t *MesnadaCancelTaskTool) Info() ToolInfo {
	return ToolInfo{
		Name:        mesnadaCancelToolName,
		Description: "Cancels a running or pending Mesnada task.",
		Parameters: map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The Mesnada task ID to cancel.",
			},
		},
		Required: []string{"task_id"},
	}
}

func (t *MesnadaCancelTaskTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if req.TaskID == "" {
		return NewTextErrorResponse("task_id is required"), nil
	}

	logging.Debug("mesnada cancel task", "taskID", req.TaskID)
	if err := t.orchestrator.Cancel(req.TaskID); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	return encodeMesnadaResult(map[string]any{
		"task_id":   req.TaskID,
		"cancelled": true,
	})
}

func (t *MesnadaGetOutputTool) Info() ToolInfo {
	return ToolInfo{
		Name:        mesnadaOutputToolName,
		Description: "Gets stdout/stderr output for a Mesnada task.",
		Parameters: map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The Mesnada task ID.",
			},
			"tail": map[string]any{
				"type":        "boolean",
				"description": "Return the tail instead of the full output. Defaults to true for running tasks.",
			},
		},
		Required: []string{"task_id"},
	}
}

func (t *MesnadaGetOutputTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		TaskID string `json:"task_id"`
		Tail   *bool  `json:"tail"`
	}
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if req.TaskID == "" {
		return NewTextErrorResponse("task_id is required"), nil
	}

	logging.Debug("mesnada get output", "taskID", req.TaskID)
	task, err := t.orchestrator.GetTask(req.TaskID)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	useTail := task.IsRunning()
	if req.Tail != nil {
		useTail = *req.Tail
	}

	output := task.Output
	if useTail {
		output = task.OutputTail
	}

	return encodeMesnadaResult(map[string]any{
		"task_id":  task.ID,
		"status":   task.Status,
		"output":   output,
		"log_file": task.LogFile,
		"is_tail":  useTail,
	})
}

func decodeMesnadaInput(input string, target any) error {
	if input == "" {
		input = "{}"
	}
	if err := json.Unmarshal([]byte(input), target); err != nil {
		return fmt.Errorf("error parsing parameters: %w", err)
	}
	return nil
}

func encodeMesnadaResult(result any) (ToolResponse, error) {
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ToolResponse{}, fmt.Errorf("encode mesnada result: %w", err)
	}
	return NewTextResponse(string(body)), nil
}

func normalizeMesnadaEngine(engine string) models.Engine {
	switch engine {
	case "claude-code":
		engine = string(models.EngineClaude)
	case "gemini-cli":
		engine = string(models.EngineGemini)
	}

	normalized := models.Engine(engine)
	if models.ValidEngine(normalized) {
		return normalized
	}
	return ""
}

func parseMesnadaStatuses(values []string) ([]models.TaskStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}

	statuses := make([]models.TaskStatus, 0, len(values))
	for _, value := range values {
		status := models.TaskStatus(value)
		switch status {
		case models.TaskStatusPending, models.TaskStatusRunning, models.TaskStatusPaused, models.TaskStatusCompleted, models.TaskStatusFailed, models.TaskStatusCancelled:
			statuses = append(statuses, status)
		default:
			return nil, fmt.Errorf("invalid status: %s", value)
		}
	}

	return statuses, nil
}
