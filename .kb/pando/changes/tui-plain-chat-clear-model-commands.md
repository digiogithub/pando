---
created_at: 2026-11-17T21:56:08.186317531Z
updated_at: 2026-11-17T21:56:08.186317531Z
tags:
    - change
    - tui
---
# TUI: clean chat render, /clear and /model slash commands (2026-11-17)

Requested by José via his assistant agent Pips (agentd msg #214): Pando runs as a remote agent under agentd (PTY), the screen is read as text, and the boxed chat rendering turned into spurious tables.

## Changes
- `internal/tui/components/chat/plain.go` (new): `SetPlainMode/IsPlainMode`, `roleBlock` (role marker prefix + indent, no border/box), notice wrapping. Markers: user `> `, assistant `● ` (plain `* `), tool `· ` (plain `- `), thinking `~ `, notice `# `.
- `message.go`: `renderMessage`, `renderThinkingBlock`, `renderToolMessage` no longer draw Thick/Normal left borders; they use `roleBlock`. Default mode is therefore borderless; plain mode also strips ANSI and padding spaces.
- `markdown.go`: in plain mode code blocks are indented text, no background/label chip.
- `chat.go` + `list.go`: `NoticeMsg` = local, non-persisted block appended after messages; cleared on `SessionClearedMsg` / session switch.
- `dialog/model_list.go` (new): `ListProviderModels`, `FormatProviderModels`, `ResolveModelArg` (exact id, `provider/id`, unique suffix).
- `page/chat.go`: `handleClearCommand` (/clear = New Session, cancels busy run), `handleModelCommand` (/model lists to chat, /model <id> emits `ModelSelectedMsg`).
- `tui.go`: `ModelSelectedMsg` handler now also posts the outcome as a chat notice (`modelChangeReport`).
- `commands/registry.go`: `clear`, `model` added; `llm/agent/setup_bridge.go`: both marked user-only for the agent.
- `cmd/root.go`: `--plain` flag; `PANDO_PLAIN`, `AGENTD_ID` env or `NO_COLOR` also enable plain mode.

## Verification
`go build ./...`; `go test ./internal/tui/... ./cmd ./internal/commands ./internal/llm/agent ./internal/api` pass. New tests: `chat/plain_test.go`, `dialog/model_list_test.go`. Not yet exercised in a real PTY under agentd.

## Caveats
- In WebUI/ACP `/clear` and `/model` are listed in the shared registry but not handled there (fall through as before).
- Typing a slash command in the TUI may open the completion menu; Enter may need pressing twice.
