---
created_at: 2026-08-29T18:50:33.949040045Z
updated_at: 2026-08-29T18:50:33.949040045Z
tags:
    - change
    - desktop
    - uiauto
    - macos
    - axuielement
    - purego
---
# Phase 5 — macOS AXUIElement backend (`internal/uiauto/platform/darwin`)

Part of [[desktop_controller_uiauto_plan]]. Follows [[uiauto_core_phase0]], [[uiauto_tools_phase1]], [[uiauto_linux_atspi_phase2]], [[uiauto_input_screen_phase3]].

Date: 2026-08-29.

## What changed

New package `internal/uiauto/platform/darwin` (13 files, ~2139 lines incl. tests) implementing `core.Backend` for the macOS Accessibility API, plus the registration file `internal/uiauto/backends_darwin.go`.

Files: `ax_darwin.go` (the whole purego FFI surface: dlopen of ApplicationServices + CoreFoundation, symbol binding, CF bridging, `NewBackend()`), `backend.go` (the 9 `core.Backend` methods over an `axConn` interface), `conn.go` (the `axConn` abstraction that makes everything else testable off-macOS), `traverse.go` (selector-driven incremental descent), `actions.go`, `element.go`, `ref.go` (durable element key), `errors.go` (AXError -> `core.DesktopError`), plus tests including `fake_conn_test.go`.

## Why

macOS AXUIElement is the only way to read the semantic UI tree on the Mac; without it the agent would be reduced to screenshots + OCR on that platform.

## Key design points

- **No cgo, no Objective-C**: `github.com/ebitengine/purego` dlopens `/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices` and `.../CoreFoundation.framework/CoreFoundation`, following the conventions Phase 3 established in `input_darwin.go`/`screen_darwin.go`.
- **CF memory discipline** (the main leak risk): typed wrappers with explicit `Release()` and consistent `defer`; every object from a CF `Copy`/`Create` function is `CFRelease`d; the fixed attribute-name CFStrings are interned once at init and never released (process-lifetime constants).
- **Batched attribute fetching**: one `AXUIElementCopyMultipleAttributeValues` per node instead of a call per attribute — the macOS analogue of AT-SPI's `GetAll` batching and UIA's CacheRequest.
- **Traversal** mirrors Phase 2: descend only branches that can still satisfy the remaining selector steps, prune early on role, stop at `limit`, cap depth, honour `ctx` cancellation. No whole-tree build.
- **AXRole AND AXSubrole are both kept** in `Element.Native` — the plan's escape-hatch principle: `AXSearchField` has no cross-platform equivalent and must not be flattened into a generic role.
- **Trust**: `Available()` calls `AXIsProcessTrusted`; untrusted returns `PERM_DENIED` with a suggestion pointing at System Settings > Privacy & Security > Accessibility. No silent `kAXTrustedCheckOptionPrompt`.
- **AXError mapping**: -25202 invalid element -> `STALE_REF`; -25211 API disabled -> `PERM_DENIED`; -25205/-25206/-25208 unsupported -> `ACTION_FAILED` (so the Manager's `ActionResolver` falls back to Phase 3 physical input); -25212 no value, -25204 cannot complete mapped accordingly.
- **Testability**: all logic sits above an `axConn` interface, so 23 tests run on linux against a fake connection despite the FFI being macOS-only.

## Verification — COMPILE-VERIFIED ONLY

**This backend has never been run on real macOS.** Verified:
- `GOOS=darwin go build ./internal/uiauto/...` — clean
- `GOOS=darwin go vet ./internal/uiauto/...` — only the expected `possible misuse of unsafe.Pointer` at `ax_darwin.go:132` (dereferencing a dlsym'd CoreFoundation global such as `kCFBooleanTrue`), the same class of false positive Phase 3 documented in `screen_darwin.go`
- `go build ./...` and `go test ./internal/uiauto/...` on linux — pass, 23 tests in this package
- `gofmt -l internal/uiauto` — empty

Note: `GOOS=darwin go build ./...` over the whole repo fails for a pre-existing unrelated reason (`go-tree-sitter` is cgo-only, excludes all files on non-linux).

## Deferred

Real-hardware validation (especially CFRelease correctness under sustained use); AX notification subscriptions (Phase 7).

## Process note

Phases 4 and 5 were both interrupted mid-flight by an API spend limit. Their code and tests were essentially complete; the missing pieces — `internal/uiauto/backends_windows.go` (the "uia" registry registration), `gofmt` on 5 files, and these two KB summaries — were finished directly in the main session.
