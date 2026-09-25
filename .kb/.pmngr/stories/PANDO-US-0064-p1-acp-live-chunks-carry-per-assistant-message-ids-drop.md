---
id: PANDO-US-0064
type: story
title: P1 ACP live chunks carry per-assistant-message ids; drop live user echo
status: done
parent: PANDO-EP-0012
author: mcp
labels: [acp]
created: 2026-09-25T14:52:19Z
updated: 2026-09-25T15:05:41Z
started: 2026-09-25T14:53:28Z
closed: 2026-09-25T15:05:41Z
---

## Description
internal/mesnada/acp/prompt_handler.go: `currentMessageID` only set on AgentEventTypeResponse so deltas have no id; user echo uses constant `<sessionID>-user`.

## Acceptance Criteria
- No `user_message_chunk` during live session/prompt.
- Every live agent_message_chunk/agent_thought_chunk has messageId == persisted assistant message.ID, from the first delta; new id after a tool round-trip.
- Unit tests.
