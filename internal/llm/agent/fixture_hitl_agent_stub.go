//go:build !agui_fixture_agent

package agent

import (
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/provider"
)

// maybeFixtureProvider is the no-op half of PANDO-T-0002's deterministic HITL
// fixture agent. The real implementation (internal/llm/agent/fixture_hitl_agent.go)
// only exists in a binary built with `-tags agui_fixture_agent`; a normal
// build (cmd/pando's default `go build ./...`, and every release build)
// never passes that tag, so this stub is what actually ships, and it always
// declines. createAgentProvider therefore behaves exactly as it did before
// this feature existed in any binary built without the tag — there is no
// code path, flag or environment variable that reaches a fixture provider
// here, because the fixture provider type is not compiled into the binary
// at all.
func maybeFixtureProvider(config.AgentName) (provider.Provider, bool) {
	return nil, false
}
