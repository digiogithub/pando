---
created_at: 2026-08-29T13:34:27.415972966Z
updated_at: 2026-08-29T13:34:27.415972966Z
---
# Pando Desktop Controller — Phase 2 (Linux AT-SPI2 backend) COMPLETE (2026-08-29)

Implements Phase 2 (P2) of [[pando/plans/desktop_controller_uiauto_plan.md]], building on
[[pando/changes/uiauto_core_phase0.md]] and [[pando/changes/uiauto_tools_phase1.md]]. Delivers
the Linux AT-SPI2 `core.Backend` over D-Bus (`github.com/godbus/dbus/v5`, no cgo). Scope was
strictly the Linux backend: `internal/uiauto/manager.go`, `internal/uiauto/backends.go`,
`internal/uiauto/core/*` (except one additive role-map extension), `internal/uiauto/input/**`,
`internal/uiauto/screen/**` were not touched (those are owned by the parallel Phase 3
input/screenshot agent working concurrently in the same tree).

## New package: `internal/uiauto/platform/linux/`

- `doc.go` — package doc explaining the (bus name, object path) element identity, the
  selector-driven traversal design, and the `busConn` test-seam.
- `conn.go` — `accessibleRef{Bus, Path}`; `busConn` interface (`call`, `getAllProps`, `close`)
  that all traversal/action code depends on instead of `*dbus.Conn` directly, so it is fully
  unit-testable against a fake in-memory tree; `dbusConn` (real implementation);
  `storeSoRefSlice` — decodes an AT-SPI `a(so)` array-of-struct reply via `reflect` + per-element
  `dbus.Store`, because `dbus.Store` cannot decode array-of-struct generically into a
  `[]struct{...}` (each element's static Go type is `interface{}`, which fails its
  convertibility check) and because a real bus can hand back either `[]interface{}` or
  `[][]interface{}` for the same "a(so)" reply depending on the code path godbus took.
- `discovery.go` — `discoverBusAddress`: `org.a11y.Bus.GetAddress` on the session bus (via
  `/org/a11y/bus`), falling back to `AT_SPI_BUS_ADDRESS`; `connectA11yBus` dials the resolved
  address with `dbus.Connect` and maps any failure to a `PERM_DENIED` `core.DesktopError` with
  an actionable suggestion (`gsettings set org.gnome.desktop.interface toolkit-accessibility
  true`, restart the target app); `sessionKind()` (x11/wayland/unknown from
  `XDG_SESSION_TYPE`/`WAYLAND_DISPLAY`/`DISPLAY`); `detectCapabilities(busAvailable bool)` — only
  `Accessibility`/`UIInspection`/`UIActions` ever flip true, `Mouse`/`Keyboard`/`Screenshot`
  always false (Phase 3's job), never faked.
- `state.go` — AT-SPI2 `AtspiStateType` bit indices (`stateEnabled`, `stateSensitive`,
  `stateVisible`, `stateShowing`, `stateFocused`, `stateFocusable`, `stateChecked`,
  `stateSelected`, `stateExpanded`, `stateExpandable`, `stateCheckable`, `statePressed`,
  `stateSelectable`, `stateEditable`, `stateBusy`); `hasState([]uint32, bit)` decodes the 2-word
  (64-bit) bitmask, tolerant of a short/nil slice; `decodedState` +
  `decodeState(raw []uint32)`; `elementEnabled()` = ENABLED‖SENSITIVE, `elementVisible()` =
  VISIBLE&&SHOWING map onto `core.Element.Enabled/Visible` (Focused = FOCUSED directly);
  `nativeExtras()` renders the rest (checked/selected/expanded/focusable/expandable/checkable/
  pressed/selectable/editable/busy) into `NativeData.Data`.
- `element.go` — `fetchNode` gathers one `fetchedNode` per AT-SPI object with a bounded, fixed
  set of round trips: one `Properties.GetAll(Accessible)` (Name/Description/ChildCount), one
  `GetRoleName`, one `GetState`, one `GetInterfaces`, and only when the relevant interface is
  advertised, one `Component.GetExtents` and one `Value.CurrentValue` read (Text content is
  *not* fetched during traversal — only on demand via `Properties()`, to keep traversal cheap).
  `toElement` builds the normalized `core.Element` via `core.NormalizeRole("atspi", rawRole)`,
  stashes `busName`/`objectPath` in `Native.Data` (`nativeBusKey`/`nativePathKey`) plus the state
  extras, and derives a coarse, zero-extra-call `Actions` list from advertised interfaces/state.
  `refFromElement` recovers the `accessibleRef` an element was built from: prefers the
  `Native.Data` handle, falls back to `AppID` (bus name) + `WindowID` (object path) for the
  synthetic root `Element` `Manager.rootElement` builds from `WindowInfo` (which carries no
  `Native` at all) — this is why `Windows()` encodes `WindowInfo.AppID` = application bus name and
  `WindowInfo.ID` = the frame's object path.
- `traverse.go` — the performance-critical piece. `traverseState` is a short-lived, per-call memo
  cache (`nodeCache`, `childCache` keyed by `accessibleRef`) so a single `Find`/`Children` call
  never re-fetches the same object twice. `findRec` implements **selector-driven, incremental**
  traversal: a `findState{ds, cs}` pair is threaded down each DFS branch — `ds` (descendant-
  combinator pending step indices) persists to every depth until matched (mirrors CSS descendant
  semantics: unanchored, could match arbitrarily deep), `cs` (child-combinator pending indices)
  applies only to the immediate children of the node that satisfied the preceding step and is
  *not* re-propagated further. A branch is pruned (no `GetChildren` call at all) the instant both
  `ds` and `cs` are empty — nothing pending can ever complete the selector below that node. Stops
  the instant `len(results) >= limit`; respects a `maxDepth` cap; checks `ctx.Err()` at every
  recursive entry and aborts on cancellation; a single unreadable/defunct node is skipped (branch
  dropped) without aborting the whole search, *unless* the error is a context error. `filterNth`
  applies `SelectorStep.Nth` (which `core.SelectorStep.MatchesElement` deliberately leaves
  unapplied per Phase 0's own documentation) for child-combinator steps, using the 0-based
  position among a parent's `GetChildren` result — descendant-combinator `Nth` stays unsupported
  (documented limitation, no sibling context available at that scope).
- `actions.go` — `performAction` dispatch per the plan: `focus` → `Component.GrabFocus`;
  `setvalue`/`type` → `EditableText.SetTextContents`; `invoke`/`toggle`/`select`/`expand`/
  `collapse` → `Action.DoAction`, picking the action (via `GetName`/`NActions`) whose name
  contains a kind-specific hint (`click`/`press`/`activate`/`invoke` for invoke, etc.), falling
  back to index 0 only for `invoke`. Any missing interface or unmatched action name/`DoAction`
  returning `false` yields `ACTION_FAILED`, letting `core.ActionResolver`'s native-first/
  physical-fallback policy take over (Phase 3 wires the physical side).
- `backend.go` — `AtspiBackend` implements `core.Backend` end to end: `Name()"atspi"`,
  `Available` (lazy-connects, never errors — reports an honest all-false `Capabilities` instead,
  matching the `NullBackend`/Manager contract), `Apps`/`Windows` (registry root children =
  applications; each app's direct children = frames/windows), `Find` (selector-driven per
  `traverse.go`, root resolution via `resolveScopeRoots`: `scope.Root` → that element;
  `scope.WindowID` (requires `scope.AppID`) → that window; `scope.AppID` only → the whole app
  subtree; neither → every running application, still selector-pruned/limit/depth-capped),
  `Children`, `Properties` (cheap by default; `"text"`/`"actions"` fetched only on request),
  `Perform`, `Close`. The bus connection is established lazily (never in `NewBackend`, so backend
  construction itself can never fail) and cached under a mutex.
- `internal/uiauto/backends_linux.go` (`//go:build linux`) — the one file registering the
  backend: `globalRegistry.Register("atspi", func() (core.Backend, error) { return
  linux.NewBackend() })` in its own `init()`, inside package `uiauto` itself (not a separate
  blank-import file) — this avoids an import cycle, since `platform/linux` never imports
  `internal/uiauto`. `manager.go`/`backends.go` needed zero changes.

## Additive core change

`internal/uiauto/core/role.go`'s `atspiRoleMap` gained 5 entries for common AT-SPI2 roles Phase 0
did not cover: `"spin button"→RoleTextField`, `"check menu item"/"radio menu item"/"tearoff menu
item"→RoleMenuItem`, `"notification"→RoleDialog`. Purely additive; all existing `core` tests still
pass unmodified.

## Tests

`internal/uiauto/platform/linux/`:
- `fake_conn_test.go` — `fakeConn` (`busConn`) backed by an in-memory `map[accessibleRef]*fakeNode`
  simulating Accessible/Component/Action/EditableText/Text/Value method+property calls, including
  rejecting a call against an interface a node doesn't advertise (mirrors a real D-Bus
  `UnknownMethod`/`UnknownInterface` error) and per-(ref,method) call counters for cache
  assertions.
- `state_test.go` — bitfield decode across both 32-bit words, short/nil-slice tolerance,
  `elementEnabled`/`elementVisible` bit-combination truth tables, `nativeExtras` content.
- `traverse_test.go` — single-step descendant match, multi-step child-combinator chain (2 of 3
  siblings match), `limit` stopping early, `maxDepth` pruning before vs. after the match depth,
  branch pruning when no selector step remains pending (isolated via a synthetic cs-only
  `findState`, since the public entry always seeds `ds={0}` which — like CSS — never fully
  exhausts), skipping one unreadable branch without aborting the whole search, `ctx` cancellation,
  the per-call node memo cache (asserted via call counters), `filterNth`.
- `actions_test.go` — invoke picks a click-like action by name / falls back to index 0 / fails
  without an Action interface; focus calls `GrabFocus`; setvalue calls `SetTextContents` / fails
  without `EditableText`; expand+collapse use their named actions; an unmapped `ActionKind`
  (e.g. `scroll`) returns `PLATFORM_NOT_SUPPORTED` so the resolver can fall back.
- `element_test.go` — `fetchNode`→`toElement` round trip (role/name/description/bounds/state→
  Enabled/Visible/Focused/Native handle); `refFromElement` prefers the Native handle, falls back
  to AppID/WindowID for the Manager's synthetic root, errors `ELEMENT_NOT_FOUND` with neither;
  `actionsFor` heuristics.
- `discovery_test.go` — session-bus lookup failure (deterministic via an unreachable
  `DBUS_SESSION_BUS_ADDRESS`) falling back to `AT_SPI_BUS_ADDRESS`; both unavailable →
  `PERM_DENIED` with a non-empty suggestion; `connectA11yBus` wraps a dial failure as
  `PERM_DENIED` with the accessibility-enable suggestion; `sessionKind()` table; `detectCapabilities`
  never sets Mouse/Keyboard/Screenshot.
- `backend_integration_test.go` — real-bus smoke tests (`TestIntegrationListApplications`,
  `TestIntegrationBackendAvailable`), auto-`t.Skip`'d unless both `DISPLAY`/`WAYLAND_DISPLAY` are
  set AND `connectA11yBus` actually succeeds, so CI/headless stays green.

## Real-bus verification (honest result)

This dev machine has `org.a11y.Bus` registered on the session bus (`busctl --user list` confirms
it) but is a bare **tty session** — `DISPLAY`/`WAYLAND_DISPLAY` are both unset, so
`backend_integration_test.go`'s guard correctly `t.Skip`s under `go test`. To still exercise the
real bus honestly, a throwaway `main.go` was built and run manually (not part of the test suite,
removed afterward): `AtspiBackend.Available` connected successfully and reported
`accessibility,uiActions,uiInspection` capabilities (Mouse/Keyboard/Screenshot correctly false);
`Apps()` returned **0 applications with no error** — the correct, honest outcome for a tty session
with no a11y-registered GUI application running (nothing to enumerate, not a failure). This also
caught and fixed a real decode bug: a live AT-SPI `a(so)` reply came back from godbus typed as
`[][]interface{}`, not the `[]interface{}` the unit tests (built against the fake conn) exercised
— `storeSoRefSlice` was made shape-tolerant via `reflect.ValueOf(raw).Kind()==Slice` iteration
instead of a single type assertion, fixing `listAppRefs`/`getChildren` against the real bus.

## Verification

- `go build ./internal/uiauto/...` and `go build ./...` — clean, no regressions.
- `GOOS=windows go build ./...` and `GOOS=darwin go build ./...` — **fail**, but for a
  pre-existing, unrelated reason: `github.com/madeindigio/go-tree-sitter` (used by
  `internal/rag/treesitter` for code indexing) does not cross-compile for windows/darwin
  (`undefined: Node`, build-constraint exclusions), confirmed present before this change (go.mod
  diff for this change only adds `godbus/dbus/v5`, `ebitengine/purego`, `jezek/xgb` —
  unrelated to go-tree-sitter) and reproducible with zero `internal/uiauto` changes involved.
  Scoped rebuild confirms Phase 2 itself cross-compiles cleanly:
  `GOOS=windows go build ./internal/uiauto/...` and `GOOS=darwin go build ./internal/uiauto/...`
  both succeed with no output.
- `go vet ./internal/uiauto/core/... ./internal/uiauto/platform/...` — clean.
- `go test ./internal/uiauto/core/... ./internal/uiauto/platform/...` — all pass (2 integration
  tests correctly skipped on this tty session).
- `go vet ./internal/uiauto/...` (whole package, all GOOS=linux) and
  `go test ./internal/uiauto/...` **fail only in the top-level `internal/uiauto` package**:
  `manager_test.go:157` references an undefined `fakeCaptureScreen` and has 3 now-unused imports.
  This is **not caused by this change** — `manager.go`/`manager_test.go`/`internal/uiauto/input`/
  `internal/uiauto/screen` are explicitly out of this phase's file ownership (owned by the
  parallel Phase 3 input/screenshot agent working concurrently in the same tree) and were not
  touched. `go build ./...` (non-test compilation) is unaffected and succeeds.
- `gofmt -l internal/uiauto` — empty (clean) for every file this change added/touched.
- `go mod tidy` — `github.com/godbus/dbus/v5 v5.1.0` was already promoted from `// indirect` to a
  direct `require` automatically (nothing else changed). No cgo introduced; no RobotGo; verified
  `internal/uiauto/platform/linux` has zero `import "C"`.

## Deferred / known limitations (documented, not silently dropped)

- `SelectorStep.Nth` is only honored for child-combinator (`>`) steps (`filterNth`); a descendant-
  combinator step with `[nth=N]` has no sibling context at that point in the traversal and is left
  unfiltered — same documented limitation Phase 0 already called out at the `core` layer.
- `Properties()`'s `"text"` fetch caps at 20000 characters (`Text.GetText(0,-1)`); large documents
  are truncated rather than dumped whole, to keep the on-demand call bounded.
- `Apps()` leaves `AppInfo.PID` at 0 — AT-SPI2 does not expose a portable, standard PID lookup over
  D-Bus without extra process-matching heuristics; not implemented.
- `org.a11y.atspi.Selection` is not wired into `Perform` (no `ActionKind` in Phase 0's vocabulary
  maps to it); left for a future phase if a selection-specific action is added.
- Physical-input fallback (`core.PhysicalInput`) is Phase 3's `internal/uiauto/input` package —
  this backend only ever returns `ACTION_FAILED`/`PLATFORM_NOT_SUPPORTED` when it cannot perform
  something natively, which is exactly what lets `core.ActionResolver` fall back once Phase 3
  wires a `PhysicalInput` into the `Manager`.