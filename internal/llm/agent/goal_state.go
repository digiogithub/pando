package agent

import (
	"fmt"
	"time"
)

// Goal status constants
const (
	GoalStatusRunning   = "running"
	GoalStatusCompleted = "completed"
	GoalStatusFailed    = "failed"
	GoalStatusCancelled = "cancelled"
	GoalStatusBlocked   = "blocked"
	GoalStatusTimeout   = "timeout"
	GoalStatusStalled   = "stalled"
)

// GoalState represents the current state of a goal.
type GoalState struct {
	ID                      string
	SessionID               string
	Objective               string
	StatusValue             string
	Iteration               int
	MaxIterations           int
	MaxDurationSecs         int
	StartedAt               time.Time
	CompletedAt             *time.Time
	LastProgress            string
	NextStep                string
	BlockedReason           string
	CreatedAt               time.Time
	InitialTodoSignature    string
	LastTodoSignature       string
	LastEvaluationSignature string
	ConsecutiveNoProgress   int

	// Internal: stop channel for cancellation
	stopChan chan struct{}

	// Internal: done channel when loop exits
	doneChan chan struct{}
}

func NewGoalState(id, sessionID, objective string, maxIter, maxDurSecs int) *GoalState {
	now := time.Now()
	return &GoalState{
		ID:              id,
		SessionID:       sessionID,
		Objective:       objective,
		StatusValue:     GoalStatusRunning,
		MaxIterations:   maxIter,
		MaxDurationSecs: maxDurSecs,
		StartedAt:       now,
		CreatedAt:       now,
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
	}
}

// Cancel signals the goal to stop.
func (g *GoalState) Cancel() {
	select {
	case <-g.stopChan:
		// Already closed
	default:
		close(g.stopChan)
	}
}

// IsCancelled returns true if the goal has been cancelled.
func (g *GoalState) IsCancelled() bool {
	select {
	case <-g.stopChan:
		return true
	default:
		return false
	}
}

// Elapsed returns the time elapsed since the goal started.
func (g *GoalState) Elapsed() time.Duration {
	return time.Since(g.StartedAt)
}

// RemainingDuration returns the remaining time before max duration.
func (g *GoalState) RemainingDuration() time.Duration {
	d := time.Duration(g.MaxDurationSecs) * time.Second
	remaining := d - g.Elapsed()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Status returns the current status for serialization.
func (g *GoalState) Status() string { return g.StatusValue }

// SetStatus updates the status and marks done if terminal.
func (g *GoalState) SetStatus(s string) {
	g.StatusValue = s
	if g.IsTerminal() {
		now := time.Now()
		g.CompletedAt = &now
		select {
		case <-g.doneChan:
		default:
			close(g.doneChan)
		}
	}
}

// IsTerminal returns true if the status is a terminal state.
func (g *GoalState) IsTerminal() bool {
	switch g.StatusValue {
	case GoalStatusCompleted, GoalStatusFailed, GoalStatusCancelled, GoalStatusBlocked, GoalStatusTimeout, GoalStatusStalled:
		return true
	}
	return false
}

// IsRunning returns true if the goal is currently active.
func (g *GoalState) IsRunning() bool { return g.StatusValue == GoalStatusRunning }

func (g *GoalState) CanTransitionTo(next string) bool {
	if g.StatusValue == next {
		return true
	}

	switch g.StatusValue {
	case GoalStatusRunning:
		return next == GoalStatusCompleted ||
			next == GoalStatusBlocked ||
			next == GoalStatusFailed ||
			next == GoalStatusCancelled ||
			next == GoalStatusTimeout ||
			next == GoalStatusStalled
	default:
		return false
	}
}

func (g *GoalState) Transition(next string, now time.Time) error {
	if !g.CanTransitionTo(next) {
		return fmt.Errorf("invalid goal status transition: %s -> %s", g.StatusValue, next)
	}

	g.StatusValue = next
	if g.IsTerminal() {
		completedAt := now
		g.CompletedAt = &completedAt
		select {
		case <-g.doneChan:
		default:
			close(g.doneChan)
		}
	}

	return nil
}

func (g *GoalState) Advance(progress string, nextStep string) {
	g.Iteration++
	g.LastProgress = progress
	g.NextStep = nextStep
}

func (g *GoalState) RecordIteration(progress string, nextStep string, todoSignature string, evaluationSignature string, progressMade bool) {
	g.Advance(progress, nextStep)
	g.LastTodoSignature = todoSignature
	g.LastEvaluationSignature = evaluationSignature
	if progressMade {
		g.ConsecutiveNoProgress = 0
		return
	}
	g.ConsecutiveNoProgress++
}

// GoalEventType represents the type of goal event.
type GoalEventType string

const (
	GoalEventStart     GoalEventType = "goal_start"
	GoalEventIteration GoalEventType = "goal_iteration"
	GoalEventComplete  GoalEventType = "goal_complete"
	GoalEventFailed    GoalEventType = "goal_failed"
	GoalEventCancelled GoalEventType = "goal_cancelled"
	GoalEventBlocked   GoalEventType = "goal_blocked"
	GoalEventTimeout   GoalEventType = "goal_timeout"
	GoalEventStalled   GoalEventType = "goal_stalled"
)

// GoalEvent represents a state change event for a goal.
type GoalEvent struct {
	Type      GoalEventType
	GoalState *GoalState
	Message   string
}
