---
created_at: 2026-08-26T22:05:29.460350936Z
updated_at: 2026-08-26T22:05:29.460350936Z
tags:
    - feature
    - design
    - cli
    - tui
    - acp
    - desktop
---
# Design Studio P5 — Desktop, TUI, ACP and CLI surfaces

Implemented 2026-08-26/27. Phase P5 of [[pando_design_studio_plan]], on top of
[[design_p4_webui_studio]] (WebUI Studio), [[design_p3_preview_server_surfaces]] (preview
server, SSE, access guard) and [[design_p2_tools_patch_engine]].

Exit criterion of the phase: **the same artifact reachable from all four surfaces plus the
CLI in one session.**

## The shared floor — `internal/design/surface.go` (new)

The four surfaces must not each invent their own lookup and their own idea of where a
preview lives. Two functions carry that:

- `(*Service).Resolve(ctx, ref) (Artifact, error)` — turns a human-typed reference into an
  artifact with **ordered** precedence: exact id, exact slug, id prefix, then a
  case-insensitive substring of slug or title. Ordered rather than scored on purpose: an
  exact id must never lose to a substring match on another artifact's title. An empty
  reference means "the one I am working on" (most recently updated), which is what a bare
  `pando design open` or `/design-open` should do.
- `ErrAmbiguousRef` — a reference matching several artifacts is reported with its
  candidates, never silently resolved. Every surface here *acts* on the result (opens a
  browser, exports a file), so guessing is the expensive failure.
- `(*Service).LiveURL(ctx, artifactID, slide) (Presentation, error)` — the single place a
  listener is allowed to come into existence: on an explicit request to *show* something,
  never as a side effect of rendering or listing. A preview server that cannot start is not
  fatal; `Presentation` degrades to `file://` on its own.

## CLI — `cmd/design.go` (new)

`pando design list | create | versions | open | export | system show|init`, `--json` on all
of them (a persistent flag on the parent).

- `runWithDesignService` boots config + DB + `design.NewProvider` and installs it as the
  process default, because the preview server resolves artifacts through it. **`design.enabled`
  gates the tools, not this command**: a user who does not want ten design tools in the
  model's context can still inspect and export what a previous session built.
- `open` starts the loopback preview, prints the URL, opens a browser and **blocks until
  interrupted** — the preview is served *by this process*, so returning would take the URL
  down with it. `--no-wait` opens the `file://` address and returns (no bridge, no relative
  asset serving).
- `export --slide` maps 0 to -1 (whole document) because `ExportOptions.Slide` uses -1 for
  "all", and a shell user typing nothing means "all".
- `silenceUsage(designCmd)` walks the tree: cobra reads `SilenceUsage` off the command that
  failed, not off its parent, so a "not found" was printing the whole flag list under the
  one line that says what went wrong.
- `humanAge` keeps tables narrow; the exact timestamp lives behind `--json`.
- **Deviation from the plan**: `system extract` is *not* implemented. The extractor (design
  system from code / URL / images) is P6 work and does not exist yet; `system show` and
  `system init` expose what the `design_system` tool already backs. No stub was added that
  would lie about the capability.

## TUI — `internal/tui/page/design.go` (new)

Two panels: artifact list and a detail panel with versions, an optional inline screenshot
and an optional diff. Keys: `↑/↓` select, `o` open preview, `s` screenshot, `d` diff, `r`
reload.

- Read-and-open, **not an editor**: artifacts are authored by asking the agent in the chat
  tab. What a terminal adds is that it is already open.
- The screenshot is bound to `s` rather than fired on selection: it costs a headless browser
  render, and arrowing through a list would fire one per keystroke.
- `o` goes through `LiveURL`, so a TUI with no API server starts the loopback preview
  instead of handing the browser a `file://` address whose relative assets and bridge do not
  work.
- Stale async replies are dropped by comparing the reply's artifact id against the current
  selection; otherwise one artifact's versions render under another's title.
- The service is resolved per command, not captured: the design provider is installed during
  application start-up, which can happen after the TUI model is constructed.

New general TUI plumbing: **`page.Refreshable`** (`Refresh() tea.Cmd`), called by
`moveToPage` on every navigation. `Init` runs once per process, so a page built once would
show a stale list exactly when the user navigates to it to see what the agent just made.

Registration: `page.DesignPage`, `keys.Global.Design` = `ctrl+alt+d`, help section, Esc back
to chat, and a "Design Studio" command-palette entry.

## ACP — `internal/mesnada/acp/design_commands.go` (new)

`/design [artifact]`, `/design-open [artifact] [slide]`, `/design-versions [artifact]`.
Control commands, not agent turns: they answer from the design store and end the turn.

- `/design-open` sends a text header, then `acpsdk.ResourceLinkBlock` with the live URL,
  then an `ImageBlock` screenshot. A missing browser reports itself and is swallowed — it
  must not turn a working preview link into a failed command.
- The screenshot goes through `imageopt.Normalize` (1280px long side, 1 MiB base64 cap):
  raw, a full-page render is megabytes of base64 inside a JSON-RPC frame.
- `splitDesignSlide` accepts `<ref> 3`, `<ref> #3` and a bare `3`, because "/design-open 3"
  on a deck the user is already looking at is the shortest thing they will type. A slug
  ending in a digit (`deck-v2`) stays part of the reference.
- **Deviation from the plan**: the plan asked for an `ImageBlock` *per version*. A version is
  a directory-scoped snapshot, not a stored document, so rendering one would mean checking
  it out over the user's working tree first — destructive for a read-only command. History
  is text; the single image is the current version, which is what exists on disk. Documented
  in the function comment.

### `design` tool kind — `tool_render.go`

ACP's `ToolKind` is a **closed protocol enum** with no `design` member; a client given an
unknown value shows a generic icon at best. So `designToolKind` maps each design tool to the
protocol kind matching what it does to the workspace: `design_create/patch/export/canvas/system`
→ `Edit`, `design_render/screenshot/inspect/versions` → `Read`, `design_present` → `Fetch`.
`designToolTitle` names the artifact, because "design_patch" repeated eight times tells the
user nothing about which artifact is changing.

## Desktop (Wails) — `internal/desktop/app.go`

- "Design Studio" menu entry (Cmd/Ctrl+Shift+D) navigating the webview to `/design`.
- `OpenInBrowser(url)` — a preview belongs in a real browser with devtools, and a PDF the
  webview cannot display should not become a blank panel.
- `SaveFileDialog(title, defaultFilename)` and `SaveDownload(url, defaultFilename)` — a
  webview is not a browser: `window.open` on an export URL has nowhere to put the file, so
  the fetch and the write happen on the Go side behind a native save dialog.
- **No CSP widening was needed.** `OnDomReady` navigates the webview to the Pando origin
  itself, so the Studio page and the `/preview/` iframe share one origin and
  `frame-ancestors 'self'` is satisfied. `preview.Options.FrameAncestors` (added in P3)
  stays for a shell that ever stops doing this; the existing
  `TestFrameAncestorsIsConfigurable` already pins both halves.

### Frontend — `web-ui/src/services/desktopRuntime.ts` (new)

`openExternal` and `saveUrlToDisk`, used by `DesignStudio.tsx` (open-external button) and
`ExportMenu.tsx` (download). They talk to `window.go.desktop.App` **directly**.

**Pre-existing repo problem found here, not fixed:** `wailsBindings.ts` dynamically imports
`../../wailsjs/go/desktop/App`, which resolves to `web-ui/wailsjs/…` — a directory that does
not exist. The only checked-in `wailsjs/` is at the repo root and is stale (it declares
`GetServerInfo`, `AssetsHandler`, `GetVersion`, `SelectDirectory`, `OpenFileDialog`,
`ShowMessageDialog`, `WindowMinimise/Maximise/ToggleMaximise/WindowSetTitle`, none of which
exist on the current `desktop.App`). So **every** Wails binding call in the WebUI currently
falls back to its web behaviour. `desktopRuntime.ts` sidesteps it by using the runtime
injection Wails performs with no generated file involved. Regenerating those bindings is
separate work.

## Tests

- `internal/design/e2e_p5_test.go` (3): **`TestOneArtifactReachableFromEverySurface`** is the
  exit criterion — one deck resolved by id, slug, id prefix and title words (one per
  surface's habit), plus the empty default, then `LiveURL` starting a loopback server and
  both the plain and the bridged URL fetched over real HTTP with the `#slide-2` deep link
  intact. Plus ambiguity reporting and unknown-reference rejection.
- `internal/mesnada/acp/design_commands_test.go` (6): commands advertised with hints,
  parsing, `/design` not shadowing `/design-open`, `splitDesignSlide` table, tool-kind
  mapping, tool titles.
- `internal/tui/page/design_test.go` (7): missing-subsystem message, empty state, selection
  bounds, stale-reply dropping, `Refreshable`, key help, sentinel error text.
- `cmd/design_test.go` (5): subcommand surface as a contract, registration, format/kind
  validation before any database work, `humanAge` zero time.
- `internal/mesnada/acp/agent_pando_test.go`: the exact available-command count assertion
  18 → 21, and the three design tokens added to its exposed/hint lists.

## Verification

- `go build ./...`, `go vet` on every touched package, `gofmt` clean.
- `go test ./internal/design/... ./internal/api ./internal/llm/tools ./internal/mesnada/acp
  ./internal/tui/... ./internal/desktop ./cmd ./internal/app ./internal/config ./internal/db`
  — all ok.
- Frontend: `bun run typecheck` clean, `bun run lint` 0 errors (4 pre-existing warnings in
  `KeyValueEditor.tsx` / `ModelCombobox.tsx`, untouched), `bun run build` ok.
- **Real binary smoke test** in a fresh temp project: `design list` (empty state),
  `create` web + deck, `list` table, `versions quarterly` (title substring), `export landing
  --format html --out …` (471 bytes written), `list --json`, `system show` (default token
  set), ambiguity error, not-found error, `open --no-wait --json` (file:// presentation), and
  `open` serving — `GET http://127.0.0.1:PORT/preview/<token>/index.html` returned **200**
  with the full locked CSP including `connect-src 'none'` and `frame-ancestors 'self'`.

## Next — P6

Design system: `designer/_system/DESIGN.md` + `design-system.json` schema, the extractor
(code / URL / images) that `pando design system extract` is waiting on, the applier,
prompt-constraint injection, KB mirroring, and the Settings UI to pick the active system per
project.
