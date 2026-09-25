---
id: PANDO-EP-0012
type: epic
title: "ACP compatibility with Xcode 27: message ids, reasoning summaries, per-session MCP"
status: done
priority: high
author: mcp
labels: [acp, xcode, mcp]
created: 2026-09-25T14:52:07Z
updated: 2026-09-25T15:05:41Z
started: 2026-09-25T14:53:28Z
closed: 2026-09-25T15:05:41Z
---

## Description
Xcode 27 native ACP client renders Pando conversations merged (answers concatenated), shows no thinking, and Pando ignores Xcode's `xcode-tools` MCP server passed in session/new. Root causes found in pando-acp.log (v1.0.3). Plan: KB `pando/plans/acp_xcode_compat.md`.

## Acceptance Criteria
- Live agent chunks carry a per-assistant-message `messageId` from the first delta; no live user echo with constant id.
- Copilot Responses API models stream reasoning summaries as `agent_thought_chunk`; reasoning effort honoured.
- Responses history keeps assistant text alongside function calls.
- MCP servers from session/new / session/load (stdio, http, sse) are connected per session and their tools usable only in that session.
- Tests added; `go build ./...` and targeted tests green.
