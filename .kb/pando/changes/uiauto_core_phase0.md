---
created_at: 2026-08-29T13:01:29.93870288Z
updated_at: 2026-08-29T13:01:29.93870288Z
---
# Pando Desktop Controller — Phase 0 (`internal/uiauto/core`) COMPLETE (2026-08-29)

Implements Phase 0 of [[pando/plans/desktop_controller_uiauto_plan.md]]: the platform-independent
core package for the Desktop Runtime. No OS calls, no cgo. Package doc comment on `element.go`
explains the boundary: platform backends live under `internal/uiauto/platform` (future phases).

## Files created (all under `internal/uiauto/core/`)

- `element.go` — `Element`, `ElementRef` (qualified `@<snapshotID>:<elemID>` ref) with
  `ParseElementRef`/`FormatElementRef`, `Bounds{X,Y,W,H}` with `Center()`/`Empty()`, `NativeData`.
- `role.go` — `Role` string type, canonical vocabulary (33 roles incl. `RoleUnknown`),
  `NormalizeRole(platform, raw string) Role` with per-platform tables for atspi/uia/ax/cdp plus a
  lowercase-fallback path, `Role.Matches(selectorRole string) bool` with alias table
  (textbox/edit/input→textfield, push/pushbutton→button, etc).
- `errors.go` — `ErrorCode` consts (PERM_DENIED, ELEMENT_NOT_FOUND, APP_NOT_FOUND, STALE_REF,
  SNAPSHOT_NOT_FOUND, POLICY_DENIED, ACTION_FAILED, PLATFORM_NOT_SUPPORTED, TIMEOUT,
  INVALID_ARGS), `DesktopError{Code,Message,Suggestion}` (implements `error`), one `New*Error`
  constructor per code with a default suggestion, `AsDesktopError(err)`, `(*DesktopError).Payload()
  map[string]any` for the `{ok:false,error:{...}}` LLM response shape.
- `capabilities.go` — `Capabilities{Screenshot,Accessibility,UIInspection,Mouse,Keyboard,
  WindowManagement,UIActions,Events bool}`, `Missing(required ...string) []string`, `String()`.
- `selector.go` — CSS-inspired (not real CSS) selector DSL: `ParseSelector(string) (*Selector,
  error)`, `Selector{Steps []SelectorStep}`, `Selector.String()` (canonical round-trip),
  `SelectorStep{Combinator,Role,Attrs []AttrPredicate,Pseudos []string,Nth int}`,
  `SelectorStep.MatchesElement(*Element) bool`. Grammar: descendant (space) / child (`>`)
  combinators; role token or `*`; `[attr op "value"]` with attr∈{name,value,description,id,role,
  class}, op∈{=,^=,$=,*=,~= (regexp)}; pseudos `:visible/:enabled/:focused/:disabled/:hidden`;
  `[nth=N]` (1-indexed, stored but NOT applied by `MatchesElement` — needs sibling context,
  documented for later-phase tree walkers to apply); bare `"Save"` shorthand → `[name="Save"]`
  on any role. All parse errors are `INVALID_ARGS` DesktopError.
- `snapshot.go` — `Snapshot{ID,CreatedAt,Backend,AppID,WindowID,Root,Elements map[string]*Element,
  Origin *Selector,NativeHandles map[string]any}`, `SnapshotStore` (goroutine-safe, mutex-guarded)
  with `NewSnapshotStore(ttl, max)`, `Put`, `Get`, `Resolve(ElementRef) (*Snapshot,*Element,error)`,
  `Prune()`, `Len()`. IDs: `s`+8 random base36 chars (crypto/rand) for snapshots, `e1,e2,...` for
  elements. TTL expiry → `STALE_REF`; unknown id → `SNAPSHOT_NOT_FOUND`; missing element in a live
  snapshot → `ELEMENT_NOT_FOUND`; malformed ref → `INVALID_ARGS`. LRU eviction beyond `max` (O(n²)
  selection pass, snapshot counts expected small).
- `backend.go` — `Backend` interface (`Name, Available, Apps, Windows, Find(scope,selector,limit),
  Children, Properties, Perform, Close`) designed so no implementation is ever forced to build a
  full tree; `AppInfo`, `WindowInfo`, `Scope{AppID,WindowID,Root,Depth}`; `Registry` with
  `Register`/`SetAutoOrder`/`Resolve("auto"|name)`; `NullBackend` (all ops →
  `PLATFORM_NOT_SUPPORTED`, `Available` returns all-false `Capabilities`, never errors).
- `action.go` — `ActionKind` consts (invoke, focus, setvalue, toggle, select, expand, collapse,
  scroll, press, type), `Action{Kind,Text,Amount,Key}`, `PhysicalInput` interface
  (`Click,MoveMouse,TypeText,PressKey,Scroll`), `ActionResolver{Backend,Physical,AllowPhysical}`
  implementing native-first/physical-fallback for `Click/Type/Scroll/Focus/Press`, returning
  `ActionResult{Method:"native"|"physical",Action,Notes []string}`. Fallback triggers only on
  `ACTION_FAILED`/`PLATFORM_NOT_SUPPORTED` and only when `AllowPhysical`, a `PhysicalInput` is set,
  and the element has non-empty bounds; any other backend error (e.g. `PERM_DENIED`) propagates
  untouched.
- `locator.go` — `Locator{Scope,Selector}` with `Resolve`/`ResolveAll`, `Condition` consts (exists,
  notexists, visible, enabled, focused), `WaitFor(ctx, Backend, *Locator, Condition, timeout,
  interval) (*Element, error)` — polls via `time.NewTimer` inside a `select` on `ctx.Done()`, never
  a bare `time.Sleep`; timeout → `TIMEOUT` DesktopError; ctx cancel → `ctx.Err()`.
- `render.go` — `RenderTree(*Snapshot, RenderOptions) string` and `RenderElements([]*Element,
  RenderOptions) string`. `RenderOptions{MaxNodes,MaxDepth int,IncludeBounds,IncludeInvisible
  bool}`. Produces the `WINDOW "Title" (app: X)` header + indented `@snap:eN role "name"
  value="..." flags...` lines; collapses semantically-empty containers (no name/value/description/
  actions) into their children without consuming a depth level; truncation notice `"... N more
  nodes not shown; narrow with desktop_find"` when `MaxNodes` is hit (N counts the whole skipped
  subtree).

## Public API surface for later phases

Everything above is exported; key entry points later phases (P1 tool wiring, P2 AT-SPI backend,
etc) will consume: `core.ParseSelector`, `core.Selector.String/Steps`, `core.SelectorStep.
MatchesElement`, `core.NormalizeRole`, `core.Role.Matches`, `core.NewSnapshotStore`, `core.
SnapshotStore.{Put,Get,Resolve,Prune}`, `core.FormatElementRef/ParseElementRef`, `core.Backend`
interface + `core.Registry` + `core.NewNullBackend`, `core.NewActionResolver` + `core.
ActionResolver.{Click,Type,Scroll,Focus,Press}`, `core.NewLocator`, `core.WaitFor`, `core.
RenderTree`, `core.RenderElements`, `core.New*Error` constructors + `core.AsDesktopError` +
`(*core.DesktopError).Payload()`.

## Verification

- `go build ./...` — OK (whole repo, no regressions).
- `go vet ./internal/uiauto/...` — clean.
- `go test ./internal/uiauto/...` — all pass (table-driven tests in `*_test.go` next to each
  source file: selector parse/round-trip/match incl. every operator+pseudo+nth+combinators+
  bare-quoted shorthand; role normalization for atspi/uia/ax/cdp + fallback paths; snapshot TTL +
  LRU + STALE_REF + qualified-ref parsing; ActionResolver native-vs-fallback with a fake backend +
  fake physical input, incl. non-fallback-eligible-error propagation and disallowed/no-bounds
  cases; locator wait/retry incl. ctx cancellation and TIMEOUT; renderer truncation/collapsing/
  bounds/invisible-filtering).
- `gofmt -l internal/uiauto` — empty (clean).
- Package builds with zero cgo; verified no `import "C"` anywhere in the tree.

## Deferred (out of Phase 0 scope, per the plan)

- No OS-specific code: `internal/uiauto/platform/{windows,darwin,linux}`, `internal/uiauto/input`,
  `internal/uiauto/screen`, `internal/uiauto/browser`, `internal/uiauto/manager.go` are all Phase
  2+ (P1 vertical slice does config/tools/permissions wiring against `NullBackend` first).
  `internal/desktop` (Wails launcher) was not touched.
- `SelectorStep.Nth` is parsed and round-trips but is intentionally NOT applied by
  `MatchesElement` (no sibling context available at that layer); future tree-walking code
  (backend `Find` implementations) must apply it explicitly.
- No tool files (`internal/llm/tools/desktop_*.go`), no config keys, no `builtin_names.go`
  entries, no agent/tools.go registration — all P1.