package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/conclusion"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

const mesnadaAwaitToolName = "mesnada_await"

// defaultAwaitTimeout is the safety deadline applied when the caller does not
// provide an explicit timeout. It must be generous: it only bounds the case where
// an awaited task never reaches a terminal state, so the parent loop can still be
// woken (with whatever completed) instead of sleeping forever.
const defaultAwaitTimeout = time.Hour

// awaitOrchestrator is the narrow slice of *orchestrator.Orchestrator the await
// tool needs. Factoring it out keeps Run unit-testable with a fake. The concrete
// orchestrator satisfies it.
type awaitOrchestrator interface {
	ListByParentSession(sessionID string) ([]*models.Task, error)
	SetAwaitIntent(intent *orchestrator.AwaitIntent)
}

// MesnadaAwaitTool registers a NON-BLOCKING wait intent for one or more delegated
// tasks and instructs the model to end its turn. It never blocks: the delegation
// supervisor resurrects the parent session when the join condition is satisfied
// (or the safety deadline passes). This is the preferred way to wait for
// background delegated work; the blocking mesnada_wait_task tool is reserved for
// short foreground waits.
type MesnadaAwaitTool struct {
	orch awaitOrchestrator
}

// NewMesnadaAwaitTool constructs the await tool over the orchestrator.
func NewMesnadaAwaitTool(orch *orchestrator.Orchestrator) BaseTool {
	return &MesnadaAwaitTool{orch: orch}
}

func (t *MesnadaAwaitTool) Info() ToolInfo {
	return ToolInfo{
		Name: mesnadaAwaitToolName,
		Description: "Registers a NON-BLOCKING wait for one or more delegated Mesnada tasks and then tells you to END YOUR TURN. This is the PREFERRED way to wait for background work spawned with mesnada_spawn_agent: it does NOT block and does NOT poll. After calling it you should stop and finish — you will be AUTOMATICALLY RESUMED with the task results once the wait condition is met (or the safety timeout fires). " +
			"Contrast with mesnada_wait_task, which BLOCKS the current turn until a single task finishes — reserve that for short foreground waits where you truly cannot continue. " +
			"If the awaited tasks have already finished, this returns their results immediately so you can continue without ending your turn.",
		Parameters: map[string]any{
			"task_ids": map[string]any{
				"type":        "array",
				"description": "Optional: the delegated task IDs to await. Leave empty to await ALL currently-outstanding delegated tasks spawned by this session.",
				"items": map[string]any{
					"type": "string",
				},
			},
			"policy": map[string]any{
				"type":        "string",
				"description": "Join policy: 'all' (default — resume when every awaited task finishes), 'any' (resume as soon as one finishes), or 'quorum' (resume when at least 'quorum' finish).",
				"enum":        []string{"all", "any", "quorum"},
			},
			"quorum": map[string]any{
				"type":        "integer",
				"description": "Required when policy='quorum': the number of awaited tasks that must finish before you are resumed.",
			},
			"timeout": map[string]any{
				"type":        "string",
				"description": "Optional safety deadline (e.g. '30m', '1h'). If the awaited tasks never finish you are resumed with whatever completed after this duration. Defaults to 1h.",
			},
		},
	}
}

func (t *MesnadaAwaitTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		TaskIDs []string `json:"task_ids"`
		Policy  string   `json:"policy"`
		Quorum  int      `json:"quorum"`
		Timeout string   `json:"timeout"`
	}
	if err := decodeMesnadaInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}

	// 1. Require a session — await is meaningless without one to resurrect.
	sessionID, _ := ctx.Value(SessionIDContextKey).(string)
	if sessionID == "" {
		return NewTextErrorResponse("mesnada_await requires an active agent session; it cannot be used outside a session."), nil
	}

	// Resolve and validate the join policy.
	policy := orchestrator.AwaitPolicy(strings.ToLower(strings.TrimSpace(req.Policy)))
	switch policy {
	case "":
		policy = orchestrator.AwaitPolicyAll
	case orchestrator.AwaitPolicyAll, orchestrator.AwaitPolicyAny, orchestrator.AwaitPolicyQuorum:
		// ok
	default:
		return NewTextErrorResponse(fmt.Sprintf("invalid policy %q: must be one of all, any, quorum", req.Policy)), nil
	}
	if policy == orchestrator.AwaitPolicyQuorum && req.Quorum <= 0 {
		return NewTextErrorResponse("policy=quorum requires a positive 'quorum' value"), nil
	}

	// Parse the safety timeout.
	timeout := defaultAwaitTimeout
	if strings.TrimSpace(req.Timeout) != "" {
		d, err := time.ParseDuration(req.Timeout)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("invalid timeout: %v", err)), nil
		}
		if d > 0 {
			timeout = d
		}
	}

	// 2. Resolve the awaited task set.
	siblings, err := t.orch.ListByParentSession(sessionID)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("failed to list delegated tasks for this session: %v", err)), nil
	}
	byID := make(map[string]*models.Task, len(siblings))
	for _, st := range siblings {
		if st != nil {
			byID[st.ID] = st
		}
	}

	var awaited []*models.Task
	if len(req.TaskIDs) == 0 {
		// Empty => await all currently-outstanding (non-terminal) correlated tasks.
		for _, st := range siblings {
			if st != nil && !st.IsTerminal() {
				awaited = append(awaited, st)
			}
		}
	} else {
		// Validate every provided id belongs to this session (is correlated).
		for _, id := range req.TaskIDs {
			st, ok := byID[id]
			if !ok {
				return NewTextErrorResponse(fmt.Sprintf(
					"task %q is not a delegated task of this session; mesnada_await can only await tasks this session spawned.", id)), nil
			}
			awaited = append(awaited, st)
		}
	}

	awaitedIDs := make([]string, 0, len(awaited))
	for _, st := range awaited {
		awaitedIDs = append(awaitedIDs, st.ID)
	}

	// 3. Short-circuit: evaluate the join condition against CURRENT statuses. If it
	// is already satisfied (e.g. nothing outstanding, or enough awaited tasks are
	// already terminal) do NOT register an intent — return the conclusions now so
	// the model continues immediately and never ends its turn waiting for a wake
	// event that will never come.
	reported := 0
	for _, st := range awaited {
		if st.IsTerminal() {
			reported++
		}
	}
	if joinSatisfied(policy, len(awaited), reported, req.Quorum) {
		return awaitAlreadySatisfiedResponse(policy, awaited), nil
	}

	// 4. Register the intent and tell the model to end its turn.
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	t.orch.SetAwaitIntent(&orchestrator.AwaitIntent{
		SessionID: sessionID,
		TaskIDs:   awaitedIDs,
		Policy:    policy,
		Quorum:    req.Quorum,
		Deadline:  deadline,
		CreatedAt: time.Now(),
	})

	logging.Debug("mesnada await registered",
		"session", sessionID, "tasks", len(awaitedIDs), "policy", string(policy), "timeout", timeout)

	msg := awaitRegisteredMessage(policy, req.Quorum, len(awaitedIDs), timeout)
	return WithResponseMetadata(NewTextResponse(msg), map[string]any{
		"awaited_task_ids": awaitedIDs,
		"policy":           string(policy),
		"quorum":           req.Quorum,
		"timeout":          timeout.String(),
		"end_turn":         true,
	}), nil
}

// joinSatisfied reports whether the join condition holds given the number of
// awaited tasks, how many have reported (reached a terminal state), the policy and
// the quorum. An empty awaited set is always satisfied (nothing to wait for).
func joinSatisfied(policy orchestrator.AwaitPolicy, total, reported, quorum int) bool {
	if total == 0 {
		return true
	}
	switch policy {
	case orchestrator.AwaitPolicyAny:
		return reported >= 1
	case orchestrator.AwaitPolicyQuorum:
		if quorum <= 0 {
			return reported >= total
		}
		return reported >= quorum
	default: // all
		return reported >= total
	}
}

// awaitAlreadySatisfiedResponse builds the immediate response when the await
// condition is already met, embedding the awaited tasks' conclusions so the model
// can continue without ending its turn.
func awaitAlreadySatisfiedResponse(policy orchestrator.AwaitPolicy, awaited []*models.Task) ToolResponse {
	var b strings.Builder
	if len(awaited) == 0 {
		b.WriteString("No outstanding delegated tasks to await — continue with your work.")
		return NewTextResponse(b.String())
	}
	b.WriteString(fmt.Sprintf("The awaited condition (policy=%s) is already satisfied. Results:\n", string(policy)))
	for _, st := range awaited {
		if st == nil || !st.IsTerminal() {
			continue
		}
		b.WriteString("\n")
		b.WriteString(conclusion.FormatForParent(st))
		b.WriteString("\n")
	}
	b.WriteString("\nContinue with your work — there is nothing left to wait for.")
	return NewTextResponse(b.String())
}

// awaitRegisteredMessage builds the unambiguous "end your turn" instruction
// returned when an intent is registered.
func awaitRegisteredMessage(policy orchestrator.AwaitPolicy, quorum, n int, timeout time.Duration) string {
	cond := string(policy)
	if policy == orchestrator.AwaitPolicyQuorum {
		cond = fmt.Sprintf("quorum=%d", quorum)
	}
	return fmt.Sprintf(
		"Await registered for %d task(s) (policy=%s). You have nothing to do until they report — END YOUR TURN now. Do NOT call more tools and do NOT poll with mesnada_get_task/mesnada_list_tasks. You will be AUTOMATICALLY RESUMED with their results when the condition is met (or after %s).",
		n, cond, timeout)
}
