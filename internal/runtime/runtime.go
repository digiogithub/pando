// Package runtime defines abstractions for executing commands and accessing
// the filesystem in different isolation environments (host, docker, podman, etc.).
package runtime

import (
	"context"
	"io/fs"

	"github.com/digiogithub/pando/internal/config"
)

// RuntimeType identifies the execution backend.
type RuntimeType string

const (
	RuntimeHost     RuntimeType = "host"
	RuntimeDocker   RuntimeType = "docker"
	RuntimePodman   RuntimeType = "podman"
	RuntimeEmbedded RuntimeType = "embedded"
)

// ExecResult holds the output of a single command execution.
type ExecResult struct {
	Stdout      string
	Stderr      string
	ExitCode    int
	Interrupted bool
}

// ExecutionRuntime handles command execution within a session.
type ExecutionRuntime interface {
	// Exec runs cmd inside the session identified by sessionID.
	Exec(ctx context.Context, sessionID string, cmd string, env []string) (ExecResult, error)
	// StartSession initialises a new persistent session rooted at workDir.
	StartSession(ctx context.Context, sessionID string, workDir string) error
	// StopSession tears down the session and releases its resources.
	StopSession(ctx context.Context, sessionID string) error
	// Output returns any buffered output for the session.
	Output(ctx context.Context, sessionID string) (string, error)
	// Kill forcibly terminates the running command inside the session.
	Kill(ctx context.Context, sessionID string) error
	// Type returns the RuntimeType for this implementation.
	Type() RuntimeType
}

// WorkspaceFS provides filesystem operations over the session workspace.
type WorkspaceFS interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte, perm fs.FileMode) error
	Stat(ctx context.Context, path string) (fs.FileInfo, error)
	MkdirAll(ctx context.Context, path string, perm fs.FileMode) error
	Remove(ctx context.Context, path string) error
	List(ctx context.Context, path string) ([]fs.DirEntry, error)
	Mounted() bool
}

// RuntimeCapabilities describes a discovered runtime and its capabilities.
type RuntimeCapabilities struct {
	Type      RuntimeType `json:"type"`
	Available bool        `json:"available"`
	Exec      bool        `json:"exec"`
	FS        bool        `json:"fs"`
	Version   string      `json:"version,omitempty"`
	Socket    string      `json:"socket,omitempty"` // socket path for docker/podman
}

// RuntimeResolver selects the appropriate runtime based on configuration.
type RuntimeResolver interface {
	// Resolve returns the ExecutionRuntime and WorkspaceFS for the given config.
	Resolve(cfg config.ContainerConfig) (ExecutionRuntime, WorkspaceFS, error)
	// Discover probes the host and returns capabilities for each known runtime.
	Discover() []RuntimeCapabilities
}
