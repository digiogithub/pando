---
id: PANDO-US-0124
type: story
title: Copilot/OpenAI providers handle empty choices without panicking
status: backlog
priority: high
parent: PANDO-EP-0021
author: mcp
labels: [copilot, bug, provider]
created: 2026-10-09T12:25:14Z
updated: 2026-10-09T12:25:14Z
---

## Description
Guard every `Choices[0]` access in copilot.go (send :468/:473, stream :613/:626/:627) and openai.go (send :296/:305). Empty choices in non-streaming send are retried within retryLimit, then returned as an error. Title-generation recover label renamed from "agent.Run" to "agent.generateTitle".

## Acceptance Criteria
- Unit tests: httptest server returning `{"choices":[]}` for send and stream; no panic.
- `go test ./internal/llm/provider ./internal/llm/agent` passes.
