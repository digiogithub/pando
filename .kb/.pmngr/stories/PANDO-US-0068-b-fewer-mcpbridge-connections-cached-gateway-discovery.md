---
id: PANDO-US-0068
type: story
title: "B: Fewer mcpbridge connections: cached gateway discovery, session dedupe, persistent direct client"
status: done
parent: PANDO-EP-0013
author: mcp
labels: [mcp, gateway]
created: 2026-09-25T15:26:27Z
updated: 2026-09-25T15:33:03Z
started: 2026-09-25T15:26:27Z
closed: 2026-09-25T15:33:03Z
---

## Acceptance Criteria
- Gateway startup skips discovery for servers with cached catalog + unchanged config fingerprint; RefreshServer always rediscovers.
- Session with client-provided Xcode bridge hides the global mcpbridge server's tools (direct tools; gateway if feasible).
- Direct mcpTool reuses a persistent client for mcpbridge servers instead of spawning per call.
- Tests.
