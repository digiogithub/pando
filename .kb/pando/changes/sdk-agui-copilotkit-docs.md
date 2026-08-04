---
created_at: 2026-08-04T07:32:12.106583286Z
updated_at: 2026-08-04T07:32:12.106583286Z
---

# SDK Documentation Update: AG-UI + CopilotKit (2026-08-04)

## Date
2026-08-04

## What Was Done

Updated TypeScript SDK documentation (EN/ES) with comprehensive AG-UI and CopilotKit integration sections. Updated SDK index pages to reflect the four interaction modes.

## Files Modified
- `content/en/sdk/typescript.md` — Added AG-UI mode section (PandoAguiClient, CopilotKit integration, shared state, frontend tools, human-in-the-loop, architecture)
- `content/es/sdk/typescript.md` — Same changes in Spanish
- `content/en/sdk/_index.md` — Updated to four interaction modes (added AG-UI)
- `content/es/sdk/_index.md` — Same update in Spanish

## Documentation Coverage

### AG-UI Mode (4. AG-UI Mode)
- **Direct Client (PandoAguiClient)**: Dependency-free client for AG-UI endpoint, streaming events, runText convenience
- **CopilotKit Integration (registerPandoCopilotKit)**: One-liner Next.js route setup
- **Agent Discovery**: createPandoAgent, discoverPandoAgents
- **Shared State Dashboard**: PandoState type, useCoAgent hook, model/todos/files/subAgents
- **Frontend Tools**: useCopilotAction for browser-side tools with suspend/resume
- **Human-in-the-Loop**: pando_permission_request tool calls with approval dialogs
- **Architecture**: Browser → Next.js → AG-UI/SSE → pando agui-serve
- **Starting the Adapter**: pando agui-serve CLI, .pando.toml config
- **Exports Table**: All AG-UI exports with descriptions

## Key Examples Documented
1. Direct AG-UI client usage (PandoAguiClient)
2. CopilotKit route setup (registerPandoCopilotKit)
3. Agent discovery (createPandoAgent, discoverPandoAgents)
4. Shared state dashboard (useCoAgent<PandoState>)
5. Frontend tools (highlight_in_page example)
6. Permission prompts (pando_permission_request)
7. Full architecture diagram

## Verification
- Hugo build successful: 122 EN pages, 120 ES pages
- No build errors
- All new sections follow existing style and format