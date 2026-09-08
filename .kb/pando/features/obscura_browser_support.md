---
created_at: 2026-09-07T15:55:01.220361375Z
updated_at: 2026-09-07T15:55:01.220361375Z
tags:
    - feature
    - browser
    - obscura
    - cdp
    - internal-tools
---
# Feature: Obscura headless browser support (COMPLETE, 2026-09-07)

Implements [[obscura_browser_support_plan]]. Phase docs: [[obscura_browser_detection_p1]],
[[obscura_browser_remote_launch_p2]], [[obscura_browser_support_p3_selector_config]],
[[obscura_browser_jsdriven_p5]].

## What ships

Pando can now use [Obscura](https://github.com/h4ckf0r0day/obscura) (Rust headless browser, own CDP server) as the
engine behind the `browser_*` internal tools:

1. **Detection** — `internal/browser/detect.go`: `obscura` candidate (`obscura`/`obscura.exe`, 8 install paths, no
   profiles), `NormalizeBrowserType` aliases (`obscura`, `obscura-browser`), and `IsRemoteBrowserType` now a switch
   returning true for `lightpanda` **and** `obscura`.
2. **Launch** — `internal/llm/tools/browser_session.go`: `startLightpandaProcess` generalized into
   `startRemoteBrowserProcess(install)`; `remoteBrowserServeArgs` builds
   `serve --host 127.0.0.1 --port <free> --allow-private-network --allow-file-access --quiet` for obscura
   (lightpanda argv unchanged). Free port + `/json/version` readiness poll + `chromedp.NewRemoteAllocator` +
   kill-on-close, all reused. Errors now use the install label.
3. **Selection** — detected engines already merge into the TUI (`buildInternalToolsSection`) and WebUI
   (`InternalToolsSettings.tsx`) browser lists; user-data-dir auto-fill is skipped for remote types and the WebUI
   shows a "launched by Pando as a local CDP server" hint. `BrowserType` comments in `cmd/init.go` /
   `internal/config/init.go` list the two remote engines.
4. **JS-driven fallbacks** — `internal/llm/tools/browser_jsdriven.go`: obscura never emits
   `DOM.documentUpdated`/`DOM.setChildNodes`, so every chromedp *query* action (`WaitVisible`, `OuterHTML`, `Text`,
   `Click`, `SendKeys`, selector `Screenshot`) hangs until timeout. For sessions where
   `browserNeedsJSDriver(type)` is true, navigate/content/interact/screenshot swap in `Runtime.Evaluate`-based
   equivalents (`jsWaitVisible`, `jsOuterHTML`, `jsText`, `jsClick`, `jsFill` with the native value setter +
   bubbling `input`/`change`, `jsElementScreenshot` via `getBoundingClientRect` + capture clip). Selectors and
   values are JSON-encoded before splicing. All other browsers keep the existing chromedp path byte-for-byte.

## Usage

Install obscura on PATH, then set `internalTools.browserType = "obscura"` (TUI/WebUI settings or config).
Pando starts and stops the CDP server per browser session; no manual `obscura serve` needed.

## Verification

`go build ./...`, `go vet ./internal/llm/tools/`, `go test ./internal/browser/ ./internal/llm/tools/
./internal/design/` — green. `TestBrowserObscuraLiveEndToEnd` runs against the real installed obscura 0.2.2 and a
local `httptest` server (this sandbox has no external network), driving navigate → get_content (text+html) → click
(page mutation asserted) → fill (`input` event asserted) → full-page and element screenshots.

## Known limits

- The design renderer (`internal/design`) still rejects remote browser types — it needs Chromium's screenshot/PDF
  surface.
- Obscura's `--stealth`, `--proxy`, `--workers`, `--user-agent` are not exposed as Pando config knobs yet.
- The `--allow-private-network` / `--allow-file-access` flags are passed on purpose: the server binds loopback only
  and the agent already has local shell/file access, so the defaults would only break local dev-server and
  `file://` use cases.
