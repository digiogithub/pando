# ACP phase 3 Claude compatibility implementation

Date: 2026-05-28
Project: pando

Implemented phase 3 ACP compatibility changes in Pando for provider-state and retry/rate-limit visibility in ACP clients.

## Changes

### 1. Extended ACP system-message normalization
Updated `normalizeSystemMessage(...)` in `internal/mesnada/acp/agent.go` to recognize more provider-facing operational messages, including:
- rate-limit retry messages
- authentication-required / oauth-required messages
- quota / too-many-requests messages
- fallback-model retry messages

These messages now have a stable ACP-visible rendering path instead of being exposed only as opaque metadata.

### 2. Surface LLM-provider notifications as visible ACP chat messages
`StartNotificationBroadcast(...)` previously sent provider notifications only through `session_info_update` metadata (`pando:notification`).

It now also sends an `agent_message_chunk` for LLM-provider notifications when the message is relevant and not suppressed by normalization. This gives ACP clients a Claude-like visible status stream while preserving the metadata broadcast.

## Resulting behaviour
ACP clients can now see more of the operational state that previously existed only in logs/notifications, including retry/rate-limit/auth-style messages, while still receiving the structured notification payload in `_meta`.

## Verification
Ran:
- `go test ./internal/mesnada/acp ./internal/llm/agent ./internal/api`

All passed.

## Notes
There is an existing untracked `sdk/` directory in the repo working tree unrelated to this work.