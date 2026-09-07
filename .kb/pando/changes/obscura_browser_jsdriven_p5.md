---
created_at: 2026-09-07T15:52:55.040549955Z
updated_at: 2026-09-07T15:52:55.040549955Z
tags:
    - change
    - browser
    - obscura
    - cdp
    - internal-tools
    - chromedp
---
# Change: JS-driven fallback path for Obscura (Phase 5 of Obscura support)

**Date:** 2026-09-07
**Status:** Done
**Plan:** [[pando/plans/obscura_browser_support_plan.md]] — this is a Phase 5 that the stored plan document did not
anticipate (it only listed P1-P4); it exists because live testing against real obscura 0.2.2 surfaced a
protocol-compatibility gap the plan's P1-P4 could not have caught.
**Builds on:** [[pando/changes/obscura_browser_detection_p1.md]] (P1 detection),
[[pando/changes/obscura_browser_remote_launch_p2.md]] (P2 CDP-server launch),
[[pando/changes/obscura_browser_support_p3_selector_config.md]] (P3 selector/config surfaces).

## Problem

Once P1-P3 landed, driving obscura through the `browser_*` tools hung until timeout. Root cause, verified live
against obscura 0.2.2 at `/home/sevir/bin/obscura`: obscura never emits the `DOM.documentUpdated` /
`DOM.setChildNodes` CDP events that chromedp's high-level *query* actions (`WaitVisible`, `Click`, `SendKeys`,
`OuterHTML`, `Text`, selector `Screenshot`) depend on to populate chromedp's internal DOM node cache. Its
`DOM.getDocument` also returns root NodeID 0. Every `chromedp.ByQuery`-based action therefore blocked until
"context deadline exceeded".

What does work against obscura (verified): `chromedp.Navigate`, `page.Navigate`, `chromedp.Evaluate`,
`chromedp.Title`, raw one-shot `dom.Enable`/`dom.GetDocument`/`dom.QuerySelectorAll`/`dom.GetOuterHTML` calls (no
event cache), `chromedp.FullScreenshot`/`chromedp.CaptureScreenshot`, and clicking/typing through plain JS
(`document.querySelector(sel).click()`, setting `.value` + dispatching `input`/`change`).

## What changed

New file `internal/llm/tools/browser_jsdriven.go`:

- `browserNeedsJSDriver(browserType string) bool` — true only for normalized `"obscura"`. Lightpanda and every
  locally-launched Chromium-family engine (chrome, chromium, msedge, opera) emit the DOM cache events normally
  and keep using the existing chromedp query path unchanged.
- `browserSessionIsJSDriven(pandoCtx context.Context) bool` — looks up the session by ID in
  `globalBrowserRegistry` (briefly taking/releasing `globalBrowserRegistry.mu`, never held across the call, to
  avoid a self-deadlock), returns its `jsDriven` flag, or `false` if no session ID/session is registered yet.
- A `jsDriven bool` field on `browserSession` (`internal/llm/tools/browser_session.go`), set in
  `GetOrCreateBrowserSession` from `browserNeedsJSDriver(resolvedInstall.Type)` right where the session struct is
  built.
- JS-based `chromedp.Action` fallbacks, each built on a small testable expression-builder function
  (`jsWaitVisibleExpr`, `jsOuterHTMLExpr`, `jsTextExpr`, `jsClickExpr`, `jsFillExpr`,
  `jsElementScreenshotRectExpr`) so the generated JS can be unit-tested without a browser. Every selector (and the
  fill value) is JSON-encoded via `encoding/json` before being spliced into the expression — never raw string
  concatenation:
  - `jsWaitVisible(selector, timeout)` — polls `document.querySelector(sel)` existence + visibility
    (`offsetParent !== null || getComputedStyle(el).display !== 'none'`) roughly every 100ms via
    `chromedp.Evaluate`, honoring `ctx.Done()`, erroring with a clear message on timeout.
  - `jsOuterHTML(selector, out)` — `document.querySelector(sel)?.outerHTML`; falls back to
    `document.documentElement.outerHTML` for an empty selector or `"html"`; errors (via `chromedp.ErrJSNull`) if
    the element is missing.
  - `jsText(selector, out)` — `innerText`, falling back to `textContent` for elements without `innerText`
    (e.g. SVG).
  - `jsClick(selector)` — `scrollIntoView({block:'center', inline:'center'})` then `.click()`.
  - `jsFill(selector, value)` — focuses the element; for `contenteditable` sets `.textContent`; for
    `<input>`/`<textarea>` sets `.value` through the native property setter
    (`Object.getOwnPropertyDescriptor(HTMLInputElement.prototype /* or HTMLTextAreaElement.prototype */,
    'value').set`) so framework (React/Vue-style) listeners that hook the setter still fire; then dispatches
    bubbling `input` and `change` events. `clear_first` becomes a no-op on this path since the value is always
    set directly (nothing to append to).
  - `jsElementScreenshot(selector, buf, quality)` — reads `getBoundingClientRect()` via `Evaluate`, then calls
    `page.CaptureScreenshot().WithClip(&page.Viewport{X,Y,Width,Height,Scale:1}).WithCaptureBeyondViewport(true)`,
    mirroring the same PNG/JPEG format-by-quality logic as `chromedp.FullScreenshot`.

Wired the fallbacks into the four call sites named in the P5 task, each branching on
`browserSessionIsJSDriven(ctx)` and leaving the existing chromedp path byte-for-byte for every other browser:

- `internal/llm/tools/browser_navigate.go` — `WaitVisible("body")` and the optional `wait_for` selector →
  `jsWaitVisible`.
- `internal/llm/tools/browser_content.go` — `OuterHTML`/`Text` → `jsOuterHTML`/`jsText` (the `Title` branch was
  already fine and is untouched).
- `internal/llm/tools/browser_interact.go` — click tool: `WaitVisible`+`Click` → `jsWaitVisible`+`jsClick`; fill
  tool: `WaitVisible`+`SendKeys`(+optional `Clear`) → `jsWaitVisible`+`jsFill`. The scroll tool already used
  `chromedp.Evaluate` and needed no change.
- `internal/llm/tools/browser_screenshot.go` — the selector-scoped screenshot branch → `jsElementScreenshot`; the
  full-page branch (`chromedp.FullScreenshot`) already worked and is untouched.

## Files/symbols touched

- `internal/llm/tools/browser_jsdriven.go` (new) — `browserNeedsJSDriver`, `browserSessionIsJSDriven`,
  `jsDriverWaitTimeout`, `jsRect`, `encodeJSSelector`, `jsWaitVisibleExpr`/`jsOuterHTMLExpr`/`jsTextExpr`/
  `jsClickExpr`/`jsFillExpr`/`jsElementScreenshotRectExpr`, `jsWaitVisible`/`jsOuterHTML`/`jsText`/`jsClick`/
  `jsFill`/`jsElementScreenshot`.
- `internal/llm/tools/browser_session.go` — `browserSession.jsDriven` field; set in `GetOrCreateBrowserSession`.
- `internal/llm/tools/browser_navigate.go`, `browser_content.go`, `browser_interact.go`, `browser_screenshot.go`
  — JS-driven branches added at the call sites listed above.
- `internal/llm/tools/browser_jsdriven_test.go` (new) — unit tests for `browserNeedsJSDriver`, `encodeJSSelector`,
  each `jsXxxExpr` builder (asserting JSON-escaped selectors/values and the expected JS calls, including an
  explicit injection-attempt case for `jsFillExpr`), and `browserSessionIsJSDriven` (no session ID, unknown
  session, registered JS-driven session).
- `internal/llm/tools/browser_obscura_live_test.go` (new) — `TestBrowserObscuraLiveEndToEnd`, skipped unless
  `obscura` is on `PATH`. Spins up an `httptest.NewServer` page (`<h1 id="h">`, a `<button id="b">` that mutates
  the h1 via its own `onclick`, an `<input id="i">` that echoes into `<span id="echo">` via `oninput`), then
  drives the real tools end to end: navigate (+`wait_for`), `browser_get_content` (text and html), `browser_click`
  (verifying the page's own mutation happened, not just a "clicked: true" false positive), `browser_fill`
  (verifying the `input` event fired and was observed by the page), and `browser_screenshot` both full-page and
  element-scoped (exercising `jsElementScreenshot`). No public network is used, only the local httptest server.

## Why

Obscura's CDP server implements enough of the protocol for navigation, evaluation, and raw one-shot DOM/page
calls, but not the DOM event stream chromedp's convenience query actions are built on. Rather than adding a
second parallel `browser_*_obscura` tool surface, each existing tool now silently branches on whether its session
is JS-driven, so from the model/agent's point of view `browser_navigate`, `browser_get_content`, `browser_click`,
`browser_fill`, and `browser_screenshot` behave identically regardless of which CDP-server engine backs them.

## Verification

- `go build ./...` — succeeds.
- `go vet ./internal/llm/tools/...` — clean.
- `go test ./internal/llm/tools/ -run 'Browser|Remote|JSDriven|Obscura' -count=1 -v` — all pass. In particular
  `TestBrowserObscuraLiveEndToEnd` **actually ran and passed** (0.57s) against the real obscura 0.2.2 binary at
  `/home/sevir/bin/obscura` — it was not skipped. The three pre-existing Chrome-dependent tests
  (`TestBrowserSessionCleanup`, `TestBrowserMaxSessions`, `TestCloseAllBrowserSessions`) still skip in this
  sandbox as before (no Chrome/Chromium installed) — unrelated to this change.
- Manual read of the harness output confirmed the click and fill assertions check actual page mutation
  (`#h` text became "Clicked", `#echo` echoed the filled value through a real `input` event), not just tool
  "success" flags, so the test would catch a regression where `.click()`/native-setter dispatch stopped firing
  real DOM events.
- `internal/llm/agent` and `internal/api` were also run per project convention; `internal/api` passes.
  `internal/llm/agent` has 4 pre-existing failures (`TestSetAndGetCavemanMode`,
  `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`,
  `TestApplyToolDiscoveryWithoutManagerIsUnchanged`) unrelated to caveman/extension-tools defaults, not to
  browser tooling — no file outside `internal/llm/tools/` was touched by this change.

## Out of scope / follow-ups

- No change to `internal/browser/detect.go` (P1) or any file outside `internal/llm/tools/`, per this phase's
  instructions.
- The pre-existing `internal/llm/agent` test failures noted above are unrelated and were not investigated further
  as part of this phase.