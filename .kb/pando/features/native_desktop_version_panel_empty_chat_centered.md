---
created_at: 2026-10-28T21:52:43.691949753Z
updated_at: 2026-10-28T21:52:43.691949753Z
---
# Native desktop: version in chat info panel, translated update banner, centered empty chat

Date: 2026-10-28. Uncommitted. Related: [[native_desktop_run_liveness_heartbeat]], [[native_desktop_chat]].

## What changed
- **Chat info panel** (`internal/nativeui/app/chat/view_panels.go`): new "Version" section (`versionSection`) like WebUI `ChatInfoSidebar.tsx`: "Pando X.Y.Z" and, when `update_available`, `version.updateAvailable` + update command (default `pando update`) on an accent tile. `fetchVersion` calls `GET /api/v1/version` once per panel opening (`Chat.version`, `Chat.verFetched`, reset when the panel closes; server caches the GitHub lookup).
- **Update banner** (`internal/nativeui/app/overlays/banners.go`): texts translated via new keys `version.banner` ("Pando {{latest}} is available (running {{version}})."), `version.updateWith`, `version.releaseNotes`, `version.dismiss` (the config-init banner's Dismiss uses it too). Keys added to all 7 web-ui locales and copied to `internal/nativeui/i18n/locales` (byte-identical requirement).
- **Empty chat centered vertically** (`internal/nativeui/app/chat/chat.go`): `Conversation.isEmpty()`; when empty, Build renders `emptyHead` (icon + `chat.empty.title`, Grow(1), Justify(End)) in place of the transcript, then filesBar, composer, then `suggestions` (Grow(1)) — greeting + composer + suggestions sit in the middle, as WebUI `.chat-pane--empty`. The composer keeps its tree position so the first send does not remount it. Transcript no longer renders the empty state.

## Verification
- Tests: `TestEmptyChatCenteredVertically`, `TestInfoPanelShowsVersionUpdate` (chat/layout_test.go).
- `go vet ./internal/nativeui/...`, `go test ./internal/nativeui/... ./internal/api/` green; gofmt clean. Not visually checked (no display in agent shell).
