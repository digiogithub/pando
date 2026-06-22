package tools

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/pkg/mesnada/models"
	"github.com/google/uuid"
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
	// Base set of built-in engines.
	engineEnum := []string{"pando", "copilot", "claude", "gemini", "opencode", "mistral", "acp", "acp-claude", "acp-codex", "ollama-claude", "ollama-opencode"}
	engineDesc := "CLI engine to use for the task. 'pando' (default) runs Pando itself as a CLI subprocess — NOT an ACP agent. Other CLI engines: copilot, claude, gemini, opencode, mistral. ACP engines: acp (requires acp_agent), acp-claude, acp-codex. NOTE: choosing engine=pando with model=copilot.gpt-5.4 does NOT use the copilot engine; it uses the Copilot provider inside Pando CLI."

	// Append custom template engines when available.
	if customEngines := t.orchestrator.ListCustomEngines(); len(customEngines) > 0 {
		engineEnum = append(engineEnum, customEngines...)
		engineDesc += " Custom template engines: " + strings.Join(customEngines, ", ") + "."
	}

	return ToolInfo{
		Name:        mesnadaSpawnToolName,
		Description: "Creates and executes a new Mesnada orchestrator task, or relaunches an existing task in-place. When task_id is provided the task is reset and re-executed preserving its ID so dependent tasks are automatically unblocked.\n\nSimplest usage — provide only 'prompt': the task runs with engine=pando using the currently active coder model, in background (fire-and-forget). No need to specify engine, model, or background unless you want to override the defaults.\n\nAfter spawning background work, PREFER calling mesnada_await and ending your turn over polling with mesnada_get_task/mesnada_list_tasks: mesnada_await registers a non-blocking wait and you are automatically resumed with the results when they finish.",
		Parameters: map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "Optional: ID of an existing task to relaunch in-place. When set, the task is reset to pending and re-executed using the stored configuration (overridden by any provided prompt/engine/model/timeout). Dependent tasks are automatically unblocked when the relaunched task completes.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The prompt or instruction for the spawned task. Required when creating a new task; optional override when relaunching via task_id.",
			},
			"work_dir": map[string]any{
				"type":        "string",
				"description": "Working directory for the task.",
			},
			"project": map[string]any{
				"type":        "string",
				"description": "Optional: target a registered project by its id, display name, or directory path. The task is routed to that project's warm instance when warm reuse is enabled, and its work_dir defaults to the project's directory. Returns an error listing the known projects if the reference does not match a registered project.",
			},
			"engine": map[string]any{
				"type":        "string",
				"description": engineDesc,
				"enum":        engineEnum,
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model identifier. Omit to use the default model configured for the chosen engine. For engine=pando use 'provider.model' format (e.g. 'copilot.modelname', 'anthropic.modelname', 'google.modelname'). For other engines use their native short model name. Only set this when the user explicitly requests a specific model.",
			},
			"background": map[string]any{
				"type":        "boolean",
				"description": "Run in background (true) or wait for completion (false).",
			},
			"timeout": map[string]any{
				"type":        "string",
				"description": "Timeout duration such as 30m or 1h.",
			},
			"dependencies": map[string]any{
				"type":        "array",
				"description": "List of task IDs that must complete before this task starts.",
				"items": map[string]any{
					"type": "string",
				},
			},
			"include_dependency_logs": map[string]any{
				"type":        "boolean",
				"description": "Append logs from dependency tasks to the prompt before execution.",
			},
			"dependency_log_lines": map[string]any{
				"type":        "integer",
				"description": "Number of lines to include from each dependency log when include_dependency_logs is true.",
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

// spawnCorrelation derives the delegation correlation metadata for a spawned
// task without touching the DB: the parent session id from the tool context, a
// freshly generated idempotency correlation id, and the canonical project path
// for the effective work dir. ProjectID is intentionally left empty here (the
// registry lookup is deferred to a later phase). This is a pure helper so it can
// be unit-tested in isolation.
func spawnCorrelation(ctx context.Context, workDir string) (parentSessionID, correlationID, projectPath string, depth int) {
	if v, ok := ctx.Value(SessionIDContextKey).(string); ok {
		parentSessionID = v
	}
	correlationID = uuid.NewString()
	effectiveWorkDir := workDir
	if effectiveWorkDir == "" {
		effectiveWorkDir = "."
	}
	projectPath = config.CanonicalProjectPath(effectiveWorkDir)
	// Depth = parent depth + 1. The parent's depth is propagated to delegated pando
	// subprocesses via PANDO_DELEGATION_DEPTH (see spawner_pando_cli.go). When unset
	// or invalid (an interactive top-level pando), the parent depth is 0, so a
	// top-level spawn is depth 1, its children depth 2, etc. This feeds the MaxDepth
	// anti-fork-bomb cap enforced by the delegation supervisor.
	parentDepth := 0
	if raw := os.Getenv("PANDO_DELEGATION_DEPTH"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			parentDepth = n
		}
	}
	depth = parentDepth + 1
	return parentSessionID, correlationID, projectPath, depth
}

// knownProjectsHint formats the registered projects into a short, helpful hint
// for the "unknown project" error returned by the spawn tool's project target
// (item B1). It lists up to a handful of "name (id)" entries so the model can
// retry with a valid reference. Returns a generic message when none are known.
func knownProjectsHint(refs []orchestrator.ProjectRef) string {
	if len(refs) == 0 {
		return "No projects are registered; omit 'project' or register one first."
	}
	const maxList = 10
	parts := make([]string, 0, len(refs))
	for i, r := range refs {
		if i >= maxList {
			parts = append(parts, fmt.Sprintf("…and %d more", len(refs)-maxList))
			break
		}
		name := r.Name
		if name == "" {
			name = r.Path
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", name, r.ID))
	}
	return "Known projects: " + strings.Join(parts, ", ") + "."
}

func (t *MesnadaSpawnTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	type spawnParams struct {
		TaskID                string   `json:"task_id"`
		Prompt                string   `json:"prompt"`
		WorkDir               string   `json:"work_dir"`
		Project               string   `json:"project"`
		Engine                string   `json:"engine"`
		Model                 string   `json:"model"`
		Background            *bool    `json:"background"`
		Timeout               string   `json:"timeout"`
		Dependencies          []string `json:"dependencies"`
		IncludeDependencyLogs bool     `json:"include_dependency_logs"`
		DependencyLogLines    int      `json:"dependency_log_lines"`
		Tags                  []string `json:"tags"`
	}

	var req spawnParams
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	background := true
	if req.Background != nil {
		background = *req.Background
	}

	// Relaunch an existing task in-place when task_id is provided.
	if req.TaskID != "" {
		logging.Debug("mesnada relaunch called", "task_id", req.TaskID, "engine", req.Engine, "model", req.Model, "background", background)

		task, err := t.orchestrator.Relaunch(ctx, req.TaskID, orchestrator.RelaunchOptions{
			Prompt:     req.Prompt,
			Engine:     normalizeMesnadaEngine(req.Engine),
			Model:      req.Model,
			Timeout:    req.Timeout,
			Background: background,
		})
		if err != nil {
			return NewTextErrorResponse(err.Error()), nil
		}

		logging.Debug("mesnada task relaunched", "taskID", task.ID, "status", string(task.Status))
		result := map[string]any{
			"task_id":    task.ID,
			"status":     task.Status,
			"work_dir":   task.WorkDir,
			"created_at": task.CreatedAt,
			"relaunched": true,
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

	if req.Prompt == "" {
		return NewTextErrorResponse("prompt is required"), nil
	}

	// Resolve an explicit project target (item B1): when the caller names a
	// registered project by id/name/path, route the delegated task to that
	// project (its warm instance when reuse is enabled) and default the work_dir
	// to the project's directory. Fail fast with a helpful list when unknown so a
	// typo never silently runs in the wrong place.
	var targetProjectID string
	if strings.TrimSpace(req.Project) != "" {
		if t.orchestrator == nil || !t.orchestrator.ProjectRefsSupported() {
			return NewTextErrorResponse("the 'project' argument is not supported in this configuration (no project registry available)"), nil
		}
		ref, ok := t.orchestrator.ResolveProjectRef(ctx, req.Project)
		if !ok {
			return NewTextErrorResponse(fmt.Sprintf(
				"unknown project %q. %s", req.Project, knownProjectsHint(t.orchestrator.ListProjectRefs(ctx)))), nil
		}
		targetProjectID = ref.ID
		if req.WorkDir == "" {
			req.WorkDir = ref.Path
		}
	}

	// Apply configured defaults for new tasks when engine and/or model are
	// omitted. This block is intentionally placed after the relaunch check so
	// relaunches preserve the original task's engine/model.
	if req.Engine == "" || req.Model == "" {
		cfg := config.Get()
		// Apply configured default engine when none is specified.
		if req.Engine == "" && cfg != nil && cfg.Mesnada.Orchestrator.DefaultEngine != "" {
			req.Engine = cfg.Mesnada.Orchestrator.DefaultEngine
		}
		// Determine the effective engine after normalization. When still empty the
		// orchestrator will fall back to its built-in default (pando), so treat
		// that case the same way.
		effectiveEngine := normalizeMesnadaEngine(req.Engine)
		if effectiveEngine == "" {
			effectiveEngine = models.DefaultEngine()
		}
		// For the pando engine, resolve the active coder model so the subprocess
		// uses the correct provider+model (e.g. "copilot.gpt-5.3-codex").
		// Other engines (copilot CLI, claude CLI, …) do not use provider prefixes
		// and rely on their own built-in defaults when no model is specified.
		if req.Model == "" && effectiveEngine == models.EnginePando {
			if cfg != nil {
				req.Model = string(cfg.Agents[config.AgentCoder].Model)
			}
		}
	}

	logging.Debug("mesnada spawn called", "prompt_length", len(req.Prompt), "engine", req.Engine, "model", req.Model, "background", background)

	// Capture delegation correlation metadata (parent session, idempotency id,
	// canonical project path) so the created task records its origin. This never
	// changes runtime behavior; the fields are only consumed by later phases.
	parentSessionID, correlationID, projectPath, depth := spawnCorrelation(ctx, req.WorkDir)

	// MaxConcurrent soft guard (default-off): when delegation is enabled and a cap
	// is configured, refuse to spawn a new task while the parent session already has
	// MaxConcurrent or more non-terminal correlated tasks outstanding. This nudges
	// the model to wait for in-flight delegated work instead of fork-bombing. Gated
	// strictly so default behaviour is unchanged.
	if parentSessionID != "" && t.orchestrator != nil {
		if dcfg := config.Get(); dcfg != nil {
			del := dcfg.Mesnada.Delegation
			if del.Enabled && del.MaxConcurrent > 0 {
				if siblings, lerr := t.orchestrator.ListByParentSession(parentSessionID); lerr == nil {
					outstanding := 0
					for _, st := range siblings {
						if st != nil && !st.IsTerminal() {
							outstanding++
						}
					}
					if outstanding >= del.MaxConcurrent {
						return NewTextErrorResponse(fmt.Sprintf(
							"delegation concurrency limit reached: %d delegated task(s) are still running for this session (max %d). Wait for existing tasks to finish (e.g. mesnada_wait_task or mesnada_list_tasks) before spawning more.",
							outstanding, del.MaxConcurrent)), nil
					}
				}
			}
		}
	}

	task, err := t.orchestrator.Spawn(ctx, models.SpawnRequest{
		Prompt:                req.Prompt,
		WorkDir:               req.WorkDir,
		Engine:                normalizeMesnadaEngine(req.Engine),
		Model:                 req.Model,
		Background:            background,
		Timeout:               req.Timeout,
		Dependencies:          req.Dependencies,
		IncludeDependencyLogs: req.IncludeDependencyLogs,
		DependencyLogLines:    req.DependencyLogLines,
		Tags:                  req.Tags,
		ParentSessionID:       parentSessionID,
		CorrelationID:         correlationID,
		ProjectID:             targetProjectID,
		ProjectPath:           projectPath,
		Depth:                 depth,
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

	result := map[string]any{
		"task_id":  task.ID,
		"status":   task.Status,
		"output":   output,
		"log_file": task.LogFile,
		"is_tail":  useTail,
	}
	if task.Error != "" {
		result["error"] = task.Error
	}
	if task.RawError != "" {
		result["raw_error"] = task.RawError
	}

	return encodeMesnadaResult(result)
}

func decodeMesnadaInput(input string, target any) error {
	return DecodeToolInput(input, target)
}

func encodeMesnadaResult(result any) (ToolResponse, error) {
	return NewStructuredResponse(result), nil
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
	// Preserve unknown engine names so the manager can resolve them against
	// the custom template registry. An empty string means "use default".
	if engine != "" {
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
