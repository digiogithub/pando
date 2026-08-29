---
created_at: 2026-08-30T02:50:55.738416689Z
updated_at: 2026-08-30T02:50:55.738416689Z
tags:
    - plan
    - desktop
    - uiauto
    - wayland
    - portal
    - routing
---
# Desktop Controller — follow-up plan: Wayland parity + backend routing

Follows [[desktop_controller_uiauto_plan]] (phases P0-P8 COMPLETE 2026-08-29). Date: 2026-08-30.
Related: [[uiauto_input_screen_phase3]], [[uiauto_cdp_browser_phase6]], [[uiauto_cdp_browser_phase6_followup]], [[uiauto_exposure_docs_phase8]].

## Why this plan exists

Three gaps found while reviewing the finished feature:

1. **Wayland absolute pointing is broken.** `input_linux.go`'s `portalInput.MoveMouse` calls `NotifyPointerMotionAbsolute(uint32(0), x, y)`. That first argument is a **ScreenCast stream node id**, hardcoded to 0. The XDG `RemoteDesktop` portal only accepts absolute motion against a stream from an associated `ScreenCast` session; with no such session there is no coordinate space to address, so the call is rejected or silently mispositions. Only relative `NotifyPointerMotion` is valid without a stream.
2. **Wayland re-prompts for consent every session.** `SelectDevices` passes no `persist_mode` and no `restore_token`, so every new Manager asks the user to approve remote control again. Unusable for an agent in practice.
3. **The `cdp` backend is unreachable under `auto`.** `core.Registry.Resolve("auto")` picks ONE backend globally — the first that constructs — with order `atspi, uia, ax, cdp, null`. On any Linux box with a live a11y bus, `atspi` wins and `cdp` is never tried. The original analysis's routing promise ("browser window -> CDP, native app -> OS accessibility API") was never implemented: CDP is reachable only by pinning `DesktopBackend="cdp"` by hand.

Plus one incidental defect recorded in Phase 8 and still unfixed: a `.pando.toml` `[InternalTools]` table that sets only `DesktopEnabled` can lose the `DesktopAllowPhysicalInput` default (pre-existing `internal/config` merge behaviour).

## Block W — Wayland parity

Owner files: `internal/uiauto/input/input_linux.go`, `internal/uiauto/screen/screen_linux.go`, plus a NEW shared package `internal/uiauto/portal/` (both input and screen currently duplicate the portal constants and the `Request`-signal dance — that duplication is what makes a shared session impossible).

Note up front: Wayland deliberately offers LESS than X11 here, and the goal is honest degradation, never a faked capability.

### W1 — Shared portal session (`internal/uiauto/portal`)
- Extract the duplicated portal plumbing (bus names, `org.freedesktop.portal.Request` response handling, handle-token generation, timeouts) into one package used by both input and screen.
- One `Session` object that performs `RemoteDesktop.CreateSession` **once**, then `SelectDevices` (keyboard+pointer) AND `ScreenCast.SelectSources` **on the same session handle**, then `Start`.
- Parse `Start`'s `streams` result (`a(ua{sv})`): each entry carries a **node id** plus `position` and `size`. Keep them as the coordinate space.

### W2 — Correct absolute pointing
- `MoveMouse`/`Click` map a global `(x,y)` to the owning stream: find the stream whose `position`+`size` rectangle contains the point, then call `NotifyPointerMotionAbsolute(<that node id>, x - streamX, y - streamY)`.
- When no ScreenCast stream could be obtained (compositor refused, portal too old), do NOT silently fall back to `stream=0`. Report `Mouse` absolute positioning as unavailable in `Capabilities` and return a `core.DesktopError` explaining that Wayland absolute pointing needs screen-cast consent. Relative motion and key/button events may still be offered if they work.

### W3 — Consent persistence
- Pass `persist_mode = 2` (persist until revoked) to `SelectDevices`/`SelectSources`, capture the `restore_token` from the `Start` response, and persist it in Pando's existing state/config directory (find how other Pando components store per-machine state — do not invent a new location).
- Feed the stored token back as `restore_token` on the next `CreateSession` so the user consents once, not once per run. Handle a rejected/expired token by falling back to a fresh consent prompt.

### W4 — Screenshot portal hardening
- Reuse the shared `portal.Session` machinery. Handle the `Request` response code properly: user cancelled -> `PERM_DENIED` (not a generic failure), portal missing -> `PLATFORM_NOT_SUPPORTED`.
- Decode the returned `file://` URI robustly (percent-decoding), read it, and **delete the temp file** afterwards — the portal writes real files into the user's filesystem.
- Report display geometry from the ScreenCast streams when a session exists, instead of the current single synthetic `wayland-portal` display.

### W5 — AT-SPI under Wayland
- `Component.GetExtents` takes a coord-type argument; verify the backend requests screen coordinates and document how those relate to the ScreenCast stream space, since that mapping is what makes the physical-input fallback land in the right place.
- Report `Capabilities` per session type honestly (a11y works on Wayland; global input/capture depend on portal consent).

### W6 — Tests + docs
- Unit tests with a fake D-Bus for: stream selection/coordinate mapping, restore-token round-trip, portal response-code -> `DesktopError` mapping, URI decoding + temp-file cleanup.
- Live tests must `t.Skip` off Wayland (the dev box is a tty session).
- Update `docs/desktop-controller.md`'s support matrix and the Wayland prerequisites section.

## Block R — Backend routing and the config default

Owner files: `internal/uiauto/manager.go`, `internal/uiauto/backends.go`, `internal/uiauto/core/backend.go`, `internal/config/config.go`.

### R1 — Per-scope backend routing
- The Manager should hold the set of **available** backends rather than a single resolved one, and pick per operation: a scope naming a browser window/app served by a live CDP session goes to `cdp`; everything else goes to the OS backend.
- `auto` must keep meaning "pick sensibly", so `desktop_apps`/`desktop_observe` should surface browser pages through CDP while native apps come from AT-SPI/UIA/AX — without ever launching a browser (Phase 6's `Available` contract already guarantees CDP stays inert with no session registered).
- Element refs must record which backend produced them so a later `desktop_click` on that ref routes back to the same one.

### R2 — Overlap guidance for the model
- `browser_*` (CSS/DOM) and `desktop_*` (AX roles) now both act on web pages. Nothing tells the model which to use. Add explicit "when to use this instead of X" guidance to both tool description sets, and document the distinction in `docs/desktop-controller.md`: `browser_*` for scripted page automation, `desktop_*` for treating the browser as one app among many on the desktop.

### R3 — Config default defect
- Fix the `[InternalTools]` partial-table merge so setting only `DesktopEnabled` does not drop `DesktopAllowPhysicalInput` (and check whether the same merge behaviour bites other defaults — this is pre-existing and not desktop-specific, so fix it at the merge level with a regression test).

## Verification bar

`go build ./...`, `go test ./...` green; `GOOS=windows|darwin go build ./internal/uiauto/...` clean (whole-repo cross-build failure on `go-tree-sitter` is pre-existing and unrelated); `gofmt` clean for touched files. No cgo. No faked capability: anything unverifiable on the dev box must be reported as such.
