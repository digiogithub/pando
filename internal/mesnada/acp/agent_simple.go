package acp

import (
	"context"
	"log"

	acpsdk "github.com/coder/acp-go-sdk"
)

// SimpleACPAgent is a minimal ACP agent for testing HTTP transport.
// This is used when the full PandoACPAgent is not available due to import cycles.
type SimpleACPAgent struct {
	version      string
	capabilities acpsdk.AgentCapabilities
	logger       *log.Logger
}

// NewSimpleACPAgent creates a simple ACP agent for testing.
func NewSimpleACPAgent(version string, logger *log.Logger) *SimpleACPAgent {
	if logger == nil {
		logger = log.Default()
	}

	return &SimpleACPAgent{
		version: version,
		logger:  logger,
		capabilities: acpsdk.AgentCapabilities{
			LoadSession: false,
			McpCapabilities: acpsdk.McpCapabilities{
				Http: true,
				Sse:  true,
			},
			PromptCapabilities: acpsdk.PromptCapabilities{
				Audio:           false,
				EmbeddedContext: false,
				Image:           false,
			},
		},
	}
}

// Initialize handles the initialization handshake.
func (a *SimpleACPAgent) Initialize(ctx context.Context, req acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	a.logger.Printf("[SIMPLE ACP] Initialize: %+v", req.ClientInfo)

	return acpsdk.InitializeResponse{
		ProtocolVersion:   req.ProtocolVersion,
		AgentInfo:         &acpsdk.Implementation{Name: "pando", Version: a.version},
		AgentCapabilities: a.capabilities,
		AuthMethods:       []acpsdk.AuthMethod{},
	}, nil
}

// Authenticate is not implemented.
func (a *SimpleACPAgent) Authenticate(ctx context.Context, req acpsdk.AuthenticateRequest) (acpsdk.AuthenticateResponse, error) {
	return acpsdk.AuthenticateResponse{}, nil
}

// Cancel handles cancellation.
func (a *SimpleACPAgent) Cancel(ctx context.Context, params acpsdk.CancelNotification) error {
	return nil
}

// NewSession is not implemented.
func (a *SimpleACPAgent) NewSession(ctx context.Context, req acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	return acpsdk.NewSessionResponse{}, nil
}

// Prompt is not implemented.
func (a *SimpleACPAgent) Prompt(ctx context.Context, req acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	return acpsdk.PromptResponse{}, nil
}

// SetSessionMode is not implemented.
func (a *SimpleACPAgent) SetSessionMode(ctx context.Context, req acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	return acpsdk.SetSessionModeResponse{}, nil
}

// ListSessions implements Agent — returns an empty list.
func (a *SimpleACPAgent) ListSessions(_ context.Context, _ acpsdk.ListSessionsRequest) (acpsdk.ListSessionsResponse, error) {
	return acpsdk.ListSessionsResponse{Sessions: []acpsdk.SessionInfo{}}, nil
}

// SetSessionConfigOption implements Agent (stub).
func (a *SimpleACPAgent) SetSessionConfigOption(_ context.Context, _ acpsdk.SetSessionConfigOptionRequest) (acpsdk.SetSessionConfigOptionResponse, error) {
	return acpsdk.SetSessionConfigOptionResponse{}, nil
}

// LoadSession implements AgentLoader (stub).
func (a *SimpleACPAgent) LoadSession(ctx context.Context, req acpsdk.LoadSessionRequest) (acpsdk.LoadSessionResponse, error) {
	return acpsdk.LoadSessionResponse{}, nil
}

// UnstableSetSessionModel implements AgentExperimental (stub).
func (a *SimpleACPAgent) UnstableSetSessionModel(ctx context.Context, req acpsdk.UnstableSetSessionModelRequest) (acpsdk.UnstableSetSessionModelResponse, error) {
	return acpsdk.UnstableSetSessionModelResponse{}, nil
}

// GetVersion returns the agent version.
func (a *SimpleACPAgent) GetVersion() string {
	return a.version
}

// GetCapabilities returns the agent capabilities.
func (a *SimpleACPAgent) GetCapabilities() acpsdk.AgentCapabilities {
	return a.capabilities
}
