package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/google/uuid"
)

type GoalStore interface {
	CreateGoal(ctx context.Context, arg db.CreateGoalParams) (db.SessionGoal, error)
	UpdateGoalStatus(ctx context.Context, arg db.UpdateGoalStatusParams) (db.SessionGoal, error)
}

type GoalOptions struct {
	MaxIterations   int64
	MaxDuration     time.Duration
	StallIterations int
	InitialPrompt   string
}

type GoalRunner struct {
	agent     Service
	goalStore GoalStore
	evaluator GoalEvaluator
	now       func() time.Time
	newGoalID func() string
}

var (
	ErrGoalMaxIterationsReached = errors.New("goal reached max iterations")
	ErrGoalMaxDurationExceeded  = errors.New("goal exceeded max duration")
)

func NewGoalRunner(agent Service, goalStore GoalStore) *GoalRunner {
	return &GoalRunner{
		agent:     agent,
		goalStore: goalStore,
		evaluator: NewHeuristicGoalEvaluator(),
		now:       time.Now,
		newGoalID: uuid.NewString,
	}
}

func (gr *GoalRunner) Run(ctx context.Context, sessionID string, objective string, opts GoalOptions) (<-chan AgentEvent, error) {
	if gr.agent == nil {
		return nil, errors.New("goal runner requires an agent service")
	}
	if gr.goalStore == nil {
		return nil, errors.New("goal runner requires a goal store")
	}
	if gr.evaluator == nil {
		return nil, errors.New("goal runner requires a goal evaluator")
	}

	objective = strings.TrimSpace(objective)
	if objective == "" {
		return nil, errors.New("goal objective cannot be empty")
	}

	resolvedOpts, err := resolveGoalOptions(opts)
	if err != nil {
		return nil, err
	}
	maxDurationSeconds := int64(resolvedOpts.MaxDuration / time.Second)

	now := gr.now()
	goal, err := gr.goalStore.CreateGoal(ctx, db.CreateGoalParams{
		ID:                 gr.newGoalID(),
		SessionID:          sessionID,
		Objective:          objective,
		Status:             GoalStatusRunning,
		MaxIterations:      resolvedOpts.MaxIterations,
		MaxDurationSeconds: maxDurationSeconds,
		StartedAt:          now.Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("create session goal: %w", err)
	}

	eventCh := make(chan AgentEvent)
	go gr.runGoalLoop(ctx, eventCh, goal, resolvedOpts, gr.evaluatorFor(resolvedOpts))

	return eventCh, nil
}

func (gr *GoalRunner) runGoalLoop(ctx context.Context, dst chan<- AgentEvent, goal db.SessionGoal, opts GoalOptions, evaluator GoalEvaluator) {
	defer close(dst)

	state := NewGoalState(goal.ID, goal.SessionID, goal.Objective, int(goal.MaxIterations), int(goal.MaxDurationSeconds))
	state.Iteration = int(goal.Iteration)
	state.StartedAt = time.Unix(goal.StartedAt, 0)
	state.CreatedAt = state.StartedAt
	initialTodos := llmtools.GetSessionTodos(goal.SessionID)
	state.InitialTodoSignature = todoSignature(initialTodos)
	state.LastTodoSignature = state.InitialTodoSignature

	promptText := BuildGoalInitialPrompt(goal.Objective, opts.InitialPrompt)
	for state.IsRunning() {
		if err := gr.applyLimitStatus(state, gr.checkLimits(state), ""); err != nil {
			dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
			return
		}
		if state.IsTerminal() {
			break
		}

		lastResponse, todos, err := gr.runIteration(ctx, dst, state, promptText)
		if err != nil {
			gr.handleIterationError(ctx, dst, goal, state, err)
			return
		}

		evaluation, err := evaluator.Evaluate(ctx, state, lastResponse, todos)
		if err != nil {
			dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
			return
		}

		state.RecordIteration(evaluation.Progress, evaluation.NextStep, evaluation.TodoSignature, evaluation.Signature, evaluation.ProgressMade)

		switch {
		case evaluation.Complete:
			state.NextStep = ""
			if err := state.Transition(GoalStatusCompleted, gr.now()); err != nil {
				dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
				return
			}
		case evaluation.Blocked:
			state.NextStep = ""
			state.BlockedReason = evaluation.Reason
			if err := state.Transition(GoalStatusBlocked, gr.now()); err != nil {
				dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
				return
			}
		case evaluation.Stalled:
			state.NextStep = ""
			state.BlockedReason = fallbackReason(evaluation.Reason, evaluation.Progress, "goal stalled")
			if err := state.Transition(GoalStatusStalled, gr.now()); err != nil {
				dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
				return
			}
		default:
			if err := gr.applyLimitStatus(state, gr.checkLimits(state), evaluation.Progress); err != nil {
				dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
				return
			}
			if !state.IsTerminal() {
				promptText = BuildGoalContinuationPrompt(goal.Objective, state.Iteration+1, state.MaxIterations, state.LastProgress, state.NextStep)
			}
		}

		if err := gr.finishGoal(ctx, goal, state); err != nil {
			dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
			return
		}
	}

	if state.IsTerminal() {
		if err := gr.finishGoal(ctx, goal, state); err != nil {
			dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
		}
	}
}

func (gr *GoalRunner) runIteration(ctx context.Context, dst chan<- AgentEvent, state *GoalState, promptText string) (string, []llmtools.TodoItem, error) {
	runCtx := ctx
	if state.MaxDurationSecs > 0 {
		remaining := state.StartedAt.Add(time.Duration(state.MaxDurationSecs) * time.Second).Sub(gr.now())
		if remaining <= 0 {
			return "", nil, context.DeadlineExceeded
		}
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, remaining)
		defer cancel()
	}

	src, err := gr.agent.Run(runCtx, state.SessionID, promptText)
	if err != nil {
		return "", nil, err
	}

	var finalResponse string
	for event := range src {
		if event.Type == AgentEventTypeResponse {
			finalResponse = extractGoalResponseText(event.Message)
		}
		dst <- event
	}

	if err := runCtx.Err(); err != nil {
		return "", nil, err
	}

	return strings.TrimSpace(finalResponse), llmtools.GetSessionTodos(state.SessionID), nil
}

func (gr *GoalRunner) handleIterationError(ctx context.Context, dst chan<- AgentEvent, goal db.SessionGoal, state *GoalState, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		state.LastProgress = "GOAL CANCELLED"
		_ = state.Transition(GoalStatusCancelled, gr.now())
	case errors.Is(err, context.DeadlineExceeded):
		state.LastProgress = fallbackReason(state.LastProgress, formatGoalTimeout(state))
		_ = state.Transition(GoalStatusTimeout, gr.now())
	default:
		state.BlockedReason = err.Error()
		_ = state.Transition(GoalStatusBlocked, gr.now())
	}
	_ = gr.finishGoal(ctx, goal, state)
	dst <- AgentEvent{Type: AgentEventTypeError, SessionID: goal.SessionID, Error: err, Done: true}
}

func (gr *GoalRunner) checkLimits(state *GoalState) error {
	switch {
	case gr.goalTimedOut(state):
		return ErrGoalMaxDurationExceeded
	case state.MaxIterations > 0 && state.Iteration >= state.MaxIterations:
		return ErrGoalMaxIterationsReached
	default:
		return nil
	}
}

func (gr *GoalRunner) goalTimedOut(state *GoalState) bool {
	if state.MaxDurationSecs <= 0 {
		return false
	}
	return !gr.now().Before(state.StartedAt.Add(time.Duration(state.MaxDurationSecs) * time.Second))
}

func (gr *GoalRunner) applyLimitStatus(state *GoalState, limitErr error, progress string) error {
	switch {
	case errors.Is(limitErr, ErrGoalMaxDurationExceeded):
		state.LastProgress = fallbackReason(formatGoalTimeout(state), progress, state.LastProgress)
		return state.Transition(GoalStatusTimeout, gr.now())
	case errors.Is(limitErr, ErrGoalMaxIterationsReached):
		state.BlockedReason = formatGoalMaxIterations(state.MaxIterations)
		state.LastProgress = fallbackReason(state.BlockedReason, progress, state.LastProgress)
		return state.Transition(GoalStatusFailed, gr.now())
	default:
		return limitErr
	}
}

func (gr *GoalRunner) evaluatorFor(opts GoalOptions) GoalEvaluator {
	if heuristic, ok := gr.evaluator.(*HeuristicGoalEvaluator); ok {
		threshold := heuristic.stallThreshold
		if opts.StallIterations > 0 {
			threshold = opts.StallIterations
		}
		return NewHeuristicGoalEvaluatorWithThreshold(threshold)
	}
	return gr.evaluator
}

func resolveGoalOptions(opts GoalOptions) (GoalOptions, error) {
	const (
		defaultGoalMaxIterations   = int64(20)
		defaultGoalMaxDuration     = time.Hour
		defaultGoalStallIterations = 3
	)

	if cfg := config.Get(); cfg != nil {
		if opts.MaxIterations <= 0 && cfg.Goal.MaxIterations > 0 {
			opts.MaxIterations = cfg.Goal.MaxIterations
		}
		if opts.MaxDuration <= 0 {
			raw := strings.TrimSpace(cfg.Goal.MaxDuration)
			if raw != "" {
				d, err := time.ParseDuration(raw)
				if err != nil {
					return GoalOptions{}, fmt.Errorf("parse goal max duration: %w", err)
				}
				opts.MaxDuration = d
			}
		}
		if opts.StallIterations <= 0 && cfg.Goal.StallIterations > 0 {
			opts.StallIterations = cfg.Goal.StallIterations
		}
	}

	if opts.MaxIterations <= 0 {
		opts.MaxIterations = defaultGoalMaxIterations
	}
	if opts.MaxDuration <= 0 {
		opts.MaxDuration = defaultGoalMaxDuration
	}
	if opts.StallIterations <= 0 {
		opts.StallIterations = defaultGoalStallIterations
	}

	return opts, nil
}

func formatGoalMaxIterations(maxIterations int) string {
	return fmt.Sprintf("GOAL STOPPED: reached max iterations (%d)", maxIterations)
}

func formatGoalTimeout(state *GoalState) string {
	if state == nil || state.MaxDurationSecs <= 0 {
		return "GOAL TIMEOUT"
	}
	return fmt.Sprintf("GOAL TIMEOUT: exceeded max duration (%s)", (time.Duration(state.MaxDurationSecs) * time.Second).String())
}

func (gr *GoalRunner) finishGoal(ctx context.Context, goal db.SessionGoal, state *GoalState) error {
	completedAt := sql.NullInt64{}
	if state.CompletedAt != nil {
		completedAt = sql.NullInt64{Int64: state.CompletedAt.Unix(), Valid: true}
	}

	lastProgress := sql.NullString{}
	if state.LastProgress != "" {
		lastProgress = sql.NullString{String: state.LastProgress, Valid: true}
	}

	nextStep := sql.NullString{}
	if state.NextStep != "" {
		nextStep = sql.NullString{String: state.NextStep, Valid: true}
	}

	blockedReason := sql.NullString{}
	if state.BlockedReason != "" {
		blockedReason = sql.NullString{String: state.BlockedReason, Valid: true}
	}

	_, err := gr.goalStore.UpdateGoalStatus(ctx, db.UpdateGoalStatusParams{
		Status:        state.Status(),
		Iteration:     int64(state.Iteration),
		LastProgress:  lastProgress,
		NextStep:      nextStep,
		BlockedReason: blockedReason,
		CompletedAt:   completedAt,
		ID:            goal.ID,
	})
	return err
}

func extractGoalResponseText(msg message.Message) string {
	var parts []string
	for _, part := range msg.Parts {
		text, ok := part.(message.TextContent)
		if !ok {
			continue
		}
		if strings.TrimSpace(text.Text) == "" {
			continue
		}
		parts = append(parts, text.Text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
