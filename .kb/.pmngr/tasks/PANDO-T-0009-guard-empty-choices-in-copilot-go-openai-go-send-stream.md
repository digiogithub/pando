---
id: PANDO-T-0009
type: task
title: Guard empty Choices in copilot.go/openai.go send+stream, retry/err, rename title recover label, tests
status: in_review
priority: high
parent: PANDO-US-0124
author: mcp
labels: [copilot, bug]
created: 2026-10-09T12:25:37Z
updated: 2026-10-09T12:27:41Z
started: 2026-10-09T12:27:41Z
---

## Description
copilot.go:468/473 (send), :613/:626/:627 (stream); openai.go:296/305 (send). agent.go:1280 RecoverPanic label -> "agent.generateTitle".
## Acceptance Criteria
No panic on `{"choices":[]}`; tests added; provider/agent tests pass.
