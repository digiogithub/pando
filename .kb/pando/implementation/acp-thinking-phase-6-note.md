# ACP thinking Phase 6 note

## Decision

ACP session thinking preferences now persist in the `sessions` table as a single JSON blob in `acp_session_state`.

Persisted fields:

1. selected model
2. `reasoning_effort`
3. `thinking_mode`
4. `thinking_stream_mode`

## Why

Phase 5 made ACP defaults stable inside the in-memory session state, but resume/load still rebuilt sessions without those ACP-specific choices. That meant a reconnect could keep the conversation history while silently falling back to the current global model or fresh thinking defaults.

Storing one ACP-specific state blob keeps the resume contract narrow and makes future extension straightforward without scattering more ACP columns through the main session record.

## Compatibility rule

- Pre-existing sessions remain backward compatible because `acp_session_state` is nullable.
- If a resumed session has no persisted ACP state, load/resume keeps the legacy behavior and does not backfill a new state blob automatically.
- If a persisted blob exists, load/resume restores it, runs the Phase 5 reconciliation path, and writes back the normalized result so stale or now-incompatible values do not keep resurfacing.
