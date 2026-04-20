package acp

import (
	"context"
	"fmt"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// PermissionQueue manages permission requests from ACP agents.
// It queues requests that need manual approval and allows resolution
// via API endpoints or automatic policies.
type PermissionQueue struct {
	mu      sync.RWMutex
	pending map[string]*PendingPermission
}

// PendingPermission represents a permission request awaiting resolution.
type PendingPermission struct {
	TaskID     string                           `json:"task_id"`
	SessionID  acpsdk.SessionId                 `json:"session_id"`
	RequestID  string                           `json:"request_id"`
	ToolCall   acpsdk.ToolCallUpdate            `json:"tool_call"`
	Options    []acpsdk.PermissionOption        `json:"options"`
	Outcome    *acpsdk.RequestPermissionOutcome `json:"outcome,omitempty"` // nil = pending, non-nil = resolved
	CreatedAt  time.Time                        `json:"created_at"`
	ResolvedAt *time.Time                       `json:"resolved_at,omitempty"`

	// done is closed when this permission is resolved, enabling zero-latency
	// notification without polling.
	done chan struct{}
}

// NewPermissionQueue creates a new permission queue.
func NewPermissionQueue() *PermissionQueue {
	return &PermissionQueue{
		pending: make(map[string]*PendingPermission),
	}
}

// QueuePermission adds a permission request to the queue for manual approval.
// Returns the request ID that can be used to resolve it later.
func (q *PermissionQueue) QueuePermission(taskID string, sessionID acpsdk.SessionId, req acpsdk.RequestPermissionRequest) string {
	q.mu.Lock()
	defer q.mu.Unlock()

	requestID := fmt.Sprintf("perm-%s-%d", taskID, time.Now().UnixNano())

	perm := &PendingPermission{
		TaskID:    taskID,
		SessionID: sessionID,
		RequestID: requestID,
		ToolCall:  req.ToolCall,
		Options:   req.Options,
		Outcome:   nil, // Pending
		CreatedAt: time.Now(),
		done:      make(chan struct{}),
	}

	q.pending[requestID] = perm

	return requestID
}

// ResolvePermission resolves a pending permission request.
// outcome contains the selected option or denial.
func (q *PermissionQueue) ResolvePermission(requestID string, outcome acpsdk.RequestPermissionOutcome) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	perm, exists := q.pending[requestID]
	if !exists {
		return fmt.Errorf("permission request not found: %s", requestID)
	}

	if perm.Outcome != nil {
		return fmt.Errorf("permission request already resolved: %s", requestID)
	}

	now := time.Now()
	perm.Outcome = &outcome
	perm.ResolvedAt = &now

	// Unblock any goroutine waiting in WaitForResolution with zero latency.
	close(perm.done)

	return nil
}

// WaitForResolution waits for a permission request to be resolved.
// Returns the outcome or an error if the context is cancelled.
// Uses a per-request channel (closed on resolution) instead of polling,
// so it wakes up instantly with zero CPU overhead while waiting.
func (q *PermissionQueue) WaitForResolution(ctx context.Context, requestID string) (acpsdk.RequestPermissionOutcome, error) {
	q.mu.RLock()
	perm, exists := q.pending[requestID]
	if !exists {
		q.mu.RUnlock()
		return acpsdk.RequestPermissionOutcome{}, fmt.Errorf("permission request not found: %s", requestID)
	}
	if perm.Outcome != nil {
		outcome := *perm.Outcome
		q.mu.RUnlock()
		return outcome, nil
	}
	// Capture the done channel before releasing the lock.
	done := perm.done
	q.mu.RUnlock()

	// Block until resolved or context cancelled — no polling, no CPU.
	select {
	case <-ctx.Done():
		return acpsdk.RequestPermissionOutcome{}, ctx.Err()
	case <-done:
		q.mu.RLock()
		perm, exists := q.pending[requestID]
		if !exists || perm.Outcome == nil {
			q.mu.RUnlock()
			// Entry was cleaned up between resolution and this read (extremely unlikely).
			return acpsdk.RequestPermissionOutcome{}, fmt.Errorf("permission request expired before result could be read: %s", requestID)
		}
		outcome := *perm.Outcome
		q.mu.RUnlock()
		return outcome, nil
	}
}

// GetPending returns all pending permission requests for a task.
func (q *PermissionQueue) GetPending(taskID string) []*PendingPermission {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var result []*PendingPermission
	for _, perm := range q.pending {
		if perm.TaskID == taskID && perm.Outcome == nil {
			result = append(result, perm)
		}
	}

	return result
}

// GetAllPending returns all pending permission requests across all tasks.
func (q *PermissionQueue) GetAllPending() []*PendingPermission {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var result []*PendingPermission
	for _, perm := range q.pending {
		if perm.Outcome == nil {
			result = append(result, perm)
		}
	}

	return result
}

// GetPermission returns a specific permission request by ID.
func (q *PermissionQueue) GetPermission(requestID string) (*PendingPermission, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	perm, exists := q.pending[requestID]
	return perm, exists
}

// CleanupResolved removes resolved permission requests older than the specified duration.
func (q *PermissionQueue) CleanupResolved(maxAge time.Duration) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for id, perm := range q.pending {
		if perm.Outcome != nil && perm.ResolvedAt != nil && perm.ResolvedAt.Before(cutoff) {
			delete(q.pending, id)
			removed++
		}
	}

	return removed
}
