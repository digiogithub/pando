---
id: PANDO-US-0065
type: story
title: "P2+P3 Copilot Responses API: reasoning summaries and assistant text with tool calls"
status: done
parent: PANDO-EP-0012
author: mcp
labels: [provider, copilot]
created: 2026-09-25T14:52:19Z
updated: 2026-09-25T15:05:41Z
started: 2026-09-25T14:53:28Z
closed: 2026-09-25T15:05:41Z
---

## Description
internal/llm/provider/copilot.go `/responses` path never requests reasoning (effort/summary) nor maps reasoning summary events; history conversion drops assistant text when tool calls present.

## Acceptance Criteria
- Reasoning{Effort, Summary:auto} sent when model supports reasoning effort (send + stream).
- reasoning summary delta events → EventThinkingDelta.
- Assistant text + function_call both in input.
- httptest-based unit tests.
