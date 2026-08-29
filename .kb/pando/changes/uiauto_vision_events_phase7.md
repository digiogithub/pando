---
created_at: 2026-08-29T19:39:10.980006173Z
updated_at: 2026-08-29T19:39:10.980006173Z
---
# Pando Desktop Controller — Phase 7 (vision fallback + events) COMPLETE (2026-08-29)

Implements Phase 7 (P7) of [[pando/plans/desktop_controller_uiauto_plan.md]], building on
Phases 0-6 ([[pando/changes/uiauto_core_phase0.md]], [[pando/changes/uiauto_tools_phase1.md]],
[[pando/changes/uiauto_linux_atspi_phase2.md]], [[pando/changes/uiauto_input_screen_phase3.md]],
[[pando/changes/uiauto_windows_uia_phase4.md]], [[pando/changes/uiauto_macos_ax_phase5.md]],
[[pando/changes/uiauto_cdp_browser_phase6.md]], [[pando/changes/uiauto_cdp_browser_phase6_followup.md]]).
Two independent features: (A) a coordinate-based vision fallback, and (B) real accessibility
event subscriptions replacing `desktop_wait`'s polling, with an honest per-backend fallback.

## A. Vision fallback

New package `internal/uiauto/vision/` (no OS calls, pure `image`/stdlib + `golang.org/x/image`):

- `vision.go` — package doc explaining the design: the MODEL is the vision engine (no OCR/image
  analysis added); this package only makes the screenshot -> model -> coordinates -> physical
  input loop safe and explicit.
- `validate.go` — `ValidateCoordinates(x, y int, bounds []core.Bounds) error`: INVALID_ARGS when
  (x,y) falls outside every given rectangle; an empty `bounds` slice (bounds could not be
  determined) skips validation rather than blocking every coordinate action.
- `grid.go` — `DrawGrid(img image.Image, opts GridOptions) image.Image`: overlays a light
  coordinate grid (default 100px step, configurable) plus real-pixel axis labels (via
  `golang.org/x/image/font/basicfont`), non-mutating, same bounds as input.

Manager wiring (`internal/uiauto/manager.go`, still zero cgo):

- `listDisplays = screen.Displays` — new test-seam var alongside the existing
  `captureScreen`/`screenCapabilities` ones.
- `Manager.SemanticAvailable() bool` — `Capabilities().Accessibility && Capabilities().UIActions`;
  the "did the semantic path already fail" helper the tools use to nudge the model toward
  desktop_find/desktop_observe first, without forcing an actual Find call on every vision
  invocation.
- `Manager.validateCoordinates(x, y int) error` — calls `listDisplays()`, converts
  `[]screen.DisplayInfo` to `[]core.Bounds`, delegates to `vision.ValidateCoordinates`; skips
  validation when displays cannot be enumerated at all.
- `Manager.ClickAt(ctx, x, y int) (*core.ActionResult, error)` — the coordinate action path:
  bypasses `core.ActionResolver`/the backend entirely (no "native" method exists for a coordinate),
  gated on `Options.AllowPhysicalInput` (POLICY_DENIED otherwise), requires a resolved
  `resolver.Physical` (PLATFORM_NOT_SUPPORTED otherwise), validates coordinates, then calls
  `Physical.Click(x,y)` directly (ACTION_FAILED on failure). Returns `Method:"physical"`; the
  caller is responsible for marking the tool response `source:"vision"`.
- `Manager.Screenshot` signature changed to `Screenshot(ctx, target string, grid bool)`: when
  `grid` is true, `vision.DrawGrid` is applied to the captured image *before* `ScreenshotScale`
  resizing, so grid labels always read real, unscaled screen pixel coordinates — the coordinates
  `desktop_click_at` expects. All call sites (`internal/llm/tools/desktop_screenshot.go`,
  `manager_test.go`) updated.

New tool `internal/llm/tools/desktop_click_at.go` (`desktop_click_at`, registered in
`builtin_names.go` and both `CoderAgentTools`/`CoderAgentToolsWithMesnada` in
`internal/llm/agent/tools.go`, alongside the existing 11 — 12 desktop_* tools total now):
params `x`/`y` (required ints); always prompts `permission.Service` with a description that
states plainly "Blind coordinate click ... this is the vision fallback"; on success responds
`{ok:true, method:"physical", source:"vision", notes:[...]}`; description text documents "try
desktop_find/desktop_observe first" and describes the desktop_screenshot(grid:true) -> estimate
-> desktop_click_at workflow. `desktop_screenshot.go` gained an optional `grid` bool param
(default false, off to keep plain screenshots uncluttered) wired straight to
`Manager.Screenshot`'s new parameter.

## B. Events (replacing `desktop_wait` polling)

New package `internal/uiauto/events/` (backend-agnostic, imports only `core`):

- `events.go` — `Kind` vocabulary (`created`, `destroyed`, `propertychanged`, `focuschanged`,
  `valuechanged`) and `Event{Kind, ElementRef, AppID, WindowID, Timestamp, Details}`.
- `subscriber.go` — `Subscriber` interface: `Subscribe(ctx, scope) (<-chan Event, func(), error)`.
  A platform backend implements it *optionally*, in addition to `core.Backend`; detected purely by
  Go type assertion (`backend.(events.Subscriber)`), so `core.Backend`/`core.Capabilities` needed
  zero structural changes.
- `bus.go` — `EventBus`: `Subscribe(buffer)`/`Publish(ev)`/`Close()`, one upstream source fanned
  out to N independent buffered-channel waiters; a full subscriber buffer drops the event for that
  waiter rather than blocking the publisher (deliberate best-effort design — `WaitFor` always
  re-checks the real condition, so a dropped event only costs a slightly later re-check).
- `wait.go` — `WaitFor(ctx, backend, sub, locator, cond, timeout)`: always checks immediately
  first; with `sub == nil` (or `Subscribe` itself failing) falls back to the existing
  `core.WaitFor` polling loop verbatim; with a live subscription, blocks on the event channel
  **plus** a 2s fallback ticker (covers a change the backend doesn't model as one of the 5 Kinds,
  or a dropped event) and re-evaluates `backend.Find` (never trusts event content) on every wakeup.
  `evaluate()` reimplements `core.Locator`'s (unexported) evaluate semantics using only exported
  API (`backend.Find` + `l.Scope`/`l.Selector`), so `core/locator.go` needed **zero changes**
  (kept out of this phase's file-ownership list on purpose).

`internal/uiauto/manager.go`'s `Wait` method now type-asserts `m.backend.(events.Subscriber)` and
calls `events.WaitFor` instead of `core.WaitFor` directly; `desktop_wait.go` itself needed no
changes (same `Manager.Wait` signature).

### Per-backend event support (the honest part)

| Backend | `Capabilities.Events` | Implementation | Verification |
|---|---|---|---|
| **Linux AT-SPI** (`platform/linux/events.go`) | `true` when the a11y bus is reachable | Real `org.a11y.atspi.Event.Object` D-Bus signal match (`AddMatchSignal`/`Conn.Signal`), one lazy listener goroutine per `AtspiBackend` shared via an internal `EventBus` (so N concurrent `Wait` callers share one D-Bus match rule) | Unit-tested (`decodeAtspiEvent`/`atspiMemberKind` table tests against synthetic `dbus.Signal`s); `TestIntegrationAtspiSubscribeLiveBus` reuses the existing `skipUnlessA11yBusReachable` guard — `t.Skip`s on this tty session (no DISPLAY/WAYLAND_DISPLAY), same honesty bar Phase 2 set |
| **Browser/CDP** (`platform/browser/events.go`) | `true` whenever a session is registered+reachable | Real CDP `DOM`+`Accessibility` domain events via `chromedp.ListenTarget`, one listener per registered session shared via an `EventBus` the same way. Maps `dom.EventChildNodeInserted/Removed`→created/destroyed, `dom.EventAttributeModified`→propertychanged, `accessibility.EventNodesUpdated`→propertychanged | **Genuinely live-tested** against real headless Chrome (`TestIntegrationLiveChromeEventSubscribe`): navigates a `data:` page, subscribes, mutates the DOM via `chromedp.Evaluate`, asserts a real decoded event arrives. Found and fixed a real bug during this: CDP only fires DOM events for nodes the client already "knows about" — `dom.GetDocument()` must be called with `WithDepth(-1)` (not the default depth-1) after `dom.Enable()`, or zero events ever fire for the existing tree. This is the **only backend with genuine live event verification** in this phase, matching the plan's explicit call-out that CDP is "the one you can actually test live" |
| **Windows UIA** | `false` (unchanged) | **Not implemented** — no `Subscribe` method added to `UiaBackend`. `AddAutomationEventHandler` is the correct real API but cannot be honestly verified without real Windows hardware, so per the task's explicit instruction ("do NOT claim an untestable implementation works") it was left undone | Falls back to `core.WaitFor` polling via `events.WaitFor`'s `sub == nil` path |
| **macOS AX** | `false` (unchanged) | **Not implemented** — no `Subscribe` method added to the darwin backend. `AXObserverCreate` is the correct real API, same untestable-on-this-box reasoning | Falls back to polling |

Only 4 files outside the two new packages were touched to wire events in for Linux/CDP, all
additive per the explicit file-ownership allowance ("additive changes needed to subscribe to
events"): `platform/linux/backend.go` (added an `events *eventSource` field + `Close()` cleanup),
`platform/linux/discovery.go` (`detectCapabilities` now sets `Events: busAvailable`),
`platform/browser/backend.go` (`Available()` now sets `Events: true` alongside the existing three
capabilities). No changes to `platform/windows/**` or `platform/darwin/**`.

## Files created

- `internal/uiauto/vision/{vision.go,validate.go,grid.go}` + `{validate_test.go,grid_test.go}`
- `internal/uiauto/events/{events.go,subscriber.go,bus.go,wait.go}` + `{bus_test.go,wait_test.go}`
- `internal/uiauto/platform/linux/events.go` + `events_test.go`
- `internal/uiauto/platform/browser/{events.go,events_integration_test.go}` + `events_test.go`
- `internal/llm/tools/desktop_click_at.go`

## Files edited

- `internal/uiauto/manager.go` — `listDisplays` var, `SemanticAvailable`, `validateCoordinates`,
  `ClickAt`, `Screenshot(ctx, target, grid)`, `Wait` now uses `events.WaitFor`.
- `internal/uiauto/manager_test.go` — updated all `Screenshot` call sites (+`false`/`+true`), new
  tests for `ClickAt` (policy/no-backend/coordinate validation), `SemanticAvailable`, and the
  event-driven `Wait` path via a `fakeBackend`+`events.Subscriber` composite.
- `internal/uiauto/platform/linux/backend.go`, `discovery.go` — additive Events wiring (above).
- `internal/uiauto/platform/browser/backend.go` — additive Events wiring (above).
- `internal/llm/tools/desktop_screenshot.go` — `grid` param.
- `internal/llm/tools/builtin_names.go`, `internal/llm/agent/tools.go` — registered
  `desktop_click_at` (both `CoderAgentTools`/`CoderAgentToolsWithMesnada`).
- `internal/llm/tools/builtin_names_test.go`, `internal/llm/agent/desktop_tools_test.go` — tool
  count 11 -> 12.
- `internal/llm/tools/desktop_tools_test.go` — new tests: `desktop_click_at` permission-denied /
  granted-then-fails / policy-denied, `desktop_screenshot` grid param decode.

## Motivation

Some GUI surfaces (canvas apps, remote desktop windows, games, broken a11y implementations)
expose no usable accessibility semantics; the agent needs a safe, clearly-labeled fallback rather
than being stuck. Separately, `desktop_wait` polling every 200ms is wasteful and slow to react
when a backend can genuinely push change notifications instead.

## Verification

- `go build ./...` — clean, whole repo, no regressions.
- `go vet ./internal/uiauto/... ./internal/llm/tools/...` — clean (native, linux).
- `go test ./internal/uiauto/... ./internal/llm/tools/ ./internal/llm/agent` — all pass, including
  the new packages and the live CDP event integration test.
- `GOOS=windows go build ./internal/uiauto/...` / `GOOS=darwin go build ./internal/uiauto/...` —
  both clean; `go vet` under those GOOS shows only the two pre-existing, documented
  `possible misuse of unsafe.Pointer` FFI warnings from Phases 3-5 (`comcall_windows.go`,
  `screen_darwin.go`, `ax_darwin.go`) — nothing new from this phase.
- `gofmt -l internal/uiauto internal/llm/tools` — clean except the same 3 pre-existing unformatted
  files (`aliases.go`, `lua_tools.go`, `remembrances_code_test.go`) noted in every prior phase.
- No cgo introduced; no new external dependency beyond `golang.org/x/image` (already in go.mod at
  v0.41.0, previously unused directly).

## Deferred / honest limitations

- Windows UIA and macOS AX event subscriptions are genuinely unimplemented (not stubbed-and-hidden
  — `Capabilities.Events` correctly reports `false`), per the explicit instruction not to claim an
  untestable implementation works. A future phase with real hardware access should implement
  `AddAutomationEventHandler`/`AXObserverCreate`.
- AT-SPI's `Subscribe` does not filter events server-side by `scope` (AT-SPI2's Event.Object
  signals are not natively scopable that way); every waiter receives every event and
  re-evaluates its own selector, which is correct but not maximally efficient under heavy event
  traffic.
- The CDP event listener requires `dom.GetDocument().WithDepth(-1)` to see events for the
  existing tree; very large pages could make this initial pull expensive. Not optimized in this
  phase (out of scope — Phase 6's traversal code already avoids pulling a full tree for Find/
  Children; this is a separate, events-only cost).
- `desktop_click_at` has no region/rectangle variant (click-center-of-a-region) — only a single
  point, per the plan's minimum bar ("Add a desktop_click_at (coordinate click)"); `Manager`'s
  vision-fallback surface could grow a `ClickRegion`/drag/scroll-at-point helper later if the
  model needs it.