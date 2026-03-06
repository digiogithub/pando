// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"context"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// Spawner defines the interface for spawning and managing CLI agent processes.
type Spawner interface {
	// Spawn starts a new agent process.
	Spawn(ctx context.Context, task *models.Task) error

	// Cancel stops a running agent.
	Cancel(taskID string) error

	// Pause stops a running agent without marking it as cancelled.
	Pause(taskID string) error

	// Wait blocks until a task completes or context is cancelled.
	Wait(ctx context.Context, taskID string) error

	// IsRunning checks if a task is currently running.
	IsRunning(taskID string) bool

	// RunningCount returns the number of currently running processes.
	RunningCount() int

	// Shutdown cancels all running processes.
	Shutdown()
}
