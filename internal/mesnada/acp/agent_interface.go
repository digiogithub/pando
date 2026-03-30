package acp

import (
	acpsdk "github.com/coder/acp-go-sdk"
)

// ACPAgent is the interface that ACP agents must implement.
// This allows the HTTP transport to work with different agent implementations.
type ACPAgent interface {
	acpsdk.Agent            // Embeds the core SDK Agent interface
	acpsdk.AgentLoader      // Embeds the optional LoadSession capability
	acpsdk.AgentExperimental // Embeds the experimental SetSessionModel capability

	// GetVersion returns the agent version
	GetVersion() string

	// GetCapabilities returns the agent capabilities
	GetCapabilities() acpsdk.AgentCapabilities
}

// Ensure SimpleACPAgent implements the interface
var _ ACPAgent = (*SimpleACPAgent)(nil)

// Ensure PandoACPAgent implements ACPAgent interface
var _ ACPAgent = (*PandoACPAgent)(nil)
