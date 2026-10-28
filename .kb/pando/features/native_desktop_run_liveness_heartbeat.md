---
created_at: 2026-10-28T21:44:06.754114053Z
updated_at: 2026-10-28T21:44:06.754114053Z
---
# Native desktop: run liveness (stream heartbeat, composer trail, stall detection) + debug ID copy

Date: 2026-10-28. Uncommitted at writing time (on top of jj commit 0abe2da5 "settings follow-ups and sessions in the sidebar"). Related: [[native_desktop_settings_followups]], [[native_desktop_chat]].

## What changed
- **Server** `internal/api/handlers_chat.go`: `streamSessionEvents` sends `event: heartbeat` `{"sessionId","running"}` every `sseHeartbeatInterval` (5 s, var for tests) while the stream is open. Before, a quiet run (long tool, slow model) and a dead connection looked the same. WebUI's SSE parser ignores unknown event types (`parseSSEPayload` default), so it is backward compatible.
- **Client** `internal/nativeui/client/events.go`: `EventHeartbeat = "heartbeat"`, `Running` decoded for it. Not content (`HasContent` false).
- **Chat** `internal/nativeui/app/chat/liveness.go` (new):
  - `Conversation.heard` (any stream event, heartbeats included) / `worked` (agent events only) / `reconnected`; set by `heardFrom` in `applyEvents`, reset by `BeginRun`/`BeginAttach`.
  - `liveness()`: `liveNone` (not streaming), `liveActive`, `liveStalled` (nothing for `stallAfter` = 20 s = 4 missed heartbeats while the run is not finished).
  - `trail(box)`: rotating `BorderConicGradient` comet (accent head, `Loop("trail", 2.4s)`) on the composer box while active; stops when stalled or done.
  - Hint shows "Working · no output for N s" when heartbeats arrive but the agent produced nothing for `quietAfter` (30 s).
  - Stall: first stall auto-reconnects (synthetic `EventReattach` truncates the partial turn, then `attach` replays the run); a second stall shows a warning notice "No response from the agent for N s, but the run has not finished. The connection may be down." with a Reconnect button (Stop stays in the actions row).
  - `watch(ctx)`: 1 s ticker invalidating the view while a stream is open (MyGo has no timed invalidate), so stalls appear/count without events. `newTag` starts it; `Conversation.Finish` now calls `cancel` to stop it.
- **Settings** `app/settings/general.go`: debug ID shown monospace with a copy icon button (`kit.CopyText`, success toast).
- **i18n**: new keys in all 7 locales of `web-ui/src/i18n/locales` copied to `internal/nativeui/i18n/locales` (must stay byte-identical, `TestLocalesInSyncWithWebUI`): `settings.general.telemetryCopyId`, `telemetryIdCopied`, `chat.composer.quiet`, `stalled`, `reconnect`.

## Verification
- `TestStreamSendsHeartbeatsWhileQuiet` (api, -race), `TestParseHeartbeat` (client), `TestComposerLivenessFromHeartbeats` (chat: heartbeat keeps alive, quiet hint, auto reconnect then notice, heartbeat clears, Finish -> none).
- `go vet` + `go test ./internal/nativeui/... ./internal/api/` green; gofmt clean.
- Not visually checked (no display in the agent shell).
