# Native desktop P1-chat: WebUI chat parity (MyGo)

Date: 2026-10-27. Workspace `/www/MCP/Pando/pando-native-desktop` (uncommitted). Builds on [[native_desktop_p0_foundation]].

## What changed
- `internal/nativeui/client/chatx.go` (new): commands, @ file search, personas, auto-approve, goal, question respond/cancel, orchestrator tasks, agentvcs diff, file-original, `SelectModel`, RoutingInfo/ContextFilterInfo decoding.
- `app/chat/model.go` rewritten: assistant turns are ordered parts (text/thinking/tool), consecutive assistant history messages merge, routing/context-filter/persona rows, plan, goal, modified files (tool results + history + agent-vcs), pending permission/question queues (dedupe, answered ids, merge with `/pending`).
- `tools.go`: tool classification (exec/read/search/fetch/edit/write/json), activity grouping and "ran N commands" summaries.
- `view_transcript.go`: tool cards, inline MyGo `editor.DiffView` for edit/write, images, folded groups, copy buttons, routing/filter/persona rows.
- `dialogs.go`: fail-closed permission dialog (no primary button, focus on Deny, Escape denies, outside click ignored, sandbox escalation variant) and question stepper (single/multi, other, review, Escape cancels).
- `composer.go`: slash popup, @ mention, Up/Down prompt history, steer/stop, auto-approve switch, context meter, char counter, queued feedback.
- `picker.go`: per-session model picker (PUT with sessionId, auto indicator, pending model for new chat), persona selector.
- `view_panels.go`: goal/plan banners, file-changes bar, full-window diff viewer (split/unified, agent-vcs blobs -> file-original -> reverted edits -> snippets), info sidebar (usage, subagents, plan, files).
- `sessions.go`: search, date buckets, load more. Area `simple-chat` at `/chat/simple` (no banners, footer status bar, sessions toggle).

## Verification
`go build ./...`, `go vet`, `go test -race ./internal/nativeui/app/chat ./internal/nativeui/client` pass (new `p1_test.go`: classification, grouping, ordering, dialogs via Tester + httptest, slash/mention/history, model picker, sidebar, simple layout). Real app on Wayland with isolated HOME and Ollama MiniCPM5: history with tool group, file-changes bar, sidebar, composer chips: `scratchpad/screens/chat-p1-session-edit.png`.

## Gaps
No image paste/attachments (the web composer has none). Info sidebar lacks version/sandbox/LSP sections. Live permission dialog not captured on screen (covered by headless test). Navigating routes between /chat and /chat/s/{id} double-builds the ListState (router transition), so session clicks do not change the route.
