package acp

import (
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// ACPAgent is the interface that ACP agents must implement.
// This allows the HTTP transport to work with different agent implementations.
// AgentExperimental is intentionally not embedded — the SDK dispatches its
// methods via type assertions, so agents only implement what they support.
type ACPAgent interface {
	acpsdk.Agent       // Embeds the core SDK Agent interface
	acpsdk.AgentLoader // Embeds the optional LoadSession capability

	// GetVersion returns the agent version
	GetVersion() string

	// GetCapabilities returns the agent capabilities
	GetCapabilities() acpsdk.AgentCapabilities
}

// Ensure SimpleACPAgent implements the interface
var _ ACPAgent = (*SimpleACPAgent)(nil)

// Ensure PandoACPAgent implements ACPAgent interface
var _ ACPAgent = (*PandoACPAgent)(nil)
