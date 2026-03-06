# Pando Plan Influences: ACP Multi-Agent Plan Analysis

## Reference Document
- **[.ai/references/opencode-multi-agent-acp-plan.md]**: Comprehensive plan (in Spanish) for modernizing the OpenCode fork with updated AI providers, ACP (Agent Communication Protocol by IBM/Linux Foundation), and a multi-agent TUI.

## Plan Goals
1. Update the AI provider system (inspired by Crush's architecture)
2. Add ACP support for agent-to-agent communication
3. Build a multi-agent TUI for visualizing and managing multiple ACP agents simultaneously

## Ideas That Align Well with Pando

### Provider Abstraction Layer
- The plan proposes a clean `Provider` interface with `Chat()`, `Stream()`, `Models()`, `Capabilities()`, `ValidateConfig()`.
- Pando already has individual provider files [internal/llm/provider/*.go] but the plan's unified interface with `ModelCost`, `ModelCapabilities`, and `StreamReader` is more structured.
- **Alignment**: Direct improvement over current Pando code. Can be adopted incrementally.

### ACP Protocol Types
- The plan defines `Message`, `AgentManifest`, `Capability`, `TaskRequest`, `TaskResponse`, `TaskStatus`, `StreamChunk` types for inter-agent communication.
- **Alignment**: Pando's existing agent system (coder, task, summarizer, title) maps naturally to ACP agent manifests. Each agent could expose capabilities via ACP.

### ACP Server/Client Architecture
- REST-based ACP server with routes: `/acp/v1/discover`, `/acp/v1/agents`, `/acp/v1/agents/{agent}/task`, `/acp/v1/agents/{agent}/stream`.
- **Alignment**: Pando's `cmd/` could gain an `agent-server` subcommand. The existing MCP infrastructure (stdio/sse) provides a pattern for adding ACP transports.

### Multi-Agent TUI
- The plan envisions split-pane TUI showing multiple agents, their status, and inter-agent message flow.
- **Alignment**: Pando's BubbleTea TUI [internal/tui/] is well-structured for adding new page types. The pubsub system can route ACP events to new TUI panels.

## Areas Requiring Adaptation Due to Licensing

### Crush Reference Code
- The plan suggests basing provider updates on Crush's architecture. However, **Crush has changed its license** (no longer MIT).
- **Adaptation needed**: Cannot directly copy Crush code. Must implement equivalent features independently, using only the architectural patterns as inspiration.
- Specifically affected: provider abstraction, dynamic model switching, cost optimization, fallback mechanisms mentioned in the plan.

### Provider SDKs
- Plan references `github.com/anthropics/anthropic-sdk-go` directly. Pando currently uses its own provider implementations.
- **Adaptation needed**: Evaluate whether to adopt official SDKs (which are MIT/Apache) vs. maintain custom HTTP clients. SDK adoption reduces maintenance but adds dependencies.

### Gorilla Mux for ACP Server
- Plan uses `github.com/gorilla/mux` for HTTP routing.
- **Adaptation needed**: Gorilla Mux is archived. Consider using `net/http` (Go 1.22+ has pattern matching) or `chi` router instead.

## Potential Improvements from Crush

### Enhanced Session Management
- Crush has improved session handling. Pando's current session service [internal/session/] is basic CRUD + pubsub.
- **Improvement**: Add session branching, session export/import, cross-device sync.

### Cost Optimization Per Model
- Crush tracks per-model costs. Pando has no cost tracking.
- **Improvement**: Add `ModelCost` struct as proposed in plan, track token usage and cost per session.

### Fallback Mechanisms
- Crush implements provider fallbacks when primary is unavailable.
- **Improvement**: Add retry/fallback logic in agent run loop [internal/llm/agent/agent.go].

### MCP HTTP Transport
- Crush supports HTTP transport for MCP in addition to stdio/sse.
- **Improvement**: Extend MCP config type [internal/config/config.go] `MCPType` to include HTTP.

## Implementation Priority (from plan)
1. **Phase 1**: Provider system update (new interface, updated models, cost tracking)
2. **Phase 2**: ACP protocol implementation (types, server, client)
3. **Phase 3**: Multi-agent TUI (panels, agent cards, status bar)
4. **Phase 4**: Bridge between existing OpenCode agents and ACP agents

## Open Questions
- Should ACP server be a separate binary or integrated into the main `opencode` command?
- How to handle ACP agent discovery in local-only vs. network deployments?
- What is the minimum viable ACP implementation to test with existing agent architecture?
- Should the provider interface change be a breaking change or maintain backwards compatibility?
- How to handle Crush feature parity without license contamination — clean-room implementation strategy needed.
