# Native desktop UI (MyGo)

`pando desktop` runs the API server in process and shows this native UI on the
main goroutine (the only desktop front end). Plan:
`.kb/pando/plans/native_desktop_mygo.md`.

Toolkit: [MyGo](https://github.com/egoist/mygo) **v0.7.1, pinned exactly**
(pure Go, GPU rendered, immediate mode with keyed retained state). It needs Go
>= 1.27.3: `GOTOOLCHAIN=go1.27.3 ~/.g/go/bin/go ...`. MyGo docs:
`$(go env GOMODCACHE)/github.com/egoist/mygo@v0.7.1/docs/ui/*.md`.

## Packages

| Package | Role | Imports MyGo |
|---|---|---|
| `client/` | Pando HTTP API client: token, local CA TLS, REST, SSE parser, chat stream with reattach, event subscriptions, DTOs | **no** |
| `i18n/` | Web UI catalogs (copied from `web-ui/src/i18n/locales`, a test keeps them in sync) and `T(key, "name", value...)` | no |
| `kit/` | Theme (web design tokens -> `ui.Theme`), lucide icon subset, base components | yes |
| `shell/` | `mygo.App`, main window, application menu (Edit roles), later tray/notifications/dock | yes |
| `app/` | `Context`, area registry, router, window layout (sidebar + header + content), placeholders | yes |
| `app/<area>/` | One package per area: `chat` (P0 MVP), then `settings`, `files`, `editor`, `terminal`, ... | yes |
| `nativeui.go` | `Run(Options)`: client + context + root view + shell; blank-imports the area packages | yes |

## Adding an area (parallel follow-up work)

1. Create `internal/nativeui/app/<area>/` with a type implementing
   `app.View` (`Build(c *ui.Context, r *ui.Route)`).
2. Register it from `init()` with the **same ID** as its placeholder in
   `app/placeholders.go` (a real registration always replaces a placeholder):

   ```go
   func init() {
       app.Register(app.Area{
           ID: "settings", Path: "/settings", Patterns: []string{"/settings", "/settings/{section}"},
           TitleKey: "nav.settings", Icon: kit.IconSettings, Section: app.SectionBottom,
           New: func(a *app.Context) app.View { return newSettings(a) },
       })
   }
   ```
3. Blank-import the package in `nativeui.go`.
4. Optional interfaces on the view: `app.Titled` (header title from state),
   `app.HeaderActions` (buttons in the header bar), `app.Closer` (stop
   goroutines at shutdown).

Area IDs and paths: chat `/chat` (+`/chat/s/{id}`), simple-chat `/chat/simple`,
projects, editor `/editor/{path...}`, terminal, orchestrator, design
`/design/{id...}`, logs, agentvcs `/snapshots/{id...}`, evaluator, instances,
extensions, settings `/settings/{section...}`, setup (hidden).

## Routing

The app's routers show pages at once (`ui.TransitionNone`, as the Web UI).
An area view is one stateful object: MyGo's sliding transition builds the
page going away too, which built the view twice per frame ("a ListState
shown by two Lists"); `Root.leavingSameArea` keeps that safe if a router ever
slides again, and a fade left pages transparent while the window got no
frames (locked macOS screen). Areas keep their state in sync with the route:
the chat pushes `/chat/s/{id}` on session clicks and follows Back/deep links
(`app/chat/shellwire.go`); agentvcs follows `/snapshots/{id...}` in place.

## Rules

- **UI thread only.** View state is touched only in `Build`, or from goroutines
  through `a.Update(func(){...})` (`app.Context.Update` -> `mygo.Window.Update`).
  Never mutate state from a goroutine directly.
- **Streams feed the UI in batches.** Use `app.NewBatch(a.Update, apply)`: many
  `Push` calls from a goroutine become one UI update per frame (the chat does
  this for token deltas).
- **Background work** goes through `a.Go(func(ctx){...})`: it recovers panics
  (toast + log) and its ctx ends at shutdown. Use `a.Client` for every server
  call; add typed wrappers to `client/` (no MyGo there) and use
  `Client.Do/Get/Post/...` meanwhile.
- **Look and feel** comes from `kit`: `kit.Current()` returns the tokens
  (`Bg`, `BgShell`, `BgCard`, `Fg`, `FgMuted`, `Accent`, `Border`, radii, text
  sizes) of the frame; MyGo widgets already follow the mapped `ui.Theme`.
  Prefer kit components (`Button`, `IconButton`, `Card`, `Badge`, `Input`,
  `Switch`, `Select`, `Dialog`, `SettingsSection/Row`, `NavItem`, `CodeBlock`,
  `EmptyState`, `Spinner`, `ToastViewport`) and add new shared ones to `kit`.
- **Icons** are lucide SVGs embedded in `kit/icons`; add a file there and a
  constant in `kit/icons.go`. Source used: lucide 1830-icon set at
  `~/.cargo/registry/src/index.crates.io-*/gpui-kit-assets-0.7.2/assets/icons`.
- **Text** goes through `a.T("web.ui.key")` so the 7 Web UI languages work;
  `count` selects `_one/_other` plurals.
- **Toasts** from goroutines: `a.Toasts.Error(title, detail)` (queued, flushed
  in the root view).
- **Keys.** Elements built in loops that keep state need `c.Key(id)`; lists use
  `ui.ListState.Key`. Do not set `ListState.Selected` when rows paint their own
  selection (it paints the accent colour).
- **Tests.** Headless with `ui.NewTester(view, w, h)` (`Click`, `Type`, `Key`,
  `Compose`, `Scroll`, `HasText`); drive goroutine updates with
  `app.QueueDispatcher` (`a.SetDispatcher(q)`, then `q.Drain(); tt.Frame()`).
  Server behaviour with `httptest` (see `client/client_test.go`,
  `app/chat/chat_test.go`). Recorded SSE/JSON fixtures live in
  `client/testdata` (from the GPUI client, Pando v1.3.0).

## Protocol lessons (ported from pando-assistant/crates/pando-client)

- Token: `GET /api/v1/token` (loopback servers hand it out), sent as
  `X-Pando-Token`, with `X-Pando-Client: desktop`; on 401 the client fetches a
  new token once (single-flight) and retries.
- https servers: the local CA `~/.config/pando/tls/ca.crt` is trusted in
  addition to the system roots.
- `POST /api/v1/chat/stream` body closing without `done` while the run is still
  active (`/sessions/{id}/pending`) => emit `reattach` and follow
  `GET /sessions/{id}/stream`, which **replays the run from its start**:
  discard the partial turn on `reattach`.
- A stream ending without `done` and no content (slash commands, runs already
  over) => reload history.
- Steer `409 not_busy` => send the text as a normal prompt.
- `PUT /api/v1/models/active` must carry `sessionId` from a chat, otherwise it
  rewrites the persisted coder model.
- Never reload history while this client streams its own run; tag streams so
  events of superseded streams are dropped.
- Runs started elsewhere: `/sessions/events` only reports a run once it ends,
  so the open session is also polled (`/pending`, 4 s) and attached when
  `running`, or reloaded when `run_seq` grew (run_seq protocol).

## Development aids

- `PANDO_NATIVE_CAPTURE=<dir>` + `PANDO_NATIVE_CAPTURE_AT=6,12` write
  `<dir>/capture-<n>s.png` with MyGo's renderer (works where compositor
  screenshots are refused).
- `PANDO_NATIVE_PATH=/chat/s/<id>` opens a route at start.
- `PANDO_NATIVE_CAPTURE_PATHS=/chat,/logs,/settings/appearance` (with
  `PANDO_NATIVE_CAPTURE`) visits each route in turn and writes
  `capture-<route slug>.png` (`PANDO_NATIVE_CAPTURE_STEP` seconds apart,
  default 3): one launch screenshots every area.
- `PANDO_NATIVE_SPIKE_MESSAGES=1000` opens a synthetic transcript.
- `MYGO_FRAME_STATS=all` logs frame timings (they land in Pando's log store:
  `GET /api/v1/logs`).
- `PANDO_NATIVE_IT_URL=https://localhost:8765 go test -run TestRealServerOwnRun
  ./internal/nativeui/app/chat -v` drives the chat headlessly against a real
  server (`PANDO_NATIVE_IT_CANCEL=1` stops mid-run).

## Window shell (P1-shell)

- `shell.Controller` (given to `shell.Options.OnReady`; `app.Host` is the
  interface the app sees): tray icon (Show, Settings, open project
  workspaces, Quit), hide-to-tray on close/minimise when
  `desktop_tray_on_minimize_close` is on **and** a tray icon is live (no
  libayatana on Linux -> normal behaviour), explicit quit always quits,
  macOS Dock click restores, OS notifications, window focus state.
- Single instance: `nativeui.AcquireSingleInstance(cwd)` (called first in
  `cmd/desktop.go`) is keyed by working directory so a project window
  (`pando desktop --cwd X`) is a separate instance. `PANDO_DESKTOP_MULTI=1`
  disables the lock (use it when several agents/tests run `pando desktop`
  from the same directory). A second launch with `--simple` shows the
  simple chat.
- `app.Context.StartServices(host)` (after the window exists): UI prefs
  (`/api/v1/ui/preferences`: theme family/mode/accent, language, sidebar,
  chatMode), the tray setting followed live via `/config/events`,
  notification relay (`/notifications/stream`, only when unfocused).
  Persist with `a.SetTheme`, `a.SetLanguage`, `a.SetSidebarCollapsed`,
  `a.ToggleSimpleChat`.
- Extension slots (all in `app/shellstate.go`): `RegisterHeaderSlot`
  (persona selector: ID `persona`, order 10), `RegisterOverlay`,
  `RegisterBanner`, `RegisterCommand` (quick menu), `Shell().NextModel` /
  `Shell().ModelChanged` (model switcher <-> chat), `Shell().FocusProject`.
  Overlays live in `app/overlays` (quick menu, model switcher, config-init
  banner, update notice). `RegisterStatusItem` adds to the status strip under
  the main tab (the extensions "status-bar" slot uses it; the strip only
  shows when an item is visible). Quick menu commands may set `TitleKey`.
- Chat wiring (`app/chat/shellwire.go`): persona selector in the header slot
  `persona` (composer chip only in project workspaces), `Shell().NextModel`
  sent with a new chat's first prompt, `Shell().ModelChanged` chained,
  Shift+Tab toggles auto-approve, the design draft (`TakeChatDraft`) fills
  the composer.
- Theme at start: desktop UI preferences, else the backend `theme` field
  (`app.ParseThemeID`, legacy families mapped); Settings > Appearance saves
  both and persists the accent in the preferences.
- Global keys (`app/chrome.go`): Cmd/Ctrl+K or +P quick menu, +O model
  switcher, +B sidebar, +Shift+L theme, Ctrl+Alt+1..9 / Left / Right / W
  project tabs. Errors from background calls: `a.ReportError(title, err)`
  (401 gets a clear, de-duplicated toast).
