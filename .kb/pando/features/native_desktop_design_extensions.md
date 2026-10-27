---
tags: [feature, desktop, mygo, native-ui, design, extensions]
---
# Native desktop P4: Design Studio + extensions (MyGo)

Date: 2026-10-27. Workspace `/www/MCP/Pando/pando-native-desktop` (uncommitted). Plan: [[native_desktop_mygo]]. Foundation: [[native_desktop_p0_foundation]].

## What changed
- `internal/nativeui/app/design` (area `design`, `/design`, `/design/{id...}`): store (designStore port: status, artifacts, versions, nodes, critique, templates, thumbnails), `/design/events` SSE kept alive for the whole app via `app.OnStart`; gallery (artifact cards with live screenshots, template gallery with Try it/Install), Studio (toolbar: back, Render, Chat, Canvas window, open external, Export menu html/png/pdf saved to ~/Downloads; canvas; deck slide strip; side column Inspector{Structure,Issues}/Versions), selection banner, "Ask about this element".
- Canvas = preview server HTML in `kit.WebPane` (MyGo embedded web view). The preview is the top-level page, so `window.parent === window`: a `PreloadScript` queues the bridge's `ready/selected/slide` messages; Go drains with `Page.Eval` every 150 ms (idle when the Studio is not on screen) and sends `select/goToSlide/clearSelection` as `window.postMessage`. No CSP change needed (frame-ancestors only matters for iframes; the page is not framed). Untrusted origins cannot call Go, Eval works anyway.
- Auto-open: `design.created` event navigates the main window to `/design/<id>` (once per artifact) + toast. Replaces the Wails `LaunchWindow` loop in `cmd/desktop.go`, which stays only on the hidden `--webview` path until P5. Canvas button opens `kit.OpenWebWindow` (native window with web view).
- `internal/nativeui/app/extensions` (area `extensions`, `/ext`, `/ext/{panelId}`): panel manifest (`GET /api/v1/extensions/ui`, loaded at start), list + panel page; sidebar entry hidden when no sidebar panels (new `app.Area.Visible`).
- Host page: `GET /api/v1/extensions/host?panel=<id>[&token=][&theme=]` (`internal/api/handlers_extensions_host.go`, route in routes.go, tests) provides `window.__PANDO_UI__` v1 (api get/post/put/patch/delete/getToken, apiBase '', panelId/extensionId in ctx) and imports `panel.entry`. LIMITATION: `ctx.react` is a stub that throws a readable error (the shell React lives in the bundled WebUI); panels that need React show it in their box; "Open in browser" opens WebUI `/ext/<id>`.
- Shared additions: `client/design.go`, `client/extensions.go`; `kit/webpane.go` (`WebPane`, `OpenWebWindow`, dev aid `PANDO_NATIVE_CAPTURE_WEB=<dir>` + `PANDO_NATIVE_WEB_EVAL=<js>`); `kit/icons` (+arrow-left, crosshair, download, upload, layout-grid, gavel, circle-question-mark); `app/webview.go` (`Context.WebViewHost`), `app/draft.go` (`InsertIntoChatDraft/TakeChatDraft/PeekChatDraft`), `Area.Visible`; blank imports in nativeui.go; `pando desktop --help` text (WebKitGTK only needed for these two features).
- Webview failure is graceful: no window/WebKitGTK -> message + "Open in browser" (tested headless).

## For other agents
- Chat agent: in the composer call `a.TakeChatDraft()` when shown and append (design "Ask about this element"/"Try it" navigate to `/chat` or `/chat/s/<session>` after `InsertIntoChatDraft`). Not wired yet.
- Settings agent: `extensions.Panels(a, client.SlotSettings)` lists sections (id `ext:<panelId>`, title `Title`); draw with `extensions.Slot(a, c, client.SlotSettings, panelID, 0)`. Chat info sidebar: `extensions.Slot(a, c, client.SlotChatSide, "", 220)`. Status bar: `extensions.Slot(a, c, client.SlotStatusBar, "", 28)`. All draw nothing on standard builds.

## Verification
- `go build ./...` (full binary), `go vet` of design/extensions/kit/client/cmd, `go test` of `app/design` (gallery, templates->chat draft, studio, selection->ask, issues + run critique, versions checkout, created-event auto-open, disabled notice, bridge messages, rebase, save), `app/extensions` (empty build, list/slots/fallback/order/Visible, HostURL), `internal/api` host page tests.
- Real app (Wayland, isolated HOME, `pando design create`): screenshots in scratchpad `screens/design-gallery.png`, `design-studio.png`, `design-preview-webview.png` (WebView.CapturePage, selection outline visible). Bridge probe: click on `[data-pando-id]` produced `selected design://n1` in the queue; `window.parent===window` true.

## Gaps
- Studio has no embedded chat column (toolbar jumps to the artifact session; draft handoff). Extension React panels need the browser. Extension host page not exercised against a real extension build (none in the OSS build). Mobile/narrow layout not special-cased (side column toggle only). Design system tokens/extract/apply endpoints (settings-side in web) not surfaced.
