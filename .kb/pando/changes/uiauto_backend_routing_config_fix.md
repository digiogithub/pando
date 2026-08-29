---
created_at: 2026-08-30T03:14:07.898856328Z
updated_at: 2026-08-30T03:14:07.898856328Z
---
# Block R — backend routing + config default defect (Desktop Controller follow-up)

Implements Block R (R1-R3) of [[desktop_controller_wayland_routing_plan]]. Date: 2026-08-30.
Ran in parallel with Block W (Wayland portal parity, `internal/uiauto/input`, `internal/uiauto/screen`,
new `internal/uiauto/portal` package) — no file overlap.

## R1 — Per-scope backend routing

Root cause: `core.Registry.Resolve("auto")` returns the FIRST backend in `SetAutoOrder(...)` that
constructs without error. `atspi`/`uia`/`ax` all construct successfully whenever their bus/API is
merely reachable (independent of whether any app is actually exposing anything through it), so
with `"cdp"` sharing that same order, a Linux box with a live a11y bus always resolved `atspi`
first and `cdp` was never even tried — `DesktopBackend="auto"` could never reach the browser.

### Files changed

- `internal/uiauto/backends.go` — `SetAutoOrder("atspi", "uia", "ax", "null")`: removed `"cdp"`
  from the OS-backend race entirely (documented why in a comment).
- `internal/uiauto/manager.go` — full rewrite of `Manager`'s backend model:
  - `Manager` now holds `osBackend`/`osBackendName` (the OS accessibility backend, resolved from
    the OS-only auto order or an explicit pin) and `cdpBackend` (the CDP backend, resolved
    *independently* via `Registry().Resolve("cdp")`, only when not pinned) instead of one fixed
    `backend`/`resolver`.
  - `resolveBackends(opts)` — new: pinned (`opts.Backend` explicit, non-`"auto"`) disables routing
    entirely (`cdpBackend` stays `nil`); `"auto"`/`""` resolves both independently.
  - `backendForScope(ctx, scope)` — routes a scope to `cdpBackend` when it names the browser's
    virtual app (`platform/browser.AppID`, now **exported**, was `appID`), an element whose
    `Backend` field is `"cdp"`, or a `WindowID` that is one of the browser's live CDP page targets
    (checked via `cdpBackend.Windows`, which itself never launches a browser). Everything else, and
    every path when `CdpAvailable()` is false (pinned or no `cdp` registered), goes to `osBackend`.
  - `backendForElement(el)` — routes a ref-addressed operation (`Click`/`Type`/`Key`/`Scroll`/
    `Focus`/`Read`) to whichever backend produced that element (`el.Backend`, already an existing
    `core.Element` field, now actually honoured for routing), not to whatever "auto" would resolve
    fresh right now — so a ref survives across snapshots and later config changes deterministically.
  - `Observe`/`Find`/`Wait` each call `backendForScope` **once** per call and stamp every resulting
    element/snapshot with that backend's name.
  - `resolverFor(backend)` replaces the single long-lived `*core.ActionResolver` — a fresh
    `core.ActionResolver` is built per action call bound to the routed backend + the shared
    `physical core.PhysicalInput`/`AllowPhysicalInput`, since which backend an action targets is
    now a per-call decision, not fixed at `Manager` construction.
  - `Apps`/`Windows` merge OS + CDP results (CDP's `AppInfo`/`WindowInfo` appended alongside the OS
    backend's, filtered through the existing allow/deny policy); a CDP-side error (no session, or
    unreachable) is absorbed silently — never surfaces as a failure of the whole call — preserving
    the "cdp never launches a browser" / stays-inert-with-no-session contract from Phase 6.
  - `Capabilities()` is unchanged (OS-backend snapshot, exactly the pre-routing behavior). New
    `CapabilitiesFor(ctx, scope)` reports the capabilities of whichever backend `backendForScope`
    would actually route `scope` to (merged with the shared physical-input/screen-capture layers),
    so a caller never learns about a capability the backend serving that scope doesn't have.
  - New: `Manager.IsPinned()`, `Manager.CdpAvailable()`.
- `internal/uiauto/platform/browser/element.go` — renamed the unexported `appID` constant to an
  exported `AppID` (kept `appID = AppID` as an unexported alias so every existing call site in the
  package needed no change) so `internal/uiauto.Manager` can recognize a browser-scoped operation
  by `app_id == browser.AppID` without probing/launching a browser to find out. This was the one
  change made inside `platform/browser` — the rest of that package (CDP wire logic, `Available`'s
  inert-with-no-session contract) is untouched.
- `internal/uiauto/manager_test.go` — `managerWithBackend` now delegates to a new
  `managerWithBackends(osBackend, cdpBackend, opts)` helper; `fakeBackend` gained a `name` field
  (to impersonate `"cdp"`), `windowsCalls`/`availableErr` counters. Six new tests: routing a
  browser-app scope to `cdp` (`TestManagerRoutesBrowserScopeToCdp`), a native-app scope to the OS
  backend (`TestManagerRoutesNativeScopeToOSBackend`), pinned mode disabling routing
  (`TestManagerPinnedBackendDisablesRouting`), ref provenance surviving across `Find`→`Click` even
  when the "current" scope would resolve differently (`TestManagerRefProvenanceHonoredAcrossOperations`),
  the cdp-absorbs-its-own-errors property (`TestManagerCdpNeverConsultedWithoutSession`), and
  per-scope capability reporting (`TestManagerCapabilitiesForScopeMatchesServingBackend`).
- `internal/uiauto/manager_cdp_integration_test.go` — **new, live-verified**:
  `TestIntegrationLiveChromeRoutesBrowserScopeToCdp` launches a real headless Chrome on this
  machine (confirmed working, same pattern as Phase 6's `backend_integration_test.go`), registers
  it via `uiautobrowser.RegisterSession` exactly as `browser_session.go` does, builds a real
  `Backend:"auto"` `Manager` (which also resolves a real live AT-SPI OS backend on this box, so
  this genuinely exercises routing between two live backends, not a null-OS-backend-by-elimination
  case), and confirms: the browser appears in `Apps()`, an `app_id:"browser"`-scoped `Find` reaches
  the real page and tags results `Backend:"cdp"`, `Click` on that ref performs a real native CDP
  click (`method:"native"`), and `CapabilitiesFor` a browser scope reports the live session's real
  capabilities. **Passed** (`go test -run TestIntegrationLiveChromeRoutesBrowserScopeToCdp -v`).

## R2 — `browser_*` vs. `desktop_*` overlap guidance

Both families can now act on the same web page (R1 makes `desktop_*` reach CDP). Added explicit
"when to use this instead of X" guidance to both tool description sets (the `WHEN TO USE THIS
TOOL` text the model reads):

- `internal/llm/tools/desktop_{apps,observe,find,click,type,read,scroll,focus,key,wait,screenshot}.go`
  — each got a tailored bullet naming its `browser_*` counterpart and the concrete reason to pick
  one over the other (CSS selector vs. accessibility ref, DOM/HTML vs. accessibility tree, JS
  execution has no `desktop_*` equivalent, OS-level focus/shortcuts have no `browser_*`
  equivalent, whole-screen vs. page-only screenshot). `desktop_click_at` (pure vision fallback) was
  left as-is — no browser-specific overlap to call out there.
- `internal/llm/tools/browser_navigate.go`, `browser_evaluate.go`, `browser_content.go`,
  `browser_screenshot.go` (the four `browser_*` files that already used the `WHEN TO USE THIS
  TOOL` format) got the reciprocal guidance.
- `internal/llm/tools/browser_interact.go` (`browser_click`/`browser_fill`, one-line
  `Description` format, no `WHEN TO USE` block) — appended a short comparative clause to each
  one-liner instead of restructuring the format.
- `docs/desktop-controller.md` — new `## Backend routing: OS accessibility vs. the browser` and
  `## \`desktop_*\` vs. \`browser_*\`` sections (with TOC entries) right after the Philosophy
  section; updated the `DesktopBackend` config-table row to describe independent OS/CDP resolution
  + per-scope routing instead of the old single-winner "auto" description. Left the platform
  support matrix and Wayland prerequisites section untouched (Block W's).

## R3 — Config default defect: investigated, NOT reproducible against the vendored viper

Phase 8 recorded ([[uiauto_exposure_docs_phase8]]) an "incidental finding": a project
`.pando.toml` `[InternalTools]` table setting only `DesktopEnabled = true` was observed to lose
the `DesktopAllowPhysicalInput = true` default, attributed to `mergeLocalConfig`'s
`viper.MergeConfigMap` "not reliably preserving unrelated global defaults for sibling keys in that
same nested table." `internal/config`'s existing `normalizeMesnadaDelegationDefaults` comment
describes the same suspected class of bug for `[Mesnada]`/`[Mesnada.Delegation]` (a documented,
but similarly never-directly-confirmed, "viper's nested-default shadowing").

**Investigation** (read `github.com/spf13/viper@v1.20.0`'s actual `AllKeys`/`Unmarshal` source,
`internal/config/config.go`'s `mergeLocalConfig`/`Load`): both `viper.Get(key)` and
`viper.Unmarshal` resolve through `v.getSettings(v.AllKeys())`, and `AllKeys`'s
`flattenAndMergeMap` only shadows a lower-priority layer's subtree when a *higher-priority* layer
holds an **immediate (non-map) value at the exact parent key path** — a plain partial TOML table
under one map (`internalTools.desktopEnabled` present, `internalTools.desktopAllowPhysicalInput`
absent) never creates that condition, because the parent key (`internaltools`) is a map at every
layer that defines it, never a scalar.

**Reproduction attempts, all against the real `Load()` entry point end to end** (see
`internal/config/config_test.go`'s new tests, all passing — i.e., no defect found):
1. Project-only partial `[InternalTools]` table (`DesktopEnabled = true` alone, no global config)
   — `DesktopAllowPhysicalInput`, `DesktopBackend`, `DesktopMaxNodes`, `DesktopDefaultDepth`,
   `DesktopActionTimeout`, `DesktopSnapshotTTL`, `DesktopScreenshotScale` all read back at their
   documented defaults.
2. Global config with a full explicit `[InternalTools]` table + project config setting only
   `DesktopEnabled` — same correct result.
3. A synthetic two-level-nesting case (`parent.child.leaf`/`parent.child.other` defaults, a config
   file setting only `parent.sibling`) exercising `mergeMaps`/`flattenAndMergeMap` directly — the
   nested default (`leaf`) still resolved correctly.
4. The literal `[Mesnada]\nEnabled = true` (no `[Mesnada.Delegation]` table) scenario the existing
   `normalizeMesnadaDelegationDefaults` comment describes — `Mesnada.Delegation.MaxResurrections`/
   `MaxDepth` also came back correct via a plain `Load()`, without that function's zero-value
   workaround even being exercised for this to hold.
5. Confirmed the opposite direction still works too: an *explicit* `DesktopAllowPhysicalInput =
   false` in the partial table is correctly honoured (not shadowed back to the default).

**Conclusion**: this defect does not reproduce against the currently vendored
`github.com/spf13/viper v1.20.0`. It is **not desktop-specific** in the sense that the same
"sibling key without the nested table" shape was checked for `InternalTools` (one level) and
`Mesnada.Delegation` (two levels) and both are fine. Also checked and ruled out as an alternate
cause: the only in-process caller that ever persists a *partial* `InternalToolsConfig` is
`internal/api/handlers_config.go`'s `PUT /config/tools`, but it always round-trips the WebUI's
**full** `ToolsConfigResponse`/`InternalToolsConfig` (confirmed by reading the handler and Phase
1's notes) — a hand-partial `.pando.toml` table is a human/template-authored file, not something
Pando itself ever writes. Most likely explanation: either the vendored viper version has since
fixed the shadowing mechanism the original diagnosis suspected, or Phase 8's observation was a
different failure (e.g. a genuine `PLATFORM_NOT_SUPPORTED` from the physical-input layer on that
headless/tty dev box, which manifests identically to a policy/config denial from
`desktop_click_at`'s caller-facing perspective) misattributed to config merging.

**What was changed**: no functional code change to `mergeLocalConfig`/viper handling — there is no
reproducible defect to fix at that level, and a heuristic "restore-default-when-zero" workaround
(the `normalizeMesnadaDelegationDefaults` pattern) is unsound for a `true`-defaulting **bool**
field like `DesktopAllowPhysicalInput` specifically: zero (`false`) is indistinguishable from "not
set" for a bool, so such a workaround would make an explicit `DesktopAllowPhysicalInput = false`
un-settable, a strictly worse regression than the (unreproduced) original bug. Added three
permanent regression tests to `internal/config/config_test.go` instead:
`TestPartialInternalToolsTablePreservesDesktopDefaults`,
`TestPartialInternalToolsTableExplicitFalseOverridesDefault`,
`TestPartialMesnadaDelegationTablePreservesDefaults` — all pass now, and will fail loudly if a
future change to `mergeLocalConfig` or a viper dependency bump reintroduces this shape of defect
in either direction.

## Verification

- `go build ./...` — clean, whole repo.
- `go vet ./internal/uiauto/...` — clean.
- `go test ./internal/uiauto/... ./internal/llm/tools/ ./internal/llm/agent ./internal/config ./internal/api` — all pass, including the new routing unit tests, the live-Chrome routing integration test, and the R3 regression tests.
- `GOOS=windows go build ./internal/uiauto/...` / `GOOS=darwin go build ./internal/uiauto/...` — both clean.
- `GOOS=windows|darwin go vet ./internal/uiauto/...` — only the two pre-existing, expected `possible misuse of unsafe.Pointer` FFI findings (`platform/windows/comcall_windows.go`, `platform/darwin/ax_darwin.go`, `screen/screen_darwin.go`), unchanged from before this work.
- `gofmt -l internal/uiauto internal/llm/tools internal/config` — only the three pre-existing unformatted files (`aliases.go`, `lua_tools.go`, `remembrances_code_test.go`), nothing new.
- Whole-repo `GOOS=windows|darwin go build ./...` still fails only for the pre-existing, unrelated `go-tree-sitter` cgo-only reason.

Ran in parallel with Block W (`internal/uiauto/input`, `internal/uiauto/screen`,
`internal/uiauto/portal`) with zero file conflicts; Block W's `internal/uiauto/screen` package had
a transient build error mid-session from their own in-progress edit (unrelated to this work) that
resolved itself once their edit landed.

[[desktop_controller_wayland_routing_plan]]