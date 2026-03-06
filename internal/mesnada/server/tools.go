package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func (s *Server) registerTools() {
	s.tools["spawn_agent"] = s.toolSpawnAgent
	s.tools["get_task"] = s.toolGetTask
	s.tools["list_tasks"] = s.toolListTasks
	s.tools["wait_task"] = s.toolWaitTask
	s.tools["wait_multiple"] = s.toolWaitMultiple
	s.tools["cancel_task"] = s.toolCancelTask
	s.tools["pause_task"] = s.toolPauseTask
	s.tools["resume_task"] = s.toolResumeTask
	s.tools["delete_task"] = s.toolDeleteTask
	s.tools["get_stats"] = s.toolGetStats
	s.tools["get_task_output"] = s.toolGetTaskOutput
	s.tools["set_progress"] = s.toolSetProgress
	s.tools["acp_session_control"] = s.toolACPSessionControl
}

func (s *Server) detectEngineForModel(modelID string) models.Engine {
	if s.config == nil || s.config.Engines == nil {
		return ""
	}

	engineOrder := []struct {
		engine     models.Engine
		binaryName string
	}{
		{models.EngineClaude, "claude"},
		{models.EngineGemini, "gemini"},
		{models.EngineOpenCode, "opencode"},
		{models.EngineMistral, "vibe"},
		{models.EngineCopilot, "copilot"},
	}

	for _, e := range engineOrder {
		if s.config.GetModelForEngine(string(e.engine), modelID) != nil {
			if _, err := exec.LookPath(e.binaryName); err == nil {
				return e.engine
			}
		}
	}

	return ""
}

func (s *Server) getToolDefinitions() []Tool {
	personas := s.orchestrator.ListPersonas()
	personaDesc := "Optional persona/role to apply to the agent (prepends persona instructions to the prompt)"
	if len(personas) > 0 {
		personaDesc += fmt.Sprintf(". Available personas: %v", personas)
	}

	modelDesc := "AI model to use. Available models depend on the selected engine. "
	if s.config != nil {
		if s.config.Engines != nil && len(s.config.Engines) > 0 {
			modelDesc += "Models by engine: "
			for engineName := range s.config.Engines {
				modelDesc += fmt.Sprintf("%s: %v; ", engineName, s.config.GetModelIDsForEngine(engineName))
			}
		} else if len(s.config.Models) > 0 {
			modelDesc += fmt.Sprintf("Available: %v", s.config.GetModelIDsForEngine(""))
		}
	}

	allModels := make(map[string]bool)
	if s.config != nil {
		if s.config.Engines != nil {
			for engineName := range s.config.Engines {
				for _, modelID := range s.config.GetModelIDsForEngine(engineName) {
					allModels[modelID] = true
				}
			}
		}
		for _, modelID := range s.config.GetModelIDsForEngine("") {
			allModels[modelID] = true
		}
	}
	modelEnum := make([]string, 0, len(allModels))
	for modelID := range allModels {
		modelEnum = append(modelEnum, modelID)
	}

	return []Tool{
		{
			Name:        "spawn_agent",
			Description: "Spawn a new CLI agent to execute a task. Supports multiple engines: 'copilot' (GitHub Copilot CLI, default), 'claude-code' (Anthropic Claude CLI), 'gemini-cli' (Google Gemini CLI), 'opencode' (OpenCode.ai CLI), 'ollama-claude' (Ollama Claude interface), or 'ollama-opencode' (Ollama OpenCode interface). The agent runs in the specified working directory with full tool access. Use background=true for long-running tasks.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The prompt/instruction for the agent to execute",
					},
					"work_dir": map[string]interface{}{
						"type":        "string",
						"description": "Working directory for the agent (absolute path)",
					},
					"engine": map[string]interface{}{
						"type":        "string",
						"description": "CLI engine to use: 'copilot' (GitHub Copilot CLI, default), 'claude-code' (Anthropic Claude CLI), 'gemini-cli' (Google Gemini CLI), 'opencode' (OpenCode.ai CLI), 'ollama-claude' (Ollama Claude interface), or 'ollama-opencode' (Ollama OpenCode interface). If not specified but model is provided, engine will be auto-detected based on the model configuration.",
						"enum":        []string{"copilot", "claude-code", "gemini-cli", "opencode", "ollama-claude", "ollama-opencode"},
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": modelDesc,
						"enum":        modelEnum,
					},
					"background": map[string]interface{}{
						"type":        "boolean",
						"description": "Run in background (true) or wait for completion (false). Default: true",
						"default":     true,
					},
					"timeout": map[string]interface{}{
						"type":        "string",
						"description": "Timeout duration (e.g., '30m', '1h'). Empty for no timeout",
					},
					"dependencies": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "List of task IDs that must complete before this task starts",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Tags for organizing and filtering tasks",
					},
					"mcp_config": map[string]interface{}{
						"type":        "string",
						"description": "Additional MCP configuration JSON or file path (prefix with @)",
					},
					"extra_args": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Additional command-line arguments for the CLI",
					},
					"persona": map[string]interface{}{
						"type":        "string",
						"description": personaDesc,
					},
					"acp_mode": map[string]interface{}{
						"type":        "string",
						"description": "ACP session mode: 'code' (default), 'ask' (read-only), 'architect' (plan only). Only used with ACP engines.",
						"enum":        []string{"code", "ask", "architect"},
					},
					"acp_agent": map[string]interface{}{
						"type":        "string",
						"description": "Name of the ACP agent to use (from config). Only used with 'acp' engine.",
					},
					"acp_config_options": map[string]interface{}{
						"type":        "object",
						"description": "ACP config options to set on the session",
						"properties": map[string]interface{}{
							"thinking_enabled":    map[string]interface{}{"type": "boolean"},
							"max_tokens":          map[string]interface{}{"type": "integer"},
							"custom_instructions": map[string]interface{}{"type": "string"},
						},
					},
					"acp_mcp_servers": map[string]interface{}{
						"type":        "array",
						"description": "Additional MCP servers to attach to the ACP session",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":    map[string]interface{}{"type": "string"},
								"command": map[string]interface{}{"type": "string"},
								"args":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
								"env":     map[string]interface{}{"type": "object"},
							},
						},
					},
				},
				"required": []string{"prompt"},
			},
		},
		{
			Name:        "get_task",
			Description: "Get detailed information about a specific task including status, output, and timing",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to retrieve",
					},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "list_tasks",
			Description: "List tasks with optional filtering by status and tags",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"pending", "running", "paused", "completed", "failed", "cancelled"},
						},
						"description": "Filter by task status",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Filter by tags (tasks must have all specified tags)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of tasks to return",
						"default":     20,
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Number of tasks to skip",
						"default":     0,
					},
				},
			},
		},
		{
			Name:        "wait_task",
			Description: "Wait for a specific task to complete. Returns the task when it reaches a terminal state (completed, failed, or cancelled)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to wait for",
					},
					"timeout": map[string]interface{}{
						"type":        "string",
						"description": "Maximum time to wait (e.g., '5m', '1h'). Empty for no timeout",
					},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "wait_multiple",
			Description: "Wait for multiple tasks to complete. Can wait for all tasks or return when any task completes",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_ids": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "List of task IDs to wait for",
					},
					"wait_all": map[string]interface{}{
						"type":        "boolean",
						"description": "Wait for all tasks (true) or return when first completes (false)",
						"default":     true,
					},
					"timeout": map[string]interface{}{
						"type":        "string",
						"description": "Maximum time to wait (e.g., '10m', '1h')",
					},
				},
				"required": []string{"task_ids"},
			},
		},
		{
			Name:        "cancel_task",
			Description: "Cancel a running or pending task",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to cancel",
					},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "pause_task",
			Description: "Pause a running or pending task without marking it as cancelled",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to pause",
					},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "resume_task",
			Description: "Resume a paused task by spawning a new agent task that continues work",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The paused task ID to resume",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Additional resume prompt/instructions",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "AI model to use (optional; defaults to previous task model)",
						"enum":        []string{"claude-sonnet-4.5", "claude-haiku-4.5", "claude-opus-4.5", "claude-sonnet-4", "gpt-5.1-codex-max", "gpt-5.1-codex", "gpt-5.2", "gpt-5.1", "gpt-5", "gpt-5.1-codex-mini", "gpt-5-mini", "gpt-4.1", "gemini-3-pro-preview"},
					},
					"background": map[string]interface{}{
						"type":        "boolean",
						"description": "Run in background (true) or wait for completion (false). Default: true",
						"default":     true,
					},
					"timeout": map[string]interface{}{
						"type":        "string",
						"description": "Timeout duration (e.g., '30m', '1h'). Empty for no timeout",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Tags for organizing and filtering tasks (optional; defaults to previous task tags)",
					},
				},
				"required": []string{"task_id", "prompt"},
			},
		},
		{
			Name:        "delete_task",
			Description: "Delete a completed, failed, or cancelled task from the store",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to delete",
					},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "get_stats",
			Description: "Get orchestrator statistics including task counts by status",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "get_task_output",
			Description: "Get the output (stdout/stderr) of a task. For running tasks, returns current output. For completed tasks, returns full or tail output",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID",
					},
					"tail": map[string]interface{}{
						"type":        "boolean",
						"description": "Return only the last 50 lines (default: false for completed, true for running)",
					},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "set_progress",
			Description: "Update the progress of a running task. This tool should be called by the agent task itself to report its progress. The percentage will be sanitized to be between 0 and 100.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to update progress for",
					},
					"percentage": map[string]interface{}{
						"type":        "integer",
						"description": "Progress percentage (0-100). Any non-numeric characters will be stripped.",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Brief description of current progress or what the task is currently doing",
					},
				},
				"required": []string{"task_id", "percentage"},
			},
		},
		{
			Name:        "acp_session_control",
			Description: "Control an active ACP session. Send follow-up prompts, change mode, or get session status.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Task ID with active ACP session",
					},
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"follow_up", "set_mode", "cancel", "status"},
						"description": "Action to perform",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Follow-up message (for 'follow_up' action)",
					},
					"mode": map[string]interface{}{
						"type":        "string",
						"description": "New mode (for 'set_mode' action)",
						"enum":        []string{"code", "ask", "architect"},
					},
				},
				"required": []string{"task_id", "action"},
			},
		},
	}
}

func (s *Server) toolSpawnAgent(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Prompt           string                 `json:"prompt"`
		WorkDir          string                 `json:"work_dir"`
		Engine           string                 `json:"engine"`
		Model            string                 `json:"model"`
		Background       *bool                  `json:"background"`
		Timeout          string                 `json:"timeout"`
		Dependencies     []string               `json:"dependencies"`
		Tags             []string               `json:"tags"`
		MCPConfig        string                 `json:"mcp_config"`
		ExtraArgs        []string               `json:"extra_args"`
		Persona          string                 `json:"persona"`
		ACPMode          string                 `json:"acp_mode"`
		ACPAgent         string                 `json:"acp_agent"`
		ACPConfigOptions map[string]interface{} `json:"acp_config_options"`
		ACPMCPServers    []models.ACPMCPServer  `json:"acp_mcp_servers"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	background := true
	if req.Background != nil {
		background = *req.Background
	}

	engineName := req.Engine
	switch engineName {
	case "claude-code":
		engineName = "claude"
	case "gemini-cli":
		engineName = "gemini"
	}

	engine := models.Engine(engineName)
	if engine == "" && req.Model != "" {
		engine = s.detectEngineForModel(req.Model)
	}

	task, err := s.orchestrator.Spawn(ctx, models.SpawnRequest{
		Prompt:           req.Prompt,
		WorkDir:          req.WorkDir,
		Engine:           engine,
		Model:            req.Model,
		Background:       background,
		Timeout:          req.Timeout,
		Dependencies:     req.Dependencies,
		Tags:             req.Tags,
		MCPConfig:        req.MCPConfig,
		ExtraArgs:        req.ExtraArgs,
		Persona:          req.Persona,
		ACPMode:          req.ACPMode,
		ACPAgent:         req.ACPAgent,
		ACPConfigOptions: req.ACPConfigOptions,
		ACPMCPServers:    req.ACPMCPServers,
	})
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
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

			if s.config != nil && engine != "" {
				availableModels := s.config.GetModelIDsForEngine(string(engine))
				if len(availableModels) > 0 {
					result["available_models"] = availableModels
					result["engine"] = string(engine)
					result["suggestion"] = fmt.Sprintf("Try one of these models for engine '%s': %v", engine, availableModels)
				}
			}
		}
	}

	return result, nil
}

func (s *Server) toolGetTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	task, err := s.orchestrator.GetTask(req.TaskID)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"task": task,
	}

	if s.config != nil && task.Status == models.TaskStatusFailed && task.Engine != "" {
		availableModels := s.config.GetModelIDsForEngine(string(task.Engine))
		if len(availableModels) > 0 {
			result["available_models"] = availableModels
			result["suggestion"] = fmt.Sprintf("Task failed. Try one of these models for engine '%s': %v", task.Engine, availableModels)
		}
	}

	return result, nil
}

func (s *Server) toolListTasks(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Status []string `json:"status"`
		Tags   []string `json:"tags"`
		Limit  int      `json:"limit"`
		Offset int      `json:"offset"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	var statuses []models.TaskStatus
	for _, status := range req.Status {
		statuses = append(statuses, models.TaskStatus(status))
	}

	if req.Limit == 0 {
		req.Limit = 20
	}

	tasks, err := s.orchestrator.ListTasks(models.ListRequest{
		Status: statuses,
		Tags:   req.Tags,
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}

	summaries := make([]models.TaskSummary, len(tasks))
	for i, task := range tasks {
		summaries[i] = task.ToSummary()
	}

	return map[string]interface{}{
		"tasks": summaries,
		"total": len(summaries),
	}, nil
}

func (s *Server) toolWaitTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID  string `json:"task_id"`
		Timeout string `json:"timeout"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	var timeout time.Duration
	if req.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(req.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
	}

	task, err := s.orchestrator.Wait(ctx, req.TaskID, timeout)
	if err != nil {
		if task != nil {
			return map[string]interface{}{
				"task":    task,
				"error":   err.Error(),
				"timeout": true,
			}, nil
		}
		return nil, err
	}

	return map[string]interface{}{
		"task":        task,
		"output_tail": task.OutputTail,
	}, nil
}

func (s *Server) toolWaitMultiple(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskIDs []string `json:"task_ids"`
		WaitAll bool     `json:"wait_all"`
		Timeout string   `json:"timeout"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	var timeout time.Duration
	if req.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(req.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
	}

	results, err := s.orchestrator.WaitMultiple(ctx, req.TaskIDs, req.WaitAll, timeout)

	taskResults := make(map[string]interface{})
	for id, task := range results {
		taskResults[id] = map[string]interface{}{
			"status":      task.Status,
			"output_tail": task.OutputTail,
			"exit_code":   task.ExitCode,
			"error":       task.Error,
		}
	}

	response := map[string]interface{}{
		"tasks":     taskResults,
		"completed": len(results),
		"requested": len(req.TaskIDs),
	}
	if err != nil {
		response["error"] = err.Error()
	}

	return response, nil
}

func (s *Server) toolCancelTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if err := s.orchestrator.Cancel(req.TaskID); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"task_id":   req.TaskID,
		"cancelled": true,
	}, nil
}

func (s *Server) toolPauseTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	task, err := s.orchestrator.Pause(req.TaskID)
	if err != nil {
		return nil, err
	}

	return task, nil
}

func (s *Server) toolResumeTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID     string    `json:"task_id"`
		Prompt     string    `json:"prompt"`
		Model      string    `json:"model"`
		Background *bool     `json:"background"`
		Timeout    string    `json:"timeout"`
		Tags       *[]string `json:"tags"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	background := true
	if req.Background != nil {
		background = *req.Background
	}

	task, err := s.orchestrator.Resume(ctx, req.TaskID, orchestrator.ResumeOptions{
		Prompt:     req.Prompt,
		Model:      req.Model,
		Background: background,
		Timeout:    req.Timeout,
		Tags:       req.Tags,
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"task_id": task.ID,
		"task":    task,
	}, nil
}

func (s *Server) toolDeleteTask(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if err := s.orchestrator.Delete(req.TaskID); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"task_id": req.TaskID,
		"deleted": true,
	}, nil
}

func (s *Server) toolGetStats(ctx context.Context, params json.RawMessage) (interface{}, error) {
	return s.orchestrator.GetStats(), nil
}

func (s *Server) toolGetTaskOutput(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID string `json:"task_id"`
		Tail   *bool  `json:"tail"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	task, err := s.orchestrator.GetTask(req.TaskID)
	if err != nil {
		return nil, err
	}

	useTail := task.IsRunning()
	if req.Tail != nil {
		useTail = *req.Tail
	}

	output := task.Output
	if useTail {
		output = task.OutputTail
	}

	return map[string]interface{}{
		"task_id":  task.ID,
		"status":   task.Status,
		"output":   output,
		"log_file": task.LogFile,
		"is_tail":  useTail,
	}, nil
}

func (s *Server) toolSetProgress(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID      string      `json:"task_id"`
		Percentage  interface{} `json:"percentage"`
		Description string      `json:"description"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	percentage := 0
	switch v := req.Percentage.(type) {
	case float64:
		percentage = int(v)
	case int:
		percentage = v
	case string:
		sanitized := ""
		for _, ch := range v {
			if (ch >= '0' && ch <= '9') || (ch == '-' && len(sanitized) == 0) {
				sanitized += string(ch)
			}
		}
		if sanitized != "" {
			_, _ = fmt.Sscanf(sanitized, "%d", &percentage)
		}
	default:
		return nil, fmt.Errorf("invalid percentage type: %T", v)
	}

	if err := s.orchestrator.SetProgress(req.TaskID, percentage, req.Description); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"task_id":     req.TaskID,
		"percentage":  percentage,
		"description": req.Description,
		"updated":     true,
	}, nil
}

func (s *Server) toolACPSessionControl(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		TaskID  string `json:"task_id"`
		Action  string `json:"action"`
		Message string `json:"message"`
		Mode    string `json:"mode"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if req.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if req.Action == "" {
		return nil, fmt.Errorf("action is required")
	}

	validActions := map[string]bool{
		"follow_up": true,
		"set_mode":  true,
		"cancel":    true,
		"status":    true,
	}
	if !validActions[req.Action] {
		return nil, fmt.Errorf("invalid action: %s (valid: follow_up, set_mode, cancel, status)", req.Action)
	}

	if req.Action == "set_mode" {
		if req.Mode == "" {
			return nil, fmt.Errorf("mode parameter required for set_mode action")
		}
		validModes := map[string]bool{"code": true, "ask": true, "architect": true}
		if !validModes[req.Mode] {
			return nil, fmt.Errorf("invalid mode: %s (valid: code, ask, architect)", req.Mode)
		}
	}

	if req.Action == "follow_up" && req.Message == "" {
		return nil, fmt.Errorf("message parameter required for follow_up action")
	}

	return s.orchestrator.ACPSessionControl(req.TaskID, req.Action, req.Message, req.Mode)
}
