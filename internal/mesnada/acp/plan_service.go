package acp

import (
	"context"
	"fmt"
	"log"
	"strings"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// planServiceImpl implements the PlanService interface for handling plan-related operations.
type planServiceImpl struct {
	logger *log.Logger
}

// NewPlanService creates a new plan service instance.
func NewPlanService(logger *log.Logger) *planServiceImpl {
	if logger == nil {
		logger = log.Default()
	}
	return &planServiceImpl{
		logger: logger,
	}
}

// DecodeTodos parses the output of a todowrite tool into TodoInfo structures.
// This matches the OpenCode implementation for decoding todo output.
func (s *planServiceImpl) DecodeTodos(output string) DecodeTodosResult {
	if strings.TrimSpace(output) == "" {
		return DecodeTodosResult{
			Success: false,
			Error:   "Empty output from todowrite tool",
		}
	}

	// Use parseTodoWritePlanStrict which already handles JSON parsing into acpsdk.PlanEntry
	entries := parseTodoWritePlan(output)
	if len(entries) > 0 {
		todos := make([]TodoInfo, 0, len(entries))
		for _, e := range entries {
			todos = append(todos, TodoInfo{
				Content:  e.Content,
				Status:   string(e.Status),
				Priority: string(e.Priority),
			})
		}
		return DecodeTodosResult{
			Success: true,
			Todos:   todos,
		}
	}

	return DecodeTodosResult{
		Success: false,
		Error:   "Failed to parse todowrite output. Expected JSON array/object or plain text list.",
	}
}

// CreatePlanEntry converts TodoInfo to PlanEntry with proper state mapping.
// This follows the OpenCode standard for mapping internal todo states to ACP plan entries.
func (s *planServiceImpl) CreatePlanEntry(todo TodoInfo) PlanEntry {
	// Map todo states to ACP plan states
	// In OpenCode: cancelled -> completed, other states remain the same
	status := todo.Status
	if status == "cancelled" {
		status = "completed"
	}

	// Default priority to medium if not specified
	priority := todo.Priority
	if priority == "" {
		priority = "medium"
	}

	return PlanEntry{
		Priority: priority,
		Status:   status,
		Content:  todo.Content,
	}
}

// CreateSessionPlan converts a list of TodoInfo to a SessionPlan.
func (s *planServiceImpl) CreateSessionPlan(todos []TodoInfo) *SessionPlan {
	entries := make([]PlanEntry, 0, len(todos))

	for _, todo := range todos {
		entries = append(entries, s.CreatePlanEntry(todo))
	}

	return &SessionPlan{
		Entries: entries,
	}
}

// SendPlanUpdate sends a plan update to the ACP client.
// This method should be called when a todowrite tool completes successfully.
func (s *planServiceImpl) SendPlanUpdate(ctx context.Context, conn *acpsdk.AgentSideConnection, sessionID acpsdk.SessionId, plan *SessionPlan) error {
	if conn == nil {
		return fmt.Errorf("no ACP connection available")
	}

	if plan == nil || len(plan.Entries) == 0 {
		return fmt.Errorf("empty plan")
	}

	entries := make([]acpsdk.PlanEntry, 0, len(plan.Entries))
	for _, e := range plan.Entries {
		entries = append(entries, acpsdk.NewPlanEntry(
			e.Content,
			acpsdk.ParsePlanEntryStatus(e.Status),
			acpsdk.ParsePlanEntryPriority(e.Priority),
		))
	}

	notification := acpsdk.SessionNotification{
		SessionId: sessionID,
		Update:    acpsdk.UpdatePlan(entries...),
	}

	s.logger.Printf("[PLAN SERVICE] Sending plan update with %d entries", len(plan.Entries))
	return conn.SessionUpdate(ctx, notification)
}

// ValidatePlanEntry validates a plan entry according to OpenCode standards.
func (s *planServiceImpl) ValidatePlanEntry(entry PlanEntry) error {
	if strings.TrimSpace(entry.Content) == "" {
		return fmt.Errorf("plan entry content cannot be empty")
	}

	validStatuses := map[string]bool{
		"pending":     true,
		"in_progress": true,
		"completed":   true,
	}

	if !validStatuses[entry.Status] {
		return fmt.Errorf("invalid plan entry status: %s", entry.Status)
	}

	validPriorities := map[string]bool{
		"high":   true,
		"medium": true,
		"low":    true,
	}

	if !validPriorities[entry.Priority] {
		return fmt.Errorf("invalid plan entry priority: %s", entry.Priority)
	}

	return nil
}

// ValidateSessionPlan validates a complete session plan.
func (s *planServiceImpl) ValidateSessionPlan(plan *SessionPlan) error {
	if plan == nil {
		return fmt.Errorf("plan cannot be nil")
	}

	if len(plan.Entries) == 0 {
		return fmt.Errorf("plan cannot be empty")
	}

	for i, entry := range plan.Entries {
		if err := s.ValidatePlanEntry(entry); err != nil {
			return fmt.Errorf("invalid plan entry at index %d: %w", i, err)
		}
	}

	return nil
}
