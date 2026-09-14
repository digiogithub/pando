//go:build agui_fixture_agent

package config

// AgentFixtureHITL is PANDO-T-0002's deterministic, test-only agent name.
//
// It exists so the AG-UI HITL round trip (permission requests and
// AskUserQuestion, internal/agui/hitl.go) can be driven end to end against a
// real `pando agui-serve` process without an LLM call — see
// internal/llm/agent/fixture_hitl_agent.go for the provider that actually
// answers as this agent.
//
// Gate: this identifier is only appended to KnownAgentNames (below) when the
// binary is built with `-tags agui_fixture_agent`. A normal `go build`
// (cmd/pando's default, and every release build) never passes that tag, so
// in a production binary "fixture-hitl" is not a known agent at all:
// agui.ConfigFromApp filters any unrecognized AGUI.Agents/--agent entry
// through config.IsKnownAgent and silently drops it (deps.go), exactly like
// a typo'd agent name. There is no config flag, env var or API call that can
// make a production binary recognize this name — the code implementing it
// is not even compiled in. The second gate (PANDO_AGUI_FIXTURE_AGENT=1) lives
// in internal/llm/agent/fixture_hitl_agent.go, for a tagged build that still
// should not activate it by accident.
const AgentFixtureHITL AgentName = "fixture-hitl"

func init() {
	KnownAgentNames = append(KnownAgentNames, AgentFixtureHITL)
}
