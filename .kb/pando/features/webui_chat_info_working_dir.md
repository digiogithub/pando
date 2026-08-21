---
created_at: 2026-08-21T07:42:59.710578679Z
updated_at: 2026-08-21T07:42:59.710578679Z
tags:
    - feature
    - webui
    - chat-info-sidebar
    - ui-parity
---
# Feature: working directory in the web-UI chat info sidebar

Date: 2026-08-21

## Motivation

The TUI shows the instance's working directory under the chat (`cwd(width)` in
`internal/tui/components/chat/chat.go`, rendered as `cwd: <path>` from
`config.WorkingDirectory()`). The web-UI right-hand info panel
(`ChatInfoSidebar.tsx`, the documented counterpart of the TUI sidebar) had no
equivalent, so a user connected to a remote / multi-project instance could not tell
which directory pando was actually working in.

## Changes

No backend change was needed: `GET /api/v1/project` (`internal/api/handlers_base.go`,
`handleProject`) already returns `{cwd, version}` from `ServerConfig.CWD`, which
`cmd/app.go` / `cmd/serve.go` / `cmd/desktop.go` fill with the same directory passed
to `config.Load` — i.e. the same value the TUI prints.

- `web-ui/src/stores/projectStore.ts`: new exported `Workspace` type
  (`{cwd, version}`), `workspace: Workspace | null` state and `fetchWorkspace()`
  hitting `/api/v1/project`. Failure resets to `null` (section simply hides).
- `web-ui/src/components/chat/ChatInfoSidebar.tsx`: new first `Section`
  (`faFolderOpen`, title `chat.info.workingDir`) rendering the path in the panel's
  monospace style with `overflowWrap: 'anywhere'` and the full path as `title`.
  Fetched once per panel open (`if (!infoSidebarOpen || workspace) return`), since the
  value only changes when pando restarts.
- `web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json`: new `chat.info.workingDir`
  key, inserted after `noSession`.

## Verification

- `tsc --noEmit` clean; `npm run build` and `bun run build:embedded` OK (embedded
  assets regenerated into `internal/api/webui/dist`, confirmed the new i18n key is in
  the bundle).
- `go build ./...` OK.
- Not run: a second pando instance in this repo to curl the endpoint — starting one
  would contend for the IPC lock of the live instance.

Related: [[fix_webui_mobile_chat_info_sidebar]], [[project_tui_chat_info_sidebar_plan]],
[[webui_phase1_layout]]
