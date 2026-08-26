---
created_at: 2026-08-26T17:27:48.180397901Z
updated_at: 2026-08-26T17:27:48.180397901Z
tags:
    - feature
    - design
    - p1
    - chromedp
    - renderer
    - inspector
---
# Design Studio — P1 Renderer + Inspector (implemented 2026-08-26)

Phase P1 of [[pando_design_studio_plan]], on top of [[design_p0_foundations]]: an
artifact-scoped headless-Chromium renderer, the `data-pando-id` node index, deck slide
handling, screenshots, PDF printing and canvas rasterization. Still no agent tools, HTTP
surface or UI — P2/P3/P4.

## Import-cycle decision: `internal/browser` extraction

The plan said "reuse `browserSession`", but P2 will register `design_*` tools inside
`internal/llm/tools`, which must import `internal/design`. `internal/design` therefore
**cannot** import `internal/llm/tools`.

Resolution: `internal/llm/tools/browser_detect.go` (stdlib-only, zero Pando deps) moved
verbatim to **`internal/browser/detect.go`**. `internal/llm/tools/browser_detect.go` is now
a thin alias file (`type BrowserInstall = browser.BrowserInstall`, plus wrappers for
`DetectInstalledBrowsers`, `ResolveBrowserInstall`, `NormalizeBrowserType`,
`IsRemoteBrowserType`), so the existing call sites in `internal/api/handlers_browser_config.go`
and `internal/tui/page/settings.go` are untouched.

The interactive `browserSession` in `internal/llm/tools` was **not** shared: the design
renderer keeps its own headless, throwaway-profile browser. Rendering an artifact must not
navigate away from the page the agent is interacting with through `browser_navigate`.

## New files in `internal/design`

- **`browser.go`** — design-owned chromedp session: lazy start, temp profile, headless,
  console (`Runtime.consoleAPICalled` + `exceptionThrown`) and network capture
  (`loadingFailed`, responses with status >= 400), both capped at 100 entries per render;
  `BrowserOptionsFromConfig` reads the same `internalTools.browser*` settings the
  `browser_*` tools use, so Pando has one browser configuration. `ErrNoBrowser` is a
  first-class error; remote CDP browsers (Lightpanda) are rejected because they lack the
  screenshot/print surface.
- **`render_script.go`** — the in-page walker (`indexScript`), the slide-rect script and
  `printBreakScript`. The walker stamps `data-pando-id` **in the live DOM only** — the
  artifact files are never rewritten by a render (covered by a test).
- **`renderer.go`** — `Renderer` with `Render`, `Screenshot` (viewport / element /
  deck-slide clip / full page), `PrintPDF` (`preferCSSPageSize`, print-media emulation),
  `SlideBreaks`, `Rasterize` (canvas → PNG, the image-generation path), `EntryURL`
  (`file://` until the P3 preview server), `Available()`. `runContext` ties every chromedp
  run to the caller's context, so a cancelled tool call aborts the page work.
  Defaults: 400 nodes, depth 14, 12 computed-style properties (`DefaultStyleProps`),
  slide selector `[data-slide], .slide, section.slide`.
- **`inspect.go`** — token-budgeted view over any node slice: filters (node subtree,
  selector/role, text, slide, depth), paging with `NextOffset`, **styles dropped unless
  `IncludeStyles`**, `MaxTextLen`, and a one-line-per-node `Text()` rendering for tool
  results.
- **`service_render.go`** — `Service.WithRenderer`, `Render` (renders + persists the index
  against the current version + syncs the deck slide count into `pando-design.json`),
  `Inspect`, `Node` (resolves a `design://<node_id>` selection). The renderer is optional:
  everything that does not need a browser keeps working without one.

## Verification

`go test ./internal/design` — 8 browser tests (skipped automatically when no
Chromium-family browser is installed; they ran against system Chrome here, ~2.5s total):

- node index has `#hero-cta` with right text, box, `font-size`, parent link; the console
  `error` from the page is captured; the stored index matches the render and
  `Node()` resolves the id.
- **render does not write `data-pando-id` back into the artifact file** (byte compare).
- deck: 3 slides counted, `#t0/#t1/#t2` attributed to slides 0/1/2, manifest deck block
  updated, slide filter does not leak other slides.
- **print fixture (the plan's deck risk)**: a deck with `@page` + `break-after: page`
  reports `break-after=page` per slide and prints a `%PDF`; the same check on a deck
  **without** print styles reports no page breaks.
- screenshots: viewport PNG, element PNG smaller than the page, one-slide clip, and a
  non-existent slide errors.
- `Rasterize` produces a PNG from a canvas scene and rejects an empty document.

Plus 6 pure inspect tests (paging/`NextOffset`, every filter, style dropping and subsetting,
text truncation, `Text()` rendering) that need no browser.

Regression: `go test ./internal/llm/tools ./internal/api ./internal/snapshot ./internal/db`
pass after the `internal/browser` extraction; `go build ./...`, `go vet`, `gofmt` clean.

## Next

P2: `design_*` builtin tools (create/patch/render/screenshot/inspect/versions/export/canvas/
system/present), the selector→file patch engine, and MCP export.

Related: [[pando_design_studio_plan]] · [[design_p0_foundations]]
