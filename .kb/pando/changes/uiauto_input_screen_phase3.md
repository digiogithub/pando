---
created_at: 2026-08-29T13:36:54.667936397Z
updated_at: 2026-08-29T13:36:54.667936397Z
---
# Pando Desktop Controller — Phase 3 (input + screen capture) COMPLETE (2026-08-29)

Implements Phase 3 (P3) of [[pando/plans/desktop_controller_uiauto_plan.md]], building on
[[pando/changes/uiauto_core_phase0.md]] and [[pando/changes/uiauto_tools_phase1.md]]. Delivers
the cross-platform physical (synthetic) mouse/keyboard input layer and the screen capture layer,
plus wires both into the Manager so `core.ActionResolver`'s native-first/physical-fallback path
(Phase 0) becomes real and `Manager.Screenshot` (a `PLATFORM_NOT_SUPPORTED` stub since Phase 1)
actually captures. No cgo anywhere; RobotGo/kbinani-screenshot were rejected per the plan.

This phase ran in parallel with another agent's Linux AT-SPI backend
(`internal/uiauto/platform/linux`, `internal/uiauto/backends_linux.go`) under a strict
file-ownership split: this phase owns `internal/uiauto/input/`, `internal/uiauto/screen/`, and is
the only phase-3 editor of `internal/uiauto/manager.go`. Nothing under `internal/uiauto/platform/`,
`internal/uiauto/backends.go`, `internal/uiauto/backends_linux.go`, or `internal/uiauto/core/*` was
touched.

## New package: `internal/uiauto/input`

Implements `core.PhysicalInput` (`Click`, `MoveMouse`, `TypeText`, `PressKey`, `Scroll`) per
platform, selected by `//go:build` tags, each providing its own `New() (core.PhysicalInput, error)`
and `Capabilities() core.Capabilities`:

- `keys.go` (+ `keys_test.go`) — platform-independent `Modifier`/`Chord` types and `ParseChord`:
  case-insensitive `+`-separated chord parsing (`"ctrl+s"`, `"cmd+alt+shift+X"`), modifier aliases
  (ctrl/control, alt/option/opt, shift, cmd/command/meta/win/windows/super), named-key vocabulary
  (enter/return, tab, esc/escape, space, backspace, delete/del, arrows, home/end, pageup/pgup,
  pagedown/pgdn/pgdown, insert, capslock, f1-f12) via `namedKeys`, single-rune keys lowercased,
  `NamedKeys()` for introspection. All malformed input returns `INVALID_ARGS` `*core.DesktopError`.
- `input_windows.go` (`//go:build windows`) — raw `user32.dll` `SendInput`/`SetCursorPos`/
  `VkKeyScanW` via `syscall.NewLazyDLL` (no cgo, no `x/sys/windows` SendInput wrapper needed since
  none exists upstream). `rawInput` mirrors the Win32 `INPUT` tagged union as a `uint32` type tag +
  32-byte payload array populated via `unsafe.Pointer`, matching the real 40-byte `sizeof(INPUT)`
  on amd64. `TypeText` uses `KEYEVENTF_UNICODE` per UTF-16 code unit (arbitrary text, not just
  ASCII). `PressKey` maps named keys to VK codes (`namedVirtualKeys`) and single chars via
  `VkKeyScanW` (also detecting when Shift is needed), holding Ctrl/Alt/Shift/Win down around the
  key event for chords.
- `input_linux.go` (`//go:build linux`) — `sessionType()` detects X11 vs Wayland from
  `WAYLAND_DISPLAY`/`DISPLAY`/`XDG_SESSION_TYPE` (XWayland with both set prefers X11, since it
  needs no consent dialog); `"none"` (neither var set — this dev box) returns a `noSessionInput`
  that honestly reports `PLATFORM_NOT_SUPPORTED` from every call rather than hanging or faking.
  X11 path (`x11Input`) uses `github.com/jezek/xgb` + `xtest.FakeInput` for motion/button/key
  events; text/named-key entry uses `runeToKeysym` (standard Unicode-keysym convention: Latin-1
  codepoints are their own keysym, else `0x01000000|codepoint`) and a temporary remap of the
  highest keycode (`GetKeyboardMapping`/`ChangeKeyboardMapping`, restored after each key) — the
  same technique tools like `xdotool` use, since XTest only accepts keycodes. Modifier chords
  resolve real Control_L/Alt_L/Shift_L/Super_L keycodes by scanning the live keymap
  (`keycodeForKeysym`) instead of hardcoding layout-specific values. Wayland path (`portalInput`)
  drives the XDG desktop portal's `org.freedesktop.portal.RemoteDesktop` interface over
  `github.com/godbus/dbus/v5`: `CreateSession`→`SelectDevices`→`Start` (each a real D-Bus call
  watching the returned `Request` object's `Response` signal, bounded by a 30s
  `portalConsentTimeout`), then `NotifyPointerMotionAbsolute`/`NotifyPointerButton`/
  `NotifyPointerAxis`/`NotifyKeyboardKeysym`. `Start` requires interactive user consent; any portal
  failure/denial/timeout surfaces as `PERM_DENIED`/`TIMEOUT`, never a silent no-op.
- `input_darwin.go` (`//go:build darwin`) — CoreGraphics `CGEvent*` via `github.com/ebitengine/
  purego` `Dlopen`/`RegisterLibFunc` against `/System/Library/Frameworks/CoreGraphics.framework/
  CoreGraphics` (no cgo, no Objective-C). `CGEventSourceCreate` per call, `CGEventCreateMouseEvent`/
  `CGEventCreateKeyboardEvent`/`CGEventCreateScrollWheelEvent`, `CGEventKeyboardSetUnicodeString`
  for arbitrary-text typing, `CGEventSetFlags` for Ctrl/Alt/Shift/Cmd chords (via
  `namedVirtualKeys`/`letterAndDigitVirtualKeys`, standard Carbon/HIToolbox US-ANSI virtual key
  codes), `CGWarpMouseCursorPosition` for mouse move. Every `CGEventRef`/`CGEventSourceRef` is
  `CFRelease`d immediately after `CGEventPost` (`postAndRelease`) or when the enclosing
  `withSource` closure returns, so nothing leaks. `AXIsProcessTrusted` (ApplicationServices) gates
  every call behind a `PERM_DENIED` pointing at System Settings > Privacy & Security >
  Accessibility when the process isn't trusted.
- `input_other.go` (`//go:build !windows && !linux && !darwin`) — stub, every call
  `PLATFORM_NOT_SUPPORTED`.
- Linux honesty test `input_linux_test.go`: asserts `sessionType()=="none"`,
  `Capabilities()=={}`, and `Click` returns `PLATFORM_NOT_SUPPORTED` when `DISPLAY`/
  `WAYLAND_DISPLAY` are both unset (true in this tty dev environment); a second test
  (`TestLiveX11Input`) is a real-X11 smoke test that `t.Skip`s here and would run under a live
  X server/Xvfb.

## New package: `internal/uiauto/screen`

`screen.go` defines `Target{Display int; Region *core.Bounds; WindowID string}` and
`DisplayInfo{Index,Name,Bounds,Primary}`; each platform file provides `Capture(ctx, Target)
(image.Image, error)`, `Displays() ([]DisplayInfo, error)`, `Capabilities() core.Capabilities`:

- `screen_windows.go` — GDI `GetDC`/`CreateCompatibleDC`/`CreateCompatibleBitmap`/`BitBlt`/
  `GetDIBits` via `syscall.NewLazyDLL` against user32/gdi32 (top-down 32bpp DIB, BGRX→RGBA
  conversion); `Displays()` via `EnumDisplayMonitors`/`GetMonitorInfoW` (a `syscall.NewCallback`
  enumeration proc) for real multi-monitor bounds + primary flag.
- `screen_linux.go` — same `sessionType()` detection as the input package. X11: root-window
  (or, when `Target.WindowID` parses as a numeric X11 window id, that window's) `GetImage` request
  with `ImageFormatZPixmap`, decoded assuming little-endian 24/32bpp BGRX (`decodeZPixmap`,
  bytes-per-pixel derived from the reply size); `Displays()` uses `xinerama.QueryScreens` when the
  extension is present, falling back to a single default-screen `DisplayInfo`. Wayland: XDG portal
  `org.freedesktop.portal.Screenshot` over godbus (same Request/Response signal-watching pattern as
  the input package's portal, 30s timeout), decoding the returned `file://` PNG URI and cropping to
  `Target.Region` when set; `portalAvailable()` does a cheap `GetNameOwner` D-Bus check for
  `Capabilities()` without opening a session.
- `screen_darwin.go` — `CGDisplayCreateImage`/`CGGetActiveDisplayList`/`CGMainDisplayID`/
  `CGDisplayBounds` via purego, converting the returned `CGImageRef`'s `CGDataProviderCopyData`
  bytes (assumed little-endian BGRA, the common byte order for this API) into a Go `*image.RGBA`;
  a `Target.Region` crops via `SubImage`. `CGImageRef`/`CGDataProviderRef`/`CFDataRef` are each
  `CFRelease`d once their bytes are copied out. A `CGDisplayCreateImage` failure (commonly: Screen
  Recording permission not granted) surfaces as `PERM_DENIED`.
- `screen_other.go` — stub, every call `PLATFORM_NOT_SUPPORTED`.
- Linux honesty test `screen_linux_test.go`: same no-session assertions as the input package
  (`sessionType()=="none"`, `Capabilities().Screenshot==false`, `Capture` →
  `PLATFORM_NOT_SUPPORTED`), plus a `t.Skip`-when-no-`DISPLAY` live smoke test.

## Manager wiring (`internal/uiauto/manager.go`)

- `NewManager` now constructs the platform `PhysicalInput` via `input.New()`, gated on
  `Options.AllowPhysicalInput`, and passes it into `core.NewActionResolver` — the native-action-
  first/physical-fallback path in `core/action.go` (Phase 0, previously always a no-op since
  `Physical` was `nil`) is now real. A construction failure degrades to no physical fallback rather
  than failing `NewManager`, mirroring the existing backend-resolution fallback.
- `Manager.Capabilities()` now ORs in what `input.Capabilities()` (Mouse/Keyboard, only when
  `AllowPhysicalInput`) and `screen.Capabilities()` (Screenshot) can genuinely deliver, on top of
  the backend's own reported capabilities — never claiming a capability none of the three can
  actually provide.
- `Manager.Screenshot` is implemented for real: `parseScreenshotTarget` turns the tool's `target`
  string into a `screen.Target` (`""`/`"screen"` → whole display; `"window:<id>"` → `WindowID`;
  a qualified element ref `"@sXXXX:eYYYY"` → resolves the ref via the SnapshotStore, enforces app
  policy, and captures a `Region` matching its `Bounds`), calls `screen.Capture`, applies
  `Options.ScreenshotScale` via `github.com/disintegration/imaging` (Lanczos resize) when not 1.0,
  encodes PNG, and passes the bytes through `internal/imageopt.Normalize` exactly like
  `browser_screenshot.go`/`image_crop.go`, returning `(bytes, "image/png", error)`.
- Four new package-level function-variable seams (`newPhysicalInput`, `physicalCapabilities`,
  `captureScreen`, `screenCapabilities`) let tests substitute fakes for `input`/`screen` without a
  live display — used by the new Manager tests below. `internal/llm/tools/desktop_screenshot.go`
  needed **no changes**: its `mgr.Screenshot(ctx, target)` call signature was already exactly what
  Phase 1 defined.

## Dependencies

Added `github.com/jezek/xgb v1.3.1` and `github.com/ebitengine/purego v0.10.2` to `go.mod`
(latest stable tags at the time; `go mod tidy` ran clean, both modules resolved from the public
proxy without issue — no missing/unreachable module).

## Tests

`internal/uiauto/input/keys_test.go` (chord/named-key table tests, already listed above);
`internal/uiauto/input/input_linux_test.go` and `internal/uiauto/screen/screen_linux_test.go`
(no-display-session honesty + `t.Skip`-gated live smoke tests, already listed above);
`internal/uiauto/manager_test.go` gained:
- `TestManagerActionResolverPhysicalFallback` — a fake backend whose `Perform` always fails
  `ACTION_FAILED`, wired through the real `NewManager` (with `newPhysicalInput`/
  `physicalCapabilities` faked to a recording `fakePhysical`) — `mgr.Click` returns
  `Method:"physical"` and the fake recorded exactly one click at the element's bounds center,
  proving Phase 3's wiring (not just Phase 0's resolver logic) works end to end.
- `TestNewManagerMergesCapabilities` (two subtests) — verifies the OR-merge: physical-input
  Mouse/Keyboard only counted when `AllowPhysicalInput=true`, Screenshot always taken from the
  screen capturer regardless, backend capabilities preserved, nothing claimed that no provider
  offers.
- `TestManagerScreenshotEncodesPNG`, `...AppliesScreenshotScale`, `...CropsToElementBounds`,
  `...WindowTarget`, `...InvalidTarget` — a fake `captureScreen` returning a synthetic
  `image.RGBA` (`syntheticImage` helper), asserting real PNG-decodable output, correct
  `ScreenshotScale` resizing (100x200 @ 0.5 → 50x100), correct `Region` derived from an element's
  `Bounds` via `Find`, `WindowID` passthrough for `"window:<id>"`, and `INVALID_ARGS` for an
  unrecognized target string.
- `TestManagerScreenshotPlatformNotSupportedWhenScreenCapturerFails` replaces Phase 1's
  `TestManagerScreenshotAlwaysPlatformNotSupported` (screenshots are no longer always
  unsupported) with an explicit fake-capturer-returns-`PLATFORM_NOT_SUPPORTED` case, keeping the
  assertion deterministic regardless of the host's display environment.

## Verification

- `go build ./...` (native, linux) — clean, whole repo.
- `GOOS=windows go build ./...` / `GOOS=darwin go build ./...` — both fail only on a **pre-existing,
  unrelated** issue: `github.com/madeindigio/go-tree-sitter` (pulled in by
  `internal/rag/treesitter`, nothing to do with this change) excludes its Go files under
  non-cgo/non-linux build constraints. Confirmed pre-existing: that dependency's version is
  untouched in this change's `go.mod` diff. `GOOS=windows`/`GOOS=darwin go build` scoped to this
  change's actual surface (`./internal/uiauto/...`, `./internal/llm/tools/...`,
  `./internal/llm/agent/...`, `./internal/api/...`, `./internal/config/...`) is clean on both.
  `GOOS=linux go build ./...` — clean.
- `go vet ./internal/uiauto/...` — clean (native). A `GOOS=darwin` cross-vet flags one
  uintptr→unsafe.Pointer conversion in `screen_darwin.go` (`decodeZPixmap`'s macOS analog,
  `imageFromDisplay`'s `raw := unsafe.Slice(...)`) as "possible misuse of unsafe.Pointer" — this is
  the standard, unavoidable pattern for purego/cgo-free FFI pointer returns and is documented
  in-place with a comment; it does not affect the required native `go vet` run.
- `go test ./internal/uiauto/... ./internal/llm/tools/...` — all pass, including the parallel
  agent's `internal/uiauto/platform/linux` package.
- `gofmt -l internal/uiauto internal/llm/tools` — clean for every file this change touched or
  added (three pre-existing, untouched `internal/llm/tools` files were already gofmt-dirty before
  this change, unrelated).
- No cgo introduced; verified no `import "C"` under `internal/uiauto/input` or
  `internal/uiauto/screen`.

## Deferred / honestly-unverifiable (out of Phase 3 scope or environment-limited)

- This box is a tty session (`DISPLAY`/`WAYLAND_DISPLAY` both unset, confirmed via `env`) — the
  X11 and Wayland-portal paths in both packages are structurally complete and exercised by the
  honesty tests, but their live behavior (real `XTestFakeInput`/`GetImage` calls, real portal
  consent dialogs) is **unverified in this environment**; they are cross-compile-verified only,
  same caveat the plan calls out for Windows/macOS.
- The Wayland `RemoteDesktop`/`Screenshot` portal implementations are best-effort against the
  documented D-Bus API; they have never been exercised against a live `xdg-desktop-portal`
  backend (none is running here) and may need adjustment (e.g. exact `NotifyPointerAxis` unit
  semantics, portal method argument quirks across backend implementations) once tested against a
  real compositor.
- `screen_windows.go`/`screen_darwin.go` window-scoped (`Target.WindowID`) capture is not
  implemented as a true per-window capture (Windows/macOS both fall back to whole-screen);
  `Manager.parseScreenshotTarget` does not additionally crop a `"window:<id>"` target to a known
  window `Bounds` on those platforms (X11 is the only backend that captures the actual window
  drawable). Flagged in code comments; picking this up would need the window's `Bounds` threaded
  through from `Manager.Windows`/AT-SPI's `WindowInfo`, which is a small follow-up.
- macOS `Capabilities()` for screen capture cannot cheaply probe the Screen Recording privacy
  permission (unlike Accessibility's `AXIsProcessTrusted`); it optimistically reports `true` once
  CoreGraphics loads and lets an actual `Capture` call surface `PERM_DENIED` if the OS returns an
  empty image — documented in code.