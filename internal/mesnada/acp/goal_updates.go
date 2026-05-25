package acp

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/digiogithub/pando/internal/db"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

const goalMetaKey = "pando:goal"

func goalStateFromDB(goal db.SessionGoal, now time.Time) *GoalStateUpdate {
	elapsedEnd := now
	if goal.CompletedAt.Valid {
		elapsedEnd = time.Unix(goal.CompletedAt.Int64, 0)
	}
	elapsed := elapsedEnd.Sub(time.Unix(goal.StartedAt, 0)).Truncate(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}

	update := &GoalStateUpdate{
		Objective:     goal.Objective,
		Status:        goal.Status,
		Iteration:     int(goal.Iteration),
		MaxIterations: int(goal.MaxIterations),
		ElapsedTime:   elapsed.String(),
	}
	if goal.LastProgress.Valid {
		update.Progress = goal.LastProgress.String
	}
	if goal.NextStep.Valid {
		update.NextStep = goal.NextStep.String
	}
	if update.Progress == "" && goal.BlockedReason.Valid {
		update.Progress = goal.BlockedReason.String
	}
	return update
}

func goalMeta(goal *GoalStateUpdate) map[string]any {
	if goal == nil {
		return nil
	}
	return map[string]any{goalMetaKey: goal}
}

func mergeMetaMaps(metaMaps ...map[string]any) map[string]any {
	var merged map[string]any
	for _, meta := range metaMaps {
		if len(meta) == 0 {
			continue
		}
		if merged == nil {
			merged = make(map[string]any, len(meta))
		}
		for key, value := range meta {
			merged[key] = value
		}
	}
	return merged
}

func isGoalNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (a *PandoACPAgent) syncGoalState(ctx context.Context, sessionID acpsdk.SessionId, activeOnly bool) (*GoalStateUpdate, error) {
	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	update, err := a.loadGoalState(ctx, acpSession.PandoSessionID(), activeOnly)
	if err != nil {
		if isGoalNotFound(err) {
			acpSession.SetGoal(nil)
			return nil, nil
		}
		return nil, err
	}
	acpSession.SetGoal(update)
	return update, nil
}

func (a *PandoACPAgent) loadGoalState(ctx context.Context, sessionID string, activeOnly bool) (*GoalStateUpdate, error) {
	var (
		goal db.SessionGoal
		err  error
	)
	if activeOnly {
		goal, err = a.sessionService.GetActiveGoal(ctx, sessionID)
	} else {
		goal, err = a.sessionService.GetGoalBySession(ctx, sessionID)
	}
	if err != nil {
		return nil, err
	}
	return goalStateFromDB(goal, time.Now()), nil
}

func (a *PandoACPAgent) sendGoalStateUpdate(sessionID acpsdk.SessionId, goal *GoalStateUpdate) {
	if a.conn == nil {
		if acpSession, err := a.getSession(sessionID); err == nil {
			acpSession.SetGoal(goal)
		}
		return
	}

	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return
	}
	acpSession.SetGoal(goal)

	update := acpsdk.UpdateSessionInfo(acpsdk.SessionInfoUpdate{
		Meta: map[string]any{goalMetaKey: goal},
	})
	if err := a.safeSessionUpdate(acpSession, sessionID, update); err != nil {
		a.logger.Printf("[ACP AGENT] Failed to send goal update for session %s: %v", sessionID, err)
	}
}
