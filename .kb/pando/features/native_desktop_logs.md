# Native desktop: logs area (MyGo)

Date: 2026-10-27. Phase P3-ops. Plan: [[native_desktop_mygo]].

## What
- `internal/nativeui/app/logs/logs.go` (id `logs`, `/logs`): level segmented filter, client-side text search (message/source/level), auto-scroll toggle (`ListState.FollowEnd`), refresh, `ui.Table` (Time/Level/Source/Message), bottom detail panel (timestamp, level, source, message, details; selectable).
- `internal/nativeui/client/logs.go`: `ListLogs(LogQuery)` over `GET /api/v1/logs` (level/search/since/limit/offset).
- Polls every 5 s (last 200 entries) only while visible, like the web.

## Verification
`go test ./internal/nativeui/app/logs` (fake server: load, level filter, detail, search) + `go vet`. Real app (isolated HOME, `PANDO_NATIVE_PATH=/logs`, MyGo capture): scratchpad `screens/logs-table.png`.

## Gaps
Server-side search/pagination not used (web neither); no SSE (`/logs/stream` exists server-side); no copy-all/export.
