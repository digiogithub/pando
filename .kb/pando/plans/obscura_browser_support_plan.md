---
created_at: 2026-09-07T15:29:22.404463792Z
updated_at: 2026-09-07T15:40:21.006819467Z
tags:
    - plan
    - browser
    - obscura
    - cdp
    - internal-tools
---
# Plan: Obscura headless browser as a selectable Pando browser

**Date:** 2026-09-07
**Status:** P1-P3 implemented; P5 (JS-driven compatibility layer) in progress
**Goal:** Let Pando detect a locally installed [Obscura](https://github.com/h4ckf0r0day/obscura) headless browser, offer it in the browser selector (TUI + WebUI + config), launch its CDP server on a free port and drive the `browser_*` internal tools through it.

Related: [[browser_tools_chromedp_plan]], [[uiauto_cdp_browser_phase6]], [[uiauto_backend_routing_config_fix]],
[[obscura_browser_detection_p1]], [[obscura_browser_remote_launch_p2]], [[obscura_browser_support_p3_selector_config]],
[[obscura_browser_jsdriven_p5]].

## Upstream facts (verified locally, obscura 0.2.2 at `/home/sevir/bin/obscura`)

- Rust headless browser engine (V8 + own CDP server), Apache-2.0. Binaries: `obscura`, `obscura-worker`.
- `obscura serve --host <H> --port <P>` starts a CDP WebSocket server; default `127.0.0.1:9222`.
- `GET /json/version` returns a Chrome-shaped payload with `webSocketDebuggerUrl`; `GET /json/list` lists page
  targets. So `chromedp.NewRemoteAllocator(ctx, "ws://host:port")` + the existing `waitForCDPEndpoint` readiness
  poll — the Lightpanda path — works unchanged.
- Relevant `serve` flags: `--allow-private-network` (default OFF: loopback/RFC1918 navigation blocked, which would
  break "agent opens the local dev server"), `--allow-file-access` (default OFF), `--stealth`, `--proxy`,
  `--user-agent`, `--workers`, `--max-connections`, `--storage-dir`, `--quiet`.
- Obscura is a CDP **server**, not a Chromium executable: it is a remote browser type
  (`browser.IsRemoteBrowserType`), never launched through `chromedp.ExecPath`.

## CDP compatibility gap found during verification (drives P5)

Measured against obscura 0.2.2 with chromedp:

| Works | Hangs until timeout |
|---|---|
| `chromedp.Navigate`, `page.Navigate`, `chromedp.Evaluate`, `chromedp.Title` | `chromedp.WaitVisible(sel, ByQuery)` |
| raw one-shot `dom.Enable` + `dom.GetDocument` + `dom.QuerySelectorAll` + `dom.GetOuterHTML` | `chromedp.OuterHTML` / `chromedp.Text` |
| `chromedp.FullScreenshot`, `chromedp.CaptureScreenshot` | `chromedp.Click` / `chromedp.SendKeys` |
| JS click / value-set + `input`/`change` dispatch | `chromedp.Screenshot(sel, ...)` |

Cause: obscura never emits `DOM.documentUpdated` / `DOM.setChildNodes`, so chromedp's internal node cache that all
its *query* selectors depend on is never populated (`DOM.getDocument` also returns root NodeID 0). Everything that
does not depend on that cache works.

## Phases

### P1 — Detection (`internal/browser`) — DONE
`obscura` candidate (ExecNames `obscura`/`obscura.exe`, 8 install paths, no profiles), `NormalizeBrowserType`
aliases, `IsRemoteBrowserType` switch covering lightpanda + obscura, new `internal/browser/detect_test.go`.

### P2 — Session launch (`internal/llm/tools/browser_session.go`) — DONE
`startLightpandaProcess` generalized to `startRemoteBrowserProcess(install)`; pure helper
`remoteBrowserServeArgs(browserType, host, port)` adds `--allow-private-network --allow-file-access --quiet` for
obscura and keeps lightpanda's argv unchanged; engine-neutral error strings using the install label.

### P3 — Selector/config surfaces — DONE
TUI and WebUI pick detected engines up through their existing merge loops; user-data-dir auto-fill is now skipped
for remote types (both in `buildInternalToolsSection` and in the `internalTools.browserType` apply path); WebUI
shows a hint for remote CDP-server engines; `cmd/init.go` and `internal/config/init.go` `BrowserType` comments now
list `lightpanda` and `obscura`.

### P5 — JS-driven tool fallbacks (`internal/llm/tools/browser_jsdriven.go`) — IN PROGRESS
`browserNeedsJSDriver(type)` (obscura only) + a `jsDriven` flag on `browserSession`. For JS-driven sessions the
tools swap chromedp query actions for `Runtime.Evaluate`-based equivalents: `jsWaitVisible`, `jsOuterHTML`,
`jsText`, `jsClick`, `jsFill` (native value setter + bubbling `input`/`change`), `jsElementScreenshot`
(`getBoundingClientRect` + `Page.captureScreenshot` clip). Selectors are JSON-encoded into the expressions. Wired
into `browser_navigate.go`, `browser_content.go`, `browser_interact.go`, `browser_screenshot.go`; every other
browser keeps the existing chromedp path untouched. Covered by unit tests plus a live obscura end-to-end test
against a local `httptest` server (this dev sandbox has no external network).

### P4 — Verification + documentation
- `go build ./...`, `go test ./internal/browser/ ./internal/llm/tools/ ./internal/design/` — green.
- Live end-to-end run of `browser_navigate` + `browser_get_content` + click/fill + screenshot through the real
  tool objects with `internalTools.browserType = "obscura"`.
- Change summaries in `pando/changes/obscura_browser_*.md`.

## Out of scope
- Making the design renderer (`internal/design`) use obscura — it needs Chromium's screenshot/PDF surface, and
  remote types stay rejected there.
- Exposing obscura's `--stealth`, `--proxy`, `--workers` as Pando config knobs (possible follow-up).
- Auto-installing obscura; upstreaming the missing `DOM.documentUpdated`/`setChildNodes` events to obscura.
