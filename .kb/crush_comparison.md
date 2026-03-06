# Crush vs Pando: Comparison & Influence Analysis

## Licensing
- **Crush license**: FSL-1.1-MIT (Functional Source License). NOT MIT today. Prohibits competing commercial use. Converts to MIT automatically 2 years after each release date. Original code by Kujtim Hoxha was MIT (2025-03-21 to 2025-05-30), then Charmbracelet relicensed to FSL. See `LICENSE.md`.
- **Pando license**: MIT. Any ideas borrowed from Crush must be **clean-room reimplemented** — no code copying. Study architecture patterns and APIs, implement independently.
- **Key constraint**: Cannot copy Crush source code into Pando. Can study patterns, interfaces, and architectural decisions, then reimplement from scratch under MIT.

## Multi-Agent / ACP Architecture

### What Crush Does Well
- **Coordinator pattern** (`coordinator.go`): Clean separation between coordinator (orchestration) and session agents (execution). Coordinator builds providers, manages models, delegates to agents. This is a good pattern for multi-agent even though Crush currently only uses one agent ("coder").
- **Sub-agent tool** (`agent_tool.go`): The `agent` tool creates child sessions with their own context, runs a "task" agent, and propagates costs back to the parent session. Uses `fantasy.NewParallelAgentTool` for parallel sub-agent execution. This is a clean sub-agent delegation pattern.
- **Agent config separation** (`config.go`): `AgentCoder` and `AgentTask` are defined as separate agent types with independent `AllowedTools` and `AllowedMCP` restrictions. This per-agent tool/MCP filtering is a good security and capability isolation pattern.
- **Fantasy abstraction** (`charm.land/fantasy`): The agent/LLM layer is fully abstracted — `fantasy.Agent`, `fantasy.LanguageModel`, `fantasy.AgentTool`. This makes swapping providers trivial and enables clean multi-agent composition.

### What Crush Plans But Hasn't Done Yet
- Comments in code: `// INFO: (kujtim) this is not used yet we will use this when we have multiple agents` on `SetMainAgent`. The `coordinator` still only has `currentAgent` — no dynamic agent switching or routing.
- `// TODO: when we support multiple agents we need to change this` on `buildAgentModels`.
- No agent-to-agent communication protocol. Sub-agents return text results, no structured handoff.

### Ideas for Pando (MIT-Compatible Reimplementation)
1. **Coordinator + SessionAgent pattern**: Create a coordinator that manages multiple named agents, each with their own tool sets, MCP access, and model configurations. This is an architectural pattern, not copyrightable code.
2. **Sub-agent with session isolation**: Spawn child sessions for sub-agent work. Track costs hierarchically. Return structured results, not just text.
3. **Per-agent tool/MCP filtering**: Allow config to specify which tools and MCP servers each agent can access. Good for security and capability isolation.
4. **Provider abstraction layer**: Build a clean provider interface that supports all major LLM APIs. Crush uses Fantasy; Pando should use its own or an MIT-licensed equivalent.
5. **Auto-summarization on context limit**: Monitor token usage and auto-summarize when approaching context window limits. Resume with summarized context. See `agent.go` StopWhen conditions.
6. **Loop detection**: Detect repeated tool calls to prevent infinite loops. See `loop_detection.go`.

## MCP Integration Comparison
- **Crush**: Full MCP client with stdio/SSE/HTTP. Dynamic tool discovery prefixed as `mcp_{server}_{tool}`. MCP instructions injected into system prompt. Per-agent MCP restrictions. Uses official Go MCP SDK.
- **Pando influence**: Adopt similar dynamic MCP tool registration pattern. Ensure MCP server instructions are surfaced in system prompts. Add per-agent MCP access control.

## Provider Support Comparison
- **Crush**: 12+ providers via Fantasy + Catwalk. Provider options merging from 3 layers. OAuth2 with auto-refresh. Custom headers/body per provider.
- **Pando influence**: Implement similar multi-layer config merging for provider options. Add OAuth2 refresh-on-401 pattern.

## TUI Comparison
- **Crush**: Bubble Tea v2 + Lipgloss v2. Rich chat rendering, diff views, completions, sidebar, animations, image support.
- **Pando influence**: Both use Bubble Tea. Can study Crush's component decomposition (15+ sub-packages in `ui/`) for better modularity.

## Skills / Agent Standards
- **Crush**: Implements agentskills.io SKILL.md standard. This is an open standard and can be freely adopted.
- **Pando influence**: Consider implementing SKILL.md support for interoperability.

## Key Differences
| Aspect | Crush | Pando |
|--------|-------|-------|
| License | FSL-1.1-MIT (not MIT today) | MIT |
| LLM abstraction | Fantasy (Charm proprietary) | Custom/needs own |
| Model catalog | Catwalk (Charm service) | Custom |
| Multi-agent | Planned, partial (sub-agent only) | Planned per ACP plan |
| MCP SDK | Official Go SDK | Needs equivalent |
| Agent Skills | agentskills.io | Not yet |
| DB | SQLite + sqlc | SQLite |
