package project

import (
	"context"
	"os/exec"
	"sync"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// Instance represents a running (or stopped) child Pando ACP process
// for a registered project directory.
type Instance struct {
	Project  Project
	cmd      *exec.Cmd
	conn     *acpsdk.ClientSideConnection
	cancel   context.CancelFunc
	mu       sync.RWMutex
	sessions []sessionEntry // cached from last session/list call
	ready    chan struct{}  // closed after ACP handshake succeeds
	errCh    chan error     // receives process exit errors
}

// sessionEntry is a lightweight session descriptor fetched from the child.
type sessionEntry struct {
	ID        string
	Title     string
	UpdatedAt string
}
