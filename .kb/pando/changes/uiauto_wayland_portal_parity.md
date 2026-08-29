---
created_at: 2026-08-30T03:03:03.958938909Z
updated_at: 2026-08-30T03:03:03.958938909Z
---
# Desktop Controller — Block W (Wayland parity) COMPLETE (2026-08-30)

Implements Block W (W1-W6) of [[pando/plans/desktop_controller_wayland_routing_plan.md]], the
Wayland half of the follow-up to [[pando/plans/desktop_controller_uiauto_plan.md]] (P0-P8
complete 2026-08-29, building on [[pando/changes/uiauto_input_screen_phase3.md]]). Block R
(backend routing, `internal/uiauto/manager.go`/`backends.go`/`internal/config`) is out of scope
here — owned by a parallel agent under the plan's file-ownership split, not touched.

## The core bug fixed (W1+W2)

`portalInput.MoveMouse` in `internal/uiauto/input/input_linux.go` called
`NotifyPointerMotionAbsolute(uint32(0), x, y)`. The first argument is a **ScreenCast stream
node id**, hardcoded to a nonexistent stream 0 — the XDG `RemoteDesktop` portal only accepts
absolute pointer motion against a real `ScreenCast` stream negotiated on the same session, and
no such stream ever existed. Fixed by negotiating ONE combined session (`RemoteDesktop.
CreateSession` → `RemoteDesktop.SelectDevices` + `ScreenCast.SelectSources` on the same
session handle → `Start`), parsing `Start`'s `streams` (`a(ua{sv})`, each a node id + position/
size), and resolving the target point against those streams before calling
`NotifyPointerMotionAbsolute(nodeID, x-streamX, y-streamY)`. When no stream covers the point (or
none was obtained at all), `MoveMouse`/`Click`/`Scroll` now return an `ACTION_FAILED`
`*core.DesktopError` explaining that Wayland absolute pointing needs screen-cast consent —
**no fallback to stream 0**. `Capabilities()` reports `Mouse: len(session.Streams) > 0`, `Keyboard:
true` (keyboard/relative notify events don't need a stream), so Manager never claims absolute
pointing works when it structurally cannot.

## New shared package: `internal/uiauto/portal`

Extracts what `input_linux.go` and `screen_linux.go` used to each duplicate (bus/object names,
handle-token generation, the `org.freedesktop.portal.Request` Response-signal wait, consent
timeout) into one package, which is what makes the single shared RemoteDesktop+ScreenCast
session possible at all.

- `portal.go` — `BusName`/`ObjectPath`/interface constants; `RequestCaller` interface
  (`Request`/`Call`/`Close`) abstracting the D-Bus surface, backed by `dbusCaller` in production
  and by fakes in every test (no real xdg-desktop-portal is reachable on this dev box: tty
  session, no `DISPLAY`/`WAYLAND_DISPLAY`, no portal running); `Dial()`; `ResponseCodeError(step,
  code)` — code 0 = success, code 1 = user cancelled → `PERM_DENIED`, any other non-zero → `ACTION_
  FAILED`; `Request(...)` — one Request-pattern call with the response-code mapping baked in, a
  transport-level error (no portal at all) → `PLATFORM_NOT_SUPPORTED`; `Current()`/`SetCurrent()`
  — a process-wide pointer to the most recently opened `*Session`, so `screen.Displays()` can
  report real stream geometry when input already negotiated a session, without forcing its own
  consent flow (W4).
- `session.go` — `Session` (`Handle()`, `Streams []Stream`, `StreamFor(x,y)`,
  `RemoteDesktopRestoreToken`/`ScreenCastRestoreToken`, `Close()`); `Stream{NodeID,X,Y,W,H}` +
  `Contains(x,y)`; `Open(ctx, caller, OpenOptions{WantScreenCast, RemoteDesktopRestoreToken,
  ScreenCastRestoreToken, ConsentTimeout})` — the CreateSession→SelectDevices(+SelectSources)→Start
  flow; `decodeStreams` decodes the `a(ua{sv})` wire shape (mirrors `storeSoRefSlice`'s
  reflection-based decode in `internal/uiauto/platform/linux/conn.go`, the in-repo D-Bus style
  reference named in the task).
- `store.go` — restore-token persistence (W3, see below).

### W3 — consent persistence

`SelectDevices`/`SelectSources` now pass `persist_mode = 2` (`PersistUntilRevoked`), and
`Session.Open` captures `restore_token` from the `Start` response. Tokens are persisted via
`internal/uiauto/portal/store.go`'s `SaveRestoreTokens`/`LoadRestoreTokens`, an atomic-write JSON
file at `<internal/config.GlobalConfigDir()>/uiauto_wayland_portal.json` — the SAME per-machine
location `internal/config/global_projects.go`'s `saveGlobalProjects` already uses for the global
projects registry ($XDG_CONFIG_HOME/pando or ~/.config/pando), found by searching the repo rather
than inventing a new location, mirroring its temp-file-then-rename atomicity pattern.
`portalInput.ensureSession` (input_linux.go) loads the stored tokens and passes them into
`portal.Open`; `Open` itself handles a rejected/expired token by retrying once with a fresh,
tokenless session (`Open`'s doc comment covers the exact retry condition) rather than a hard
failure — so a revoked token degrades to a normal consent prompt.

Deliberate deviation from the plan's literal wording ("pass it back as restore_token on the next
CreateSession"): the actual XDG portal spec puts `restore_token` in `SelectDevices`/
`SelectSources`' options dict, not `CreateSession` (which only takes `session_handle_token`). This
implementation follows the real protocol; flagged here since it reads as a correction rather than
a literal instruction-follow.

### W4 — screenshot portal hardening

`screen_linux.go`'s `capturePortal`/`decodePortalScreenshot` now reuse `portal.Request` (so user-
cancelled → `PERM_DENIED`, portal missing → `PLATFORM_NOT_SUPPORTED`, both via the shared mapping
rather than one generic error as before). New `decodeFileURI` robustly percent-decodes the
returned `file://` URI (`url.Parse`'s automatic path decoding for the standard form, with a manual
`url.PathUnescape` fallback for a non-standard authority-less `file:/path`); `decodePortalScreenshot`
now `defer os.Remove(path)`s the temp file the portal wrote, unconditionally (even on a PNG decode
failure) — the portal writes a real file into the user's filesystem per call and it must not be
left behind. `Displays()` on Wayland now reports real per-stream `DisplayInfo` (`Bounds` from
`Stream.X/Y/W/H`) when `portal.Current()` has an active session with streams (set by
`input_linux.go` after a successful `ensureSession`), falling back to the single synthetic
`"wayland-portal"` display only when no shared session exists — `Displays()` itself never forces a
consent dialog.

### W5 — AT-SPI coordinate space

Verified (not changed): `internal/uiauto/platform/linux/element.go`'s `Component.GetExtents` call
already passes `uint32(0)` = `ATSPI_COORD_TYPE_SCREEN` (0=SCREEN, 1=WINDOW, 2=PARENT), so AT-SPI
element bounds are in the same global/compositor pixel space as ScreenCast stream rectangles and
`PhysicalInput` calls — no translation needed between the accessibility tree and the physical-input
fallback's coordinates. Documented in place with a comment at that call site explaining why this
matters (a WINDOW/PARENT coord type would silently misplace every physical-fallback click on a
non-origin-positioned window) and how it relates to `portal.Stream.Contains`.

### W6 — tests

- `internal/uiauto/portal/session_test.go` — `Open` happy path with streams, no-ScreenCast (no
  streams), user-cancelled → `PERM_DENIED`, restore-token-rejected → fallback retry (asserts
  `CreateSession` called exactly twice), no-token failure → no retry (called exactly once),
  transport error → `PLATFORM_NOT_SUPPORTED`, missing `session_handle` → `PERM_DENIED`,
  `ResponseCodeError` table, `decodeStreams` (multi-entry + malformed), `Current`/`SetCurrent`.
- `internal/uiauto/portal/store_test.go` — restore-token round trip, clearing one token without
  disturbing the other, corrupt-store-file degrades to empty (never a hard error).
- `internal/uiauto/input/input_linux_wayland_test.go` — `portalInput.MoveMouse` maps a global
  point to `NotifyPointerMotionAbsolute(nodeID, x-streamX, y-streamY)` via a fake
  `portal.RequestCaller` driving the real `portal.Open`; a point outside every stream's rectangle
  returns `ACTION_FAILED` and asserts `NotifyPointerMotionAbsolute` was never called (the old
  stream=0 bug, made unreproducible); a session opened without ScreenCast has zero streams and
  `PressKey` still succeeds (keyboard doesn't need a stream) while `MoveMouse` still fails
  honestly.
- `internal/uiauto/screen/screen_linux_wayland_test.go` — `decodeFileURI` table (plain,
  percent-encoded space, percent-encoded Unicode, authority-less `file:/path`, non-file scheme,
  malformed); `decodePortalScreenshot` deletes the temp file on success AND on a PNG decode
  failure; `Displays()` falls back to the synthetic placeholder with no shared session and reports
  real stream `Bounds` once `portal.SetCurrent` registers one (built via `portal.Open` + a
  scripted fake caller, not a test-only constructor in production code).
- `internal/uiauto/platform/linux/element.go` — no new test needed; the coord-type verification is
  a read + comment, not a behavior change (existing `element_test.go` already exercises
  `GetExtents`).

Live-session tests (real X11/Wayland, real portal round trip) were not added beyond the existing
`t.Skip`-when-no-`DISPLAY` smoke tests already in the Phase 3 files — this box has neither
`DISPLAY` nor `WAYLAND_DISPLAY` and no `xdg-desktop-portal` running.

## Files touched

- New: `internal/uiauto/portal/portal.go`, `session.go`, `store.go`,
  `session_test.go`, `store_test.go`.
- Changed: `internal/uiauto/input/input_linux.go` (Wayland section rewritten on top of
  `internal/uiauto/portal`; X11 section untouched), new
  `internal/uiauto/input/input_linux_wayland_test.go`.
- Changed: `internal/uiauto/screen/screen_linux.go` (Wayland section rewritten on top of
  `internal/uiauto/portal`; X11 section untouched), new
  `internal/uiauto/screen/screen_linux_wayland_test.go`.
- Changed: `internal/uiauto/platform/linux/element.go` (comment only, additive per the task's file-
  ownership rule — no behavior change).
- NOT touched: `internal/uiauto/manager.go`, `backends*.go`, `internal/uiauto/core/backend.go`,
  `internal/config/**` (besides reading `GlobalConfigDir()`), `internal/llm/**` — all Block R
  territory.

`docs/desktop-controller.md` was NOT edited in this pass (the plan scopes that to Block W's
Wayland/Linux prerequisites + support-matrix rows only; deferred to avoid colliding with Block R's
concurrent edits to the same file — should be picked up as a fast follow once Block R lands, or
done now if Block R has already finished its edits there).

## Verification

- `go build ./...` — clean, whole repo.
- `go vet ./internal/uiauto/...` — clean (native/linux).
- `go test ./internal/uiauto/... ./internal/llm/tools/...` — all pass, including the new portal/
  input/screen Wayland unit tests and the pre-existing suites (unaffected).
- `GOOS=windows go build ./internal/uiauto/...` / `GOOS=darwin go build ./internal/uiauto/...` —
  both clean (the new `portal` package has no build tags and compiles everywhere; the linux-tagged
  input/screen files are excluded as before).
- `GOOS=windows go vet` / `GOOS=darwin go vet` scoped to `internal/uiauto/...` — each still flags
  exactly the same pre-existing "possible misuse of unsafe.Pointer" FFI-pattern warnings this
  change did not introduce (`platform/windows/comcall_windows.go`, `screen_darwin.go`,
  `platform/darwin/ax_darwin.go` — none of these files were touched by Block W).
- `gofmt -l internal/uiauto` — clean.
- No cgo introduced; the `portal` package imports only `internal/uiauto/core`,
  `internal/config`, and `github.com/godbus/dbus/v5` (already a module dependency since Phase 3).

## What could NOT be verified (honest limits)

This dev box is a tty session: `DISPLAY` and `WAYLAND_DISPLAY` are both unset and no
`xdg-desktop-portal` backend is running (same constraint recorded in
[[pando/changes/uiauto_input_screen_phase3.md]] for the original Phase 3 work). Consequently:

- The real `RemoteDesktop.CreateSession`/`SelectDevices`/`ScreenCast.SelectSources`/`Start` D-Bus
  round trip, the real consent dialog, and real `streams` payload shape from an actual compositor
  (GNOME Mutter, KDE KWin, wlroots-based, etc.) were never exercised — only the wire-shape decoding
  (`decodeStreams`) and state machine (`Open`) were tested, against fake `RequestCaller`s built
  from the documented XDG portal spec. Real compositors are known to vary in exactly which
  properties they include and how consistently `restore_token` is honoured; this implementation is
  best-effort against the spec, not against a live backend.
- The real `NotifyPointerMotionAbsolute`/`NotifyPointerButton`/`NotifyPointerAxis`/
  `NotifyKeyboardKeysym` calls were never sent to a live portal — only that the correct
  session/stream/coordinate arguments are constructed and passed to the (fake, in tests) caller.
  Whether a real compositor's stream node ids and position/size semantics exactly match this
  reading of the spec is unverified.
- The real `org.freedesktop.portal.Screenshot` interface's exact response shape (URI format,
  whether it is always `file://` vs. some backends returning something else) was not verified
  against a live portal; `decodeFileURI`'s test cases are synthetic.
- Restore-token persistence was verified as a pure JSON round trip on disk; whether a real
  compositor's restore_token semantics match this reading (which portal interface returns which
  token, whether combining RemoteDesktop+ScreenCast in one session yields one token or two) is
  unverified — this implementation stores both `RemoteDesktopRestoreToken` and
  `ScreenCastRestoreToken` in case they differ, per Start's response using `WantScreenCast` to pick
  which field is populated (best-effort; no real portal to confirm against).
- AT-SPI SCREEN-coordinate space matching ScreenCast stream space (W5) is a documentation/design
  argument grounded in both specs' text, not something runnable on this box (AT-SPI backend is
  smoke-verified with no live GUI app either, per Phase 2's existing honest verification note).

None of this was faked or claimed working; every Wayland code path this change touches degrades
honestly (`PLATFORM_NOT_SUPPORTED`/`PERM_DENIED`/`ACTION_FAILED` with an actionable message) when
run on this box, exactly like the rest of the input/screen Linux code did before this change.