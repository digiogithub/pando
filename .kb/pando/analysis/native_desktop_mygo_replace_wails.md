---
created_at: 2026-10-27T19:45:31.010408144Z
updated_at: 2026-10-27T19:45:31.010408144Z
tags:
    - analysis
    - desktop
    - mygo
    - native-ui
    - wails
---
# Analysis: native Go desktop (MyGo) replacing the Wails webview wrapper

Date: 2026-10-27. Status: analysis, no code. Decision pending (P0 spike proposed).

## Goal (user)
A desktop app equivalent to the WebUI, implemented natively in Go inside the pando module, no webview, removing Wails. Prior art: Rust + GPUI client `digiogithub/pando-desktop-assistant` (local `/www/MCP/Pando/pando-assistant`). Work in a jj workspace / worktree.

## Current Wails surface
`desktop/` separate Wails v2 binary (~2.5k LOC Go with `internal/desktop`), built by `wails build`, embedded via go:embed and extracted by `pando desktop`. Root of recent bugs: embedded macOS bundle lost code signature, GTK/WebKitGTK runtime libs, Windows release embedded a Linux ELF, Wails runtime re-injection on the Pando origin, Cmd+C/V needing Edit menu. CI: desktop-build.yml, release.yml, build-matrix.yml + Makefile desktop-* targets.

## MyGo (github.com/egoist/mygo v0.7.1, MIT) — findings (source read)
- Pure Go, no cgo: purego/dlopen on macOS+Linux, syscall/COM on Windows. Metal / D3D11 (WARP fallback) / OpenGL 3.3 in GtkGLArea; CPU raster fallback.
- Linux: GTK 3 dlopened at Init, native Wayland or X11 via GDK; still a runtime GTK3+Pango+cairo dependency (no WebKitGTK unless webview used).
- Immediate-mode API with keyed retained state, on-demand frames (0% idle CPU), own flexbox + grid.
- Text via system engines (DirectWrite, Core Text, Pango+HarfBuzz): shaping, bidi, CJK, emoji, fallback. IME on all three platforms. Accessibility tree on all three (not complete, #105).
- Widgets: textarea (rope, virtualized), rich text editor, Zed-style code editor (32 lexers, pluggable highlighter, LSP hooks), diff viewer (split/unified, word diff), **streaming LLM markdown** (tail re-parse, syntax healing, GFM tables/tasks/math), virtualized List with FollowAnimation (chat tail-follow), Table, Tree, Tabs, Sidebar, Split, menus, popover, dialogs, toasts, forms, charts, SVG, images, video, canvas, animations, terminal plugin (libghostty-vt), embedded webviews.
- Platform: tray, native menus, notifications (Windows balloons only), dialogs, clipboard incl. PNG, global shortcuts, multi-window, deep links, single instance, secrets, signed auto-update, `mygo build` packaging with signing/notarization.
- Embedding: library usable via go get; importing `mygo` locks main OS thread in init (global but harmless); `App.Run` must be on main goroutine; headless returns "cannot open display" error instead of crashing. Requires **Go >= 1.27.3** (we use 1.27.2).
- Maturity: repo created 2026-09-26 (~1 month), ~850 PRs, ~30 releases, one breaking API change already (with codemod), effectively one maintainer + AI-authored PRs, few external apps, open issues: layout perf #615, Linux 2nd GL window blank #680, unanswered security notice #840.

## Scope to replicate (web-ui inventory)
~41.9k LOC (31.5k src + 10.4k packages/pando-client), ~135 components, 7 locales. Biggest areas: Settings 23 sections (~10.4k LOC incl stores), Chat (~5k), layout/shell, setup wizard, projects/workspace tabs (iframes today), editor (Monaco), terminal (xterm + WebSocket PTY), design studio (iframe preview), orchestrator, agentvcs, evaluator, logs, instances, extensions (runtime ES modules — inherently web).
GPUI client already covers chat parity minus syntax highlighting, file-changes bar, attachments; nothing for the rest. Its pando-client crate documents protocol lessons: REST + chat/stream SSE (not AG-UI), run_seq reattach, body closing without done => reattach, steer 409 => resend as prompt, PUT /models/active needs sessionId, discovery via /tmp/pando-instances + local CA, X-Pando-Token from /api/v1/token. GPUI lessons: pin toolkit exactly behind a facade, use kit inputs not custom IME, virtualize the transcript, batch deltas per frame.

## Recommendation
Feasible; MyGo fits an AI chat client unusually well. Main risks: toolkit youth/churn/bus factor, full rewrite cost, Linux GL edges. Mitigate:
1. Native UI talks to pando through the existing HTTP API (Go port of pando-client logic), so web and native share one contract, child projects / remote instances work, and the UI can be swapped if MyGo dies. In-process optimisation later.
2. `internal/nativeui/kit` facade is the only package importing mygo/ui; exact version pin.
3. Web-only features (extension panels, design preview) via MyGo embedded webview.
4. Keep the React WebUI for browser/`pando app`/remote; native only replaces Wails.
5. Phases with go/no-go gate after P0 spike: P0 spike (workspace, Go 1.27.3, `pando desktop --native` window, streaming chat on real API, IME es/ja, 1000-message perf, Linux X11/Wayland + macOS + Windows) -> P1 shell + chat + tray/menus -> P2 settings (schema-driven forms) -> P3 editor/file tree/terminal/agentvcs/orchestrator/logs/evaluator/instances/projects -> P4 design studio + extensions (webview) -> P5 remove Wails (desktop/, launcher embed, Makefile, CI — pipeline changes need explicit user OK).

Related: [[desktop_frameless_titlebar_tray]], [[desktop_tray_setting_macos_native_titlebar]], [[wails_desktop_app_plan]]
