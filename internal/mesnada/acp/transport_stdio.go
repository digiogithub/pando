package acp

import (
	"context"
	"log"
	"os"

	acpsdk "github.com/coder/acp-go-sdk"
)

// StdioTransport wraps the SDK's AgentSideConnection for stdio transport.
type StdioTransport struct {
	agent  *PandoACPAgent
	logger *log.Logger
	conn   *acpsdk.AgentSideConnection
}

// NewStdioTransport creates a new stdio transport for the ACP agent.
// It uses the SDK's AgentSideConnection to handle JSON-RPC communication.
func NewStdioTransport(agent *PandoACPAgent, logger *log.Logger) *StdioTransport {
	if logger == nil {
		logger = log.Default()
	}

	// Create the agent-side connection
	// peerInput is where we write to (stdout), peerOutput is where we read from (stdin)
	conn := acpsdk.NewAgentSideConnection(agent, os.Stdout, os.Stdin)

	// Give the agent a reference to the connection so it can stream updates
	// back to the client while processing prompts.
	agent.SetConnection(conn)

	return &StdioTransport{
		agent:  agent,
		logger: logger,
		conn:   conn,
	}
}

// Run starts the stdio transport loop.
// The AgentSideConnection automatically starts handling messages when created.
// We just need to wait for the connection to be done.
func (t *StdioTransport) Run(ctx context.Context) error {
	t.logger.Printf("[ACP TRANSPORT] Starting stdio transport with SDK connection")

	// Wait for either context cancellation or connection to be done
	select {
	case <-ctx.Done():
		t.logger.Printf("[ACP TRANSPORT] Context cancelled")
		return ctx.Err()
	case <-t.conn.Done():
		t.logger.Printf("[ACP TRANSPORT] Connection closed")
		return nil
	}
}
