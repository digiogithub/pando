# Native desktop P0: foundation + spike (MyGo)

Date: 2026-10-27. Status: P0 done in jj workspace `/www/MCP/Pando/pando-native-desktop` (not committed). Plan: [[native_desktop_mygo]]. Analysis: [[native_desktop_mygo_replace_wails]].

## What changed
- `go.mod`: `go 1.27.3`, `github.com/egoist/mygo v0.7.1` (exact, direct). Side bumps from MVS: purego 0.10.2 -> 0.11.1 (also used by internal/uiauto darwin, builds on macOS), x/image 0.46, x/text 0.43, x/sync 0.24, go-text/typesetting 0.3.5.
- `cmd/desktop.go`: `pando desktop` starts the API server as before, then runs `nativeui.Run` on the main goroutine with the in-process base URL and `server.GetToken()`. Hidden `--webview` keeps the Wails path (and design auto-open windows) until P5. No display -> `nativeui.ErrNoDisplay` + hint `pando app`. New helper `shutdownServer`.
- `internal/nativeui/` (new tree, conventions in its README.md):
  - `client/` (no MyGo): `client.go` (Options, token fetch/single-flight refresh on 401, `X-Pando-Client: desktop`, local CA `~/.config/pando/tls/ca.crt` trust, `Do/Get/Post/Put/Patch/Delete`, `Health`, `OpenStream`), `sse.go` (incremental SSE parser: chunk/UTF-8/CRLF safe, comments, multi-line data, ids), `types.go` (Session, SessionSummary, SessionDetail, Message, Part with untagged classification, Pending, Model, ModelList, SessionModel, ServerInfo), `events.go` (StreamEvent + all event payloads, synthetic `reattach`), `chat.go` (`ChatStream`/`AttachStream` with reattach while `/pending` says running, `Steer` -> `ErrNotBusy` on 409, `Cancel`, `RespondPermission`), `sessions.go` (list/get/rename/delete/pending, models, `SetActiveModel(model, sessionID)`, `SubscribeSessionEvents`, generic `Subscribe` with backoff). Fixtures copied from pando-assistant (`testdata/`).
  - `i18n/`: Web UI locale JSON copies + `T(key, "count", n)` (dotted keys, `{{var}}`, `_one/_other`), sync test against web-ui.
  - `kit/`: `theme.go` (all 4 families x light/dark + 7 accents from tokens.css -> `Tokens` + `ui.Theme` + markdown theme; `Apply(c, Settings)` per frame, `Current()`), `icons.go` + 63 lucide SVGs (ISC LICENSE), `components.go` (Button variants/sizes, IconButton, Tooltip, Card, Badge+tones, Input, Switch, Select, Dialog, Toaster (goroutine-safe queue) + ToastViewport, SettingsSection/Row, NavItem, SectionLabel, CodeBlock, Spinner, EmptyState).
  - `shell/`: `Run(Options)`: app name, default menu (macOS app menu, File, Edit role menu, View, Window), main window with native decorations (`StateKey: main`), macOS reopen on Dock activate, quit on ctx cancel, `ErrNoDisplay`.
  - `app/`: `Context` (Ctx, Client, Server, CWD, Router, Toasts, Theme, Log, Update/Invalidate/Go/T/Navigate), `Batch[T]` (coalesce pushes into one UI update), `QueueDispatcher` (tests), registry (`Area`, `Register`, `Areas`, `Lookup`; `View`, `Titled`, `HeaderActions`, `Closer`), placeholders for every Web UI area, `Root` layout (collapsible sidebar with sections, header with title/actions, router, toasts, theme mode toggle).
  - `app/chat/`: chat MVP: sessions pane (list, live via `/sessions/events`, rename dialog + delete via context menu), history folding (tool results into calls), new chat, send (Enter; Shift+Enter newline), steering while running (409 -> prompt), stop, streaming markdown (`markdown.Stream` -> `View`) in a virtualized `ui.List` with FollowEnd + Smooth, thinking (collapsible), tool rows (status, expandable input/output), notices, errors, permission card (allow / allow for session / deny), context % badge, run follow for runs started elsewhere (poll `/pending` every 4 s + session upserts: reload history, trim to last prompt, attach), stream tags to drop superseded streams, no history reload while streaming. Spike generator (`PANDO_NATIVE_SPIKE_MESSAGES`).
  - `nativeui.go` (`Run`), `capture.go` (`PANDO_NATIVE_CAPTURE` dev aid), env `PANDO_NATIVE_PATH`.

## Why
Replace the Wails WebView wrapper (signing/embedding/GTK-WebKit issues) with a pure Go native UI talking to the same HTTP API as the Web UI; P0 proves toolkit fit and lays extension points so P1-P4 areas can be built in parallel.

## Verification
- `go build ./...` OK; `go vet ./internal/nativeui/... ./cmd/...` OK; `go test -race ./internal/nativeui/...` OK (client: SSE parser/fixtures, reattach, no-reattach for finished run, 401 refresh incl. 8 concurrent calls, 409 steer, https with CA file, session events reconnect; kit: tokens, icons, components headless incl. dialog/switch/toast; app: Batch coalescing, registry/placeholder override, routing; chat: event folding with real fixtures, reattach truncation, steering, history fold, full view against httptest server, 1000-row spike, IME/dead-key composition); `go test ./cmd/` OK.
- `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go vet ./internal/nativeui/...` and GOOS=windows: OK (builds too). Full pando binary needs cgo (tree-sitter) for cross builds, as before.
- `pando --help`, `pando desktop --help`, TUI start, `pando -p` (Ollama) work with MyGo linked in (init LockOSThread harmless).
- Real app on Linux (COSMIC Wayland and XWayland/X11) with isolated HOME + Ollama MiniCPM5-2B: window renders on GPU, sessions list, history, run started from another surface followed live and rendered (markdown, highlighted code, copy). Headless own-run against a real `pando serve` over https with local CA (`TestRealServerOwnRun`): streamed `- alpha\n- beta`; stop mid-run -> "request cancelled by user".
- macOS (Mac mini, git worktree, removed afterwards): vet, tests, full build, app launched and captured (Spanish locale from LANG).

## Spike findings
- 1000 messages: GPU frame stats (Wayland): first frame 118 ms (fonts + layout), then 1-3 ms; headless steady 0.2 ms/frame, scrolling 400 DIP steps 1.3 ms/frame; only visible rows built.
- IME: Linux uses GtkIMMulticontext (dead keys, IBus/Fcitx); headless composition test passes. Real dead-key typing could not be injected (xdotool XTEST/XSendEvent does not reach the XWayland window under COSMIC; ydotool would type into the user's session) -> manual check pending.
- Wayland: ~260 `Gdk-CRITICAL gdk_monitor_get_scale_factor/get_workarea` lines at start (MyGo queries the monitor before the window maps); harmless, not on X11. `cosmic-screenshot` portal refused from the agent shell -> MyGo `CapturePage` used instead.
- MyGo frame logs go through `log` -> Pando's slog store (`/api/v1/logs`), not stderr.
- No blocker requiring Pando core changes.

## Known gaps (P1+)
Question prompts (shown as notice), model picker/persona, slash and @ completion, attachments, diff viewer, plan/goal/todos panels, file-changes bar, tray/notifications/close-to-tray, single instance, UI prefs persistence (theme/language from `/api/v1/ui/preferences`), instance discovery for remote/child servers, `--simple` ignored by native path, sessions pagination beyond 100, design auto-open windows (webview only).
