---
created_at: 2026-08-31T09:46:53.518816218Z
updated_at: 2026-08-31T09:46:53.518816218Z
tags:
    - fix
    - desktop
    - uiauto
    - tests
    - atspi
    - x11
---
# Fix: desktop/uiauto environment-dependent test failures — 3 real bugs behind them (2026-08-31)

## Starting point

Five tests failed on a developer box with a display (they pass in headless CI, which is why
they reached `main`):

- `internal/llm/tools`: `TestDesktopClickAtToolPermissionGrantedThenFails`,
  `TestDesktopScreenshotToolPlatformNotSupportedWhenGranted`,
  `TestDesktopScreenshotToolGridParamDecodes`
- `internal/uiauto/screen`: `TestLiveX11Capture`
- `internal/uiauto/platform/linux`: `TestIntegrationListApplications`

None of them were "just flaky tests" — each sat on top of a genuine defect.

## Bug 1 — `DesktopBackend = "null"` did not stop OS access

`withDesktopTestConfig` documents the `"null"` backend as "deterministic, zero-OS-call", but
`Options.Backend` only pins the **accessibility-tree** backend (`resolveBackends`). Two side
channels bypass it entirely:

- `Manager.Screenshot` calls the package-level `captureScreen` (`screen.Capture`) directly.
- `Manager.physical` is built from `newPhysicalInput()` gated only on `AllowPhysicalInput`.

So a Manager configured as "null" still grabbed the real screen and could drive the real
mouse. The tests failed because on a box with a display they got `ACTION_FAILED` /
a successful physical click instead of `PLATFORM_NOT_SUPPORTED`.

**Fix**: new `Options.Inert`. When set, `NewManager` builds no physical-input layer, reports
no screen capability, and `Manager.Screenshot` returns `PLATFORM_NOT_SUPPORTED` up front
(`Manager.inert` field).

The policy deliberately lives at the **config→Options boundary**, not in `NewManager`:
`OptionsFromConfig` sets `Inert: backend == nullBackendName`. `"null"` has two distinct
meanings in this codebase and conflating them broke things — a first attempt derived
inertness from `pinned && osName == "null"` inside `NewManager` and immediately broke
`TestManagerClickAtValidatesCoordinates` and
`TestManagerClickAtSkipsValidationWhenDisplaysUnknown`, which pin `"null"` as a *neutral*
accessibility backend while injecting a **fake** physical input layer. In config, `"null"`
is the user saying "no real desktop automation"; in a direct `NewManager` call it is just a
backend choice. Also added the `nullBackendName` constant in `backends.go`.

## Bug 2 — X11 `Capabilities()` lied about screenshot support

`screen.Capabilities()` reported `Screenshot: true` whenever `xgb.NewConn()` succeeded. An X
server reached **without authority info** accepts the connection and then rejects `GetImage`
with `BadMatch`, so the Manager told the model it could take screenshots that always failed.

**Fix**: new `canGetImage(conn)` helper does a 1×1 `GetImage` on the root window — the
cheapest honest probe of the exact path `Capture` uses — and `Capabilities()` returns its
result. `TestLiveX11Capture` now gates on `Capabilities().Screenshot` instead of merely
`DISPLAY != ""`, so it stays meaningful where capture works (Xvfb CI, a real desktop) and
skips where the session cannot serve it.

## Bug 3 (the serious one) — AT-SPI connection lifetime bound to a per-operation context

`connectA11yBus(ctx)` dialed with `dbus.Connect(addr, dbus.WithContext(ctx))`. `WithContext`
binds the **connection's** lifetime to that context, but every caller passes a
per-operation context — and `AtspiBackend.ensureConn` **caches** the result in `b.conn` for
the whole process. The moment the first operation's deadline elapsed or its `cancel()` ran,
the cached connection was dead and every later AT-SPI call failed with:

```
read unix @->/run/user/1000/at-spi/bus_1: use of closed network connection
```

That is a production defect in the Linux desktop backend (first call works, all subsequent
ones fail), not a test artifact. The integration test merely exposed it because its guard
helper made one call under a `defer cancel()` scope before the test body made a second.

**Fix**: `dbus.Connect(addr, dbus.WithContext(context.WithoutCancel(ctx)))` — ctx still
bounds discovery and the dial, but the connection outlives the caller's scope. Also hardened
`skipUnlessA11yBusReachable` to make a real `listAppRefs` round trip before deciding the bus
is usable (connecting alone is not proof).

## Files touched

- `internal/uiauto/manager.go` — `Options.Inert`, `Manager.inert`, `Screenshot` guard,
  physical-input and screen-capability gating, `OptionsFromConfig`
- `internal/uiauto/backends.go` — `nullBackendName` constant
- `internal/uiauto/screen/screen_linux.go` — `canGetImage`, honest `Capabilities()`
- `internal/uiauto/screen/screen_linux_test.go` — gate on real capability
- `internal/uiauto/platform/linux/discovery.go` — `context.WithoutCancel` on the bus dial
- `internal/uiauto/platform/linux/backend_integration_test.go` — round-trip guard

## Verification

- `go test ./...` across the whole repository: **no failures** (previously 5).
- `TestIntegrationListApplications` now genuinely runs instead of failing, and enumerates 4
  live AT-SPI applications (xdg-desktop-portal-gtk, mailspring, evolution-alarm-notify,
  Microsoft Edge) — proof that bug 3 was the cause and not an unusable bus.
- `TestLiveX11Capture` skips with an explicit reason on this box (DISPLAY set, GetImage
  rejected) instead of failing.
- `go vet ./...` exit 0; cross-builds `GOOS=darwin` and `GOOS=windows` of
  `./internal/uiauto/...` OK.

Related: [[feature_desktop_controller_uiauto]], [[codeql-easy-wins-2026-08-31]].
