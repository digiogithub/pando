---
created_at: 2026-08-29T19:17:55.516506547Z
updated_at: 2026-08-29T19:17:55.516506547Z
tags:
    - change
    - desktop
    - uiauto
    - cdp
    - browser
    - fix
---
# Phase 6 follow-up — CDP backend made to actually work against real Chrome

Supersedes the "blocked/skipped" conclusion in [[uiauto_cdp_browser_phase6]]. Part of [[desktop_controller_uiauto_plan]].

Date: 2026-08-29.

## Context

Phase 6 landed the CDP backend (`internal/uiauto/platform/browser`) but concluded its live path was blocked by an upstream gap: the vendored `cdproto` (2025-07) did not know Chrome's `AXNode.ignoredReasons` value `"uninteresting"`, so cdproto's generated `PropertyName` decoder rejected the whole `Accessibility.getChildAXNodes` reply. Its integration test therefore skipped. That diagnosis was correct but the conclusion was not final — the gap was simply a stale dependency.

## What changed

1. **Dependency bump** (`go.mod`/`go.sum`): `github.com/chromedp/cdproto` 2025-07-24 -> 2026-08-04 (which defines `PropertyNameUninteresting`) and, because the newer cdproto changes `css.GetComputedStyleForNode`'s arity, `github.com/chromedp/chromedp` v0.14.2 -> v0.16.0. Pulls `golang.org/x/sys` 0.46.0 -> 0.47.0 and a newer `go-json-experiment/json`.

   With that, the integration test stopped skipping and started actually running — which immediately exposed two real bugs in the backend that no fake-conn unit test could have caught.

2. **Fix: duplicate `Find` results** (`traverse.go`, `backend.go`). A one-button page returned the same button twice (identical `axNodeId`/`backendDOMNodeId`). Root cause is normal Chrome behaviour, not a race: `Accessibility.getChildAXNodes` on the root returns BOTH the ignored wrapper node AND the unignored descendants that wrapper flattens to, so a plain DFS reaches the same subtree by two paths. Fixed with a `visited map[accessibility.NodeID]bool` threaded through `findRec`, shared across all scope roots of one `Find` — the general guard, since an AX graph may legitimately present a node via several paths.

3. **Fix: `Perform(invoke)` failed with "describeNode returned no node"** (`conn.go`). `DOM.describeNode` returns a node whose `NodeID` is `0` until the DOM agent has pushed that node to the frontend. Replaced `resolveNodeID` with `DOM.getDocument` + `DOM.pushNodesByBackendIdsToFrontend`, the canonical mapping for ids that came from the accessibility tree rather than from a prior DOM query.

4. **Fix: click then hung until the 20s timeout** (`conn.go`). `chromedp.Click(..., ByNodeID)` runs chromedp's own wait-for-node-ready machinery, which expects a node id produced by a chromedp selector query and simply blocks on one that came from the AX tree. Replaced with `DOM.resolveNode` + `Runtime.callFunctionOn` invoking the element's own `this.click()`, with `Runtime.releaseObject` in a defer. `SetValue` got the same treatment and now also dispatches bubbling `input`/`change` events so framework-bound inputs observe the edit. This is also the more semantic action, matching the plan's "native action before synthetic mouse input" principle.

## Verification

- `go test ./internal/uiauto/platform/browser/` — **passes, including `TestIntegrationLiveChromeFindAndClick` against a real headless Chrome on this machine**: navigate, Apps, Windows, selector-driven `Find` (now exactly one match), `Perform(invoke)` click, and `SetValue` on a text input. This is the first uiauto platform backend with a genuine end-to-end live verification.
- `go build ./...`, `go test ./...` repo-wide — clean; specifically `./internal/llm/tools`, `./internal/llm/agent`, `./internal/api`, `./internal/config` pass with the bumped chromedp, so the existing `browser_*` tools are unaffected by the upgrade.
- `GOOS=windows|darwin go build ./internal/uiauto/...` — clean.
- `gofmt -l internal/uiauto internal/llm/tools` — clean for every file this work touched (three pre-existing unformatted files elsewhere in `internal/llm/tools` were left alone).

## Note

Phases 4 and 5 remain compile-verified only. Phase 2 (AT-SPI) is smoke-verified against a live a11y bus but with no GUI apps registered. Phase 6 is now the only fully live-verified backend.
