---
id: PANDO-US-0135
type: story
title: "agent: wire the session-history source (message + session services, SummaryMessageID)"
status: done
parent: PANDO-EP-0023
author: mcp
labels: [agent, edit]
created: 2026-11-03T12:02:14Z
updated: 2026-11-03T12:06:20Z
started: 2026-11-03T12:04:52Z
closed: 2026-11-03T12:06:20Z
---

## Description
Install the history source from the agent layer using the message service (`message.Service.List`) and the session service (`SummaryMessageID`), so the tools package stays decoupled. Covers every surface that builds tools (WebUI, TUI, ACP).

## Acceptance Criteria
- The source is installed wherever the agent is constructed.
- `go test ./internal/llm/tools/ ./internal/llm/agent ./internal/api` passes.
