// Package evaluatortools provides MCP tool wrappers for Pando's self-improvement
// evaluator subsystem. It lives in its own package to avoid an import cycle:
// internal/llm/tools ← internal/evaluator ← internal/llm/provider ← internal/llm/tools.
package evaluatortools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// ---------------------------------------------------------------------------
// pando_evaluator_stats – returns UCB rankings, skill count, avg reward
// ---------------------------------------------------------------------------

type evaluatorStatsTool struct {
	svc evaluator.Service
}

// NewEvaluatorStatsTool creates a tool that returns the self-improvement
// evaluator's current statistics (UCB rankings, skill library summary, etc.).
func NewEvaluatorStatsTool(svc evaluator.Service) tools.BaseTool {
	return &evaluatorStatsTool{svc: svc}
}

func (t *evaluatorStatsTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        "pando_evaluator_stats",
		Description: "Returns the current self-improvement evaluator statistics: total session evaluations, UCB-ranked prompt templates, skill library count, top skills, and average reward score.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}
}

func (t *evaluatorStatsTool) Run(ctx context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	if t.svc == nil || !t.svc.IsEnabled() {
		return tools.NewTextErrorResponse("evaluator is not enabled"), nil
	}
	stats, err := t.svc.GetStats(ctx)
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("failed to get evaluator stats: %v", err)), nil
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("failed to marshal stats: %v", err)), nil
	}
	return tools.NewTextResponse(string(data)), nil
}

// ---------------------------------------------------------------------------
// pando_evaluator_skills – lists active skills optionally filtered by task type
// ---------------------------------------------------------------------------

type evaluatorSkillsParams struct {
	TaskType string `json:"task_type"`
}

type evaluatorSkillsTool struct {
	svc evaluator.Service
}

// NewEvaluatorSkillsTool creates a tool that lists active skills from the
// self-improvement skill library, optionally filtered by task type.
func NewEvaluatorSkillsTool(svc evaluator.Service) tools.BaseTool {
	return &evaluatorSkillsTool{svc: svc}
}

func (t *evaluatorSkillsTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        "pando_evaluator_skills",
		Description: "Lists active skills from the self-improvement skill library. Optionally filter by task_type (e.g. 'coding', 'debugging'). Returns skill title, content, success_rate, and usage_count.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_type": map[string]interface{}{
					"type":        "string",
					"description": "Optional task type filter (e.g. 'coding', 'debugging'). Leave empty to return all active skills.",
				},
			},
			"required": []string{},
		},
	}
}

func (t *evaluatorSkillsTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	if t.svc == nil || !t.svc.IsEnabled() {
		return tools.NewTextErrorResponse("evaluator is not enabled"), nil
	}

	var params evaluatorSkillsParams
	if call.Input != "" && call.Input != "{}" {
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return tools.NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
		}
	}

	skills, err := t.svc.GetActiveSkills(ctx, params.TaskType)
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("failed to get skills: %v", err)), nil
	}

	data, err := json.MarshalIndent(skills, "", "  ")
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("failed to marshal skills: %v", err)), nil
	}
	return tools.NewTextResponse(string(data)), nil
}

// ---------------------------------------------------------------------------
// pando_evaluator_evaluate – trigger evaluation of a specific session
// ---------------------------------------------------------------------------

type evaluatorEvaluateParams struct {
	SessionID string `json:"session_id"`
}

type evaluatorEvaluateTool struct {
	svc evaluator.Service
}

// NewEvaluatorEvaluateTool creates a tool that triggers evaluation of a
// completed session, computing its reward score and extracting skills.
func NewEvaluatorEvaluateTool(svc evaluator.Service) tools.BaseTool {
	return &evaluatorEvaluateTool{svc: svc}
}

func (t *evaluatorEvaluateTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        "pando_evaluator_evaluate",
		Description: "Triggers self-improvement evaluation for a specific session. Computes reward score, extracts learnable skills, and updates UCB template statistics. Requires a valid session_id.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "The session ID to evaluate.",
				},
			},
			"required": []string{"session_id"},
		},
	}
}

func (t *evaluatorEvaluateTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	if t.svc == nil || !t.svc.IsEnabled() {
		return tools.NewTextErrorResponse("evaluator is not enabled"), nil
	}

	var params evaluatorEvaluateParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if params.SessionID == "" {
		return tools.NewTextErrorResponse("session_id is required"), nil
	}

	if err := t.svc.EvaluateSession(ctx, params.SessionID); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("evaluation failed: %v", err)), nil
	}
	return tools.NewTextResponse(fmt.Sprintf("evaluation triggered for session %s", params.SessionID)), nil
}
