---
created_at: 2026-08-29T12:51:50.568386813Z
updated_at: 2026-08-29T19:52:14.092361934Z
---
# Pando Desktop Controller — Implementation Plan (`internal/uiauto`)

Status: **COMPLETE (all phases P0-P8), 2026-08-29**. Source analysis:
`/home/sevir/Descargas/PandoDesktopController.md`.
Related: [[plans/browser_tools_chromedp_plan]], [[pando/plans/internal_tools_implementation]], [[plans/tool-discovery-aliasing-anthropic-gpt-plan]].

## 1. Goal

Give Pando a **Desktop Runtime**: a semantic representation of the graphical environment (accessibility tree) plus the ability to act on it, so the agent does not have to reason over screenshots. Vision/OCR and physical mouse/keyboard are **fallbacks**, never the primary mechanism.

```
Pando agent
   |
   +-- desktop_* tools  (internal/llm/tools/desktop_*.go)
              |
        Desktop Manager (internal/uiauto)
              |
   +----------+-----------+-----------+
 Observation   Actions      Events   Screenshot
   |             |
 UI Tree      Action Resolver
 Normalizer   (native action -> physical fallback)
   |             |
   +------+------+
          |
     Backend interface
          |
 +--------+--------+---------+---------+
 Windows   macOS    Linux     Browser   Null
  UIA       AX      AT-SPI2    CDP      (unsupported)
 (go-ole)  (purego) (godbus)  (chromedp)
```

## 2. Hard constraints derived from the repo

- **Package name**: `internal/desktop` is ALREADY TAKEN (Wails desktop app launcher: `app.go`, `launcher.go`, `embed*.go`). The new subsystem lives in **`internal/uiauto`**. `internal/desktop` was never touched across all 9 phases.
- **NO CGO**. Held for the entire feature: zero `import "C"` anywhere in `internal/uiauto`, verified at every phase.
  - Windows UIA -> COM via `github.com/go-ole/go-ole` + hand-built vtable dispatch.
  - Linux AT-SPI2 -> D-Bus via `github.com/godbus/dbus/v5`.
  - macOS AXUIElement -> `github.com/ebitengine/purego` (dlopen ApplicationServices/CoreFoundation).
  - Input/screenshot -> raw `user32`/`gdi32` via `syscall.NewLazyDLL` (Windows), `github.com/jezek/xgb` (X11 + XTEST + GetImage), XDG portals over godbus (Wayland), purego CGEvent/CGDisplay (macOS).
  - **RobotGo was rejected** (cgo-only on every platform) — confirmed never used.
- **Tool pattern**: `BaseTool` = `Info() ToolInfo` + `Run(ctx, ToolCall)`. Responses via `NewStructuredResponse` and `ToolResponseTypeImage` for screenshots — followed exactly.
- **Registration**: `internal/llm/agent/tools.go`, both `CoderAgentTools()` and `CoderAgentToolsWithMesnada()`, gated on `it.DesktopEnabled` — done in Phase 1, unchanged since.
- **Builtin names**: all 12 tool names in `internal/llm/tools/builtin_names.go` — done (11 in Phase 1, `desktop_click_at` added in Phase 7).
- **Config**: `InternalToolsConfig` in `internal/config/config.go`, API DTO in `internal/api/handlers_config.go`, TUI in `internal/tui/page/settings.go`, WebUI in `web-ui/src/components/settings/InternalToolsSettings.tsx`, `.pando.toml` templates in `internal/config/init.go`/`cmd/init.go` — all done in Phase 1.
- **Permissions**: every state-changing action goes through `permission.Service.Request(...)` — done, verified per-tool in Phase 1 and exercised live in Phase 8's e2e suite.
- **Tests**: Go tests next to every package; Python e2e under `tests/` — `tests/test_desktop_controller_mcp_e2e.py` added in Phase 8, building the real binary and driving the real MCP stdio transport.
- Everything **off by default** (`DesktopEnabled=false`) — held throughout; verified explicitly by the Phase 8 e2e suite (`tools/list` shows zero `desktop_*` tools both with `DesktopEnabled=false` and with the field omitted entirely).

## 3. Core design (from xa11y / agent-desktop, adapted)

Five concepts, all implemented as designed: `Locator -> Selector -> Lazy Resolve -> Native Action -> Snapshot/Ref`.

- **Element**: role, name, value, description, bounds, enabled/visible/focused, parent/children refs, actions, provenance (backend, app, window) and a `Native` escape hatch (`platform`, raw role/subrole, `map[string]any`).
- **Selector DSL** (CSS-inspired, NOT real CSS): `button[name="Save"]`, `textfield[name^="Search"]`, `group > button`, `app[name="Chrome"] window[name="Settings"] button[name="New Tab"]`, pseudo-filters `:visible`, `:enabled`, `:focused`, `nth=2`. Documented with examples in `docs/desktop-controller.md`.
- **Locator**: lazy; re-resolves immediately before acting; wait+retry loop with timeout instead of `sleep`.
- **Snapshot + qualified refs**: `@s8f3k2p9:e17`. Refs are ALWAYS qualified by snapshot id. Each ref stores the locator that produced it, so it can be re-resolved. `STALE_REF`/`SNAPSHOT_NOT_FOUND`/`ELEMENT_NOT_FOUND` are distinct, documented codes.
- **Structured errors for the LLM** — `{ok:false,error:{code,message,suggestion}}` with all 10 codes (`PERM_DENIED`, `ELEMENT_NOT_FOUND`, `APP_NOT_FOUND`, `STALE_REF`, `SNAPSHOT_NOT_FOUND`, `POLICY_DENIED`, `ACTION_FAILED`, `PLATFORM_NOT_SUPPORTED`, `TIMEOUT`, `INVALID_ARGS`) implemented and exercised live end-to-end in Phase 8's e2e tests.
- **Capabilities**, never `if linux { everythingWorks() }`: `Screenshot, Accessibility, UIInspection, Mouse, Keyboard, WindowManagement, UIActions, Events` — every backend reports honestly, never faked (e.g. Windows/macOS `Events: false` rather than a silent stub).
- **Backend must NOT be forced to build the whole tree.** `Find(root, selector)` is selector-driven on every backend (AT-SPI incremental D-Bus queries, UIA CacheRequest prefetch + local filter, AX batched attribute fetch, CDP incremental `getChildAXNodes`) — a branch that can no longer satisfy the remaining selector is pruned without even being fetched.
- **Progressive traversal**: `desktop_apps` (cheap, top-level) before `desktop_observe`/`desktop_find` (depth-capped, `MaxNodes`-budgeted).
- **Native action first**: every mutating tool tries the accessibility action first; physical-input fallback only on `ACTION_FAILED`/`PLATFORM_NOT_SUPPORTED` and only when `DesktopAllowPhysicalInput` and bounds are available.

## 4. Package layout (as built)

```
internal/uiauto/
  core/            Element, Role, Selector DSL, Locator, Snapshot store, Action/ActionResolver,
                    Errors, Capabilities, Backend interface + Registry + NullBackend, render.go   [P0]
  manager.go       Desktop Manager: backend selection, capability probe, policy, Screenshot,
                    ClickAt (vision), Wait (event-aware)                                          [P1,P3,P7]
  backends.go, backends_{linux,windows,darwin,browser}.go   per-platform registry wiring          [P1-P6]
  platform/
    linux/         AT-SPI2 backend over godbus                                                    [P2]
    windows/       UI Automation backend over go-ole + hand-built COM vtable calls                [P4]
    darwin/        AXUIElement backend over purego (dlopen ApplicationServices/CoreFoundation)     [P5]
    browser/       CDP backend riding the existing chromedp browser_* session                     [P6]
  input/           mouse.go keyboard.go + per-platform impls (Windows SendInput, X11 XTEST,
                    Wayland RemoteDesktop portal, macOS CGEvent)                                   [P3]
  screen/          screenshot.go + per-platform impls (GDI, X11 GetImage/Xinerama, Wayland
                    Screenshot portal, macOS CGDisplayCreateImage)                                 [P3]
  vision/          coordinate validation + grid overlay for the click_at fallback                  [P7]
  events/          backend-agnostic EventBus + WaitFor, optional events.Subscriber per backend     [P7]
internal/llm/tools/
  desktop_{apps,observe,find,read,click,type,key,scroll,focus,wait,screenshot,click_at}.go
  desktop_common.go   shared manager/error/permission/scope helpers                                [P1,P7]
```

## 5. Tool surface exposed to the model (12 tools, final)

| tool | purpose |
|---|---|
| `desktop_apps` | list running apps/windows (cheap, top level) |
| `desktop_observe` | snapshot of an app/window, `depth` limited, returns qualified refs |
| `desktop_find` | resolve a selector, returns matching refs |
| `desktop_read` | read text/value of a ref |
| `desktop_click` | native invoke, physical fallback |
| `desktop_type` | native SetValue -> text interface -> keyboard simulation |
| `desktop_key` | key/chord press (global or targeted) |
| `desktop_scroll` | scroll a ref |
| `desktop_focus` | focus element/window |
| `desktop_wait` | wait for a selector condition; event-driven on AT-SPI/CDP, polling elsewhere |
| `desktop_screenshot` | full screen / display / window / element crop, optional coordinate-grid overlay |
| `desktop_click_at` | vision-fallback coordinate click (`P7`), always marked `source:"vision"` |

Full argument/return documentation: `docs/desktop-controller.md`.

## 6. Config keys (`InternalToolsConfig`) — all implemented, defaults unchanged from plan

`DesktopEnabled` (default false), `DesktopBackend` (`auto|atspi|uia|ax|cdp|null`), `DesktopAllowPhysicalInput` (default true), `DesktopMaxNodes` (default 500), `DesktopDefaultDepth` (default 3), `DesktopActionTimeout` seconds (default 10), `DesktopSnapshotTTL` seconds (default 60), `DesktopScreenshotScale` (default 1.0), `DesktopAllowedApps` / `DesktopDeniedApps`.

**Known merge quirk (not fixed, documented for future work)**: a project `.pando.toml`
`[InternalTools]` table setting only some `Desktop*` keys can observably fail to inherit
`viper.SetDefault` values for the keys it omits (seen with `DesktopAllowPhysicalInput`) — a
pre-existing `internal/config` `mergeLocalConfig` behavior, not specific to this feature. Set
every `Desktop*` key explicitly in a project config until investigated.

## 7. Phases — all COMPLETE

### P0 — Core, platform independent — COMPLETE, unit-tested, zero OS calls
See [[pando/changes/uiauto_core_phase0.md]].

### P1 — Vertical slice: config + tools + permissions + UI wiring — COMPLETE
Null-backend-only pipeline, fully testable end to end. See [[pando/changes/uiauto_tools_phase1.md]].

### P2 — Linux AT-SPI2 backend (godbus) — COMPLETE
**Verification: smoke-verified against a real `org.a11y.Bus` session bus, but with no GUI
application registered** on the dev box (bare tty session). `Available()`/capability detection
and `Apps()` (correctly zero apps, not an error) exercised live; `Find`/`Perform` against a real
GUI app not exercised. Unit tests cover traversal/action logic against a fake D-Bus connection.
See [[pando/changes/uiauto_linux_atspi_phase2.md]].

### P3 — Input + screenshot layer — COMPLETE
**Verification: compile-verified only for the live-input paths** (this box has neither
`DISPLAY` nor `WAYLAND_DISPLAY`, so even the Linux X11 path never ran live here); the
no-session honesty behavior itself (`PLATFORM_NOT_SUPPORTED`, never a fake success) is
unit-tested. Windows/macOS: compile-verified only (`GOOS=windows|darwin go build`/`go vet`
clean). See [[pando/changes/uiauto_input_screen_phase3.md]].

### P4 — Windows UIA backend (go-ole) — COMPLETE
**Verification: COMPILE-VERIFIED ONLY. Never run against real Windows or a real COM
implementation.** Highest risk: vtable slot-index drift (every call site names its slot in a
comment for a first-run audit). See [[pando/changes/uiauto_windows_uia_phase4.md]].

### P5 — macOS AXUIElement backend (purego) — COMPLETE
**Verification: COMPILE-VERIFIED ONLY. Never run on real macOS.** Highest risk: CFRelease
discipline under sustained use. See [[pando/changes/uiauto_macos_ax_phase5.md]].

### P6 — Browser/CDP backend — COMPLETE
**Verification: FULLY LIVE-VERIFIED** against real headless Chrome on this machine — `Find`
(deduplicated), `Perform(invoke)` click, `SetValue`, and (Phase 7) real event subscription all
confirmed working end to end. The only backend with genuine live *action* verification. Required
a `cdproto`/`chromedp` dependency bump (2025-07 -> 2026-08) to fix a real upstream decode gap
plus two real bugs found only by running live (duplicate-node dedup, `describeNode`/click
node-id resolution). See [[pando/changes/uiauto_cdp_browser_phase6.md]] and
[[pando/changes/uiauto_cdp_browser_phase6_followup.md]].

### P7 — Vision fallback + events — COMPLETE
Vision fallback (`desktop_click_at`, `desktop_screenshot(grid:true)`) is pure Go/stdlib, unit
tested; its live status matches the input layer it rides on (P3). Events: **AT-SPI real
D-Bus-signal-driven** (unit-tested + honesty-gated live skip); **CDP genuinely live-verified**
(`TestIntegrationLiveChromeEventSubscribe` against real Chrome, found and fixed a real
`dom.GetDocument(WithDepth(-1))` requirement); **Windows/macOS event subscriptions were not
implemented** (not stubbed-and-hidden — `Capabilities.Events` correctly `false`), per the
explicit instruction not to claim an untestable implementation works; `desktop_wait` falls back
to polling there. See [[pando/changes/uiauto_vision_events_phase7.md]].

### P8 — Exposure + docs + e2e — COMPLETE (2026-08-29)
MCP exposure of the 12 desktop tools through `pando mcp-server` (`cmd/mcp_server.go`,
`buildMCPServerTools`), gated on the identical `InternalTools.DesktopEnabled` flag used
internally, confirmed as a pure external door (Pando's own agent loop never routes through MCP;
`internal/mcpgateway`'s builtin-redirect already handled desktop tool names correctly with no
change needed). New `docs/desktop-controller.md` (philosophy, all 12 tools, selector DSL,
qualified refs, error codes, every config key, an honest per-platform matrix, OS permission
prerequisites, vision fallback guardrails, plain security posture) plus a README Features
bullet. New `tests/test_desktop_controller_mcp_e2e.py` — 14 tests, builds the real `pando`
binary and drives the real MCP stdio JSON-RPC transport; **all 14 pass** against this machine's
real AT-SPI bus, asserting honest structured-error behavior (`APP_NOT_FOUND`, `INVALID_ARGS`,
`SNAPSHOT_NOT_FOUND`, `ACTION_FAILED`/`PLATFORM_NOT_SUPPORTED` for the display-less coordinate
click) rather than a fabricated GUI interaction. See
[[pando/changes/uiauto_exposure_docs_phase8.md]].

## 8. Risks — final status

- **Wayland**: confirmed — no global input/screenshot without portal consent; capabilities
  correctly report false/require live consent, never faked. (P3)
- **macOS via purego**: CFRelease discipline implemented with explicit `Release()`/`defer`
  patterns throughout; genuinely unverified on real hardware (never run). (P5)
- **AT-SPI performance**: selector-driven traversal implemented and unit-tested (branch pruning
  before child fetch); never load-tested against a dense real app (no GUI app was available on
  this box). (P2)
- **Cross-compile verification only**: held for P4 (Windows) and P5 (macOS) end to end — both
  compile- and vet-clean under their `GOOS`, never run.
- Accessibility-bus-disabled case: surfaces `PERM_DENIED` with the actionable
  `gsettings set org.gnome.desktop.interface toolkit-accessibility true` suggestion — implemented
  and unit-tested (P2).

## Final honest verification summary (whole feature, P0-P8)

- **Fully live-verified**: Browser/CDP backend (Chrome, real Find/Perform/SetValue/events);
  Phase 8's MCP exposure and e2e suite (real binary, real MCP stdio transport, 14/14 passing).
- **Smoke-verified, not action-verified**: Linux AT-SPI (real bus connection + honest empty
  enumeration; no GUI app ever available to click/type against on this box).
- **Compile-verified only, never run**: Windows UIA, macOS AX, and the Windows/macOS/X11/Wayland
  input+screen-capture paths.
- **Honestly unimplemented**: Windows UIA and macOS AX event subscriptions (reported via
  `Capabilities.Events == false`, not hidden).