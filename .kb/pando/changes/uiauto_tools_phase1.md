---
created_at: 2026-08-29T13:17:07.989623341Z
updated_at: 2026-08-29T13:17:07.989623341Z
---
# Pando Desktop Controller — Phase 1 (vertical slice) COMPLETE (2026-08-29)

Implements Phase 1 (P1) of [[pando/plans/desktop_controller_uiauto_plan.md]], building on
[[pando/changes/uiauto_core_phase0.md]] (`internal/uiauto/core`, unchanged — reused as-is, no
core modifications needed). Delivers the full vertical slice: Desktop Manager, config, the 11
agent tools, permissions, registration, and settings UI. No OS-specific backend code — only the
`core.NullBackend` is wired, so every tool answers `PLATFORM_NOT_SUPPORTED` cleanly, but the
whole pipeline (config -> manager -> tool -> structured response) is testable end to end. No
cgo. Everything OFF by default (`DesktopEnabled=false`). `internal/desktop` (Wails launcher) was
not touched.

## New package: `internal/uiauto`

- `internal/uiauto/backends.go` — process-wide `core.Registry` (`globalRegistry`), with `"null"`
  registered in `init()` and `SetAutoOrder("atspi","uia","ax","cdp","null")`. `Registry()`
  exposes it. Documented extension point: P2-P6 platform packages register themselves via their
  own `init()`, pulled in by a NEW per-GOOS build-tagged file added later (e.g.
  `backends_linux.go`) — this file and `manager.go` never need editing again.
- `internal/uiauto/manager.go` — `Manager` (backend, `*core.SnapshotStore`, `*core.
  ActionResolver`, `core.Capabilities`, `Options` snapshot) + `Options{Backend, MaxNodes,
  DefaultDepth, ActionTimeout, SnapshotTTL, AllowPhysicalInput, AllowedApps, DeniedApps,
  ScreenshotScale}`. `NewManager(opts) (*Manager, error)` resolves the backend via `Registry()`
  (falls back to `NewNullBackend()` on any resolution failure, so construction never fails).
  Methods every tool calls: `Apps`, `Windows`, `Observe(scope, depth) (*core.Snapshot, error)`
  (progressive `Children` traversal, own element-id assignment `e1..eN`, budget-capped by
  `MaxNodes`/depth), `Find(scope, selectorStr, limit) ([]*core.Element, *core.Snapshot, error)`,
  `Read(ref)`, `Click/Type/Key/Scroll/Focus(ref, ...)` (via the Phase-0 `ActionResolver`, no
  `PhysicalInput` wired yet — Phase 3), `Wait(scope, selectorStr, condition, timeout)` (via
  `core.WaitFor`), `Screenshot(target)` (always `PLATFORM_NOT_SUPPORTED` in Phase 1 — no screen
  backend exists until Phase 3). Policy: `checkAppPolicy(id, name)` — deny-list wins, then an
  empty allow-list means "all apps", a non-empty one requires a case-insensitive id/name match;
  returns `core.NewPolicyDeniedError`. `Shared()` is the process-wide singleton mirroring
  `browser_session.go`'s pattern, built from `config.Get().InternalTools` via
  `OptionsFromConfig`, rebuilt automatically when the relevant Desktop* config fields change
  (tracked by a `desktopOptionsKey` comparison), with `ResetShared()` for tests.
- `internal/uiauto/manager_test.go` — `fakeBackend` (pointer-identity-based `Children`, deterministic
  `Find`/`Windows`/`Apps`/`Perform`) covers: null-backend fallback + `PLATFORM_NOT_SUPPORTED`;
  `Screenshot` always `PLATFORM_NOT_SUPPORTED`; `Observe` tree building with depth cap and
  `MaxNodes` budget, snapshot-store resolvability; window-not-found -> `ELEMENT_NOT_FOUND`;
  policy deny-list and allow-list (`Windows`, `Apps` filtering); `Find`+`Read`+all five action
  methods succeeding natively through a fake `Perform`; invalid selector -> `INVALID_ARGS`;
  stale/unknown ref -> `SNAPSHOT_NOT_FOUND`; `Wait` timeout -> `TIMEOUT`; global (ref-less) `Key`
  without a `PhysicalInput` -> `PLATFORM_NOT_SUPPORTED`.

## Config (`internal/config/config.go`, `init.go`, `cmd/init.go`)

Added to `InternalToolsConfig`: `DesktopEnabled` (bool, default false), `DesktopBackend` (string,
default `"auto"`), `DesktopAllowPhysicalInput` (bool, default true), `DesktopMaxNodes` (int,
default 500), `DesktopDefaultDepth` (int, default 3), `DesktopActionTimeout` (int seconds,
default 10), `DesktopSnapshotTTL` (int seconds, default 60), `DesktopScreenshotScale` (float64,
default 1.0), `DesktopAllowedApps`/`DesktopDeniedApps` ([]string). Viper defaults added next to
the browser ones. Both `.pando.toml` templates (`internal/config/init.go`'s project template and
`cmd/init.go`'s global template) document the new `[InternalTools]` keys.
`pando-schema.json` has no `InternalTools`/`Browser` section at all (checked — nothing to
mirror there for this feature).

## The 11 tools (`internal/llm/tools/desktop_*.go`)

`desktop_apps`, `desktop_observe`, `desktop_find`, `desktop_read`, `desktop_click`,
`desktop_type`, `desktop_key`, `desktop_scroll`, `desktop_focus`, `desktop_wait`,
`desktop_screenshot` — one file each, following the `Browser*Tool`/`ImageCropTool` shape
(`ToolInfo`, `DecodeToolInput`, `NewStructuredResponse`). Shared helpers in
`internal/llm/tools/desktop_common.go`: `desktopManager()` (wraps `uiauto.Shared()`, converting
a non-DesktopError failure like "config not loaded" into `PLATFORM_NOT_SUPPORTED`),
`desktopErrorResponse(err)` (renders any error — wrapping as `ACTION_FAILED` if it isn't already
a `*core.DesktopError` — as the `{ok:false,error:{code,message,suggestion}}` structured payload,
`IsError: true`; every `core.DesktopError` from the manager is surfaced this way, never a bare Go
error string), `desktopPermDeniedResponse`/`requestDesktopPermission` (permission-gate helper),
`desktopScopeParams`/`desktopScopeProperties` (shared `app_id`/`window_id` targeting).
`desktop_observe`/`desktop_find` render via `core.RenderTree`/`core.RenderElements` plus the
snapshot id, so refs come back as `@sXXXX:eN`. `desktop_screenshot` returns
`ToolResponseTypeImage` (base64 PNG normalized through `internal/imageopt.Normalize`), exactly
like `browser_screenshot.go`. Mutating tools (`click`, `type`, `key`, `scroll`, `focus`) take
`permission.Service` and call `permissions.Request` before acting; read-only tools do not prompt
**except** `desktop_screenshot`, which does (captures the whole screen). Session id comes from
`ctx` via the existing `SessionIDContextKey`/`GetContextValues` pattern.

Tests: `internal/llm/tools/desktop_tools_test.go` — decode-error path, `INVALID_ARGS` for missing
required params, `PLATFORM_NOT_SUPPORTED` end-to-end through the real `uiauto.Shared()` singleton
forced onto the `"null"` backend via `config.SetForTests`/`uiauto.ResetShared()`, `SNAPSHOT_NOT_FOUND`
for a stale ref, `INVALID_ARGS` for a bad wait condition, and `PERM_DENIED` vs. granted-then-
backend-error for every mutating tool plus `desktop_screenshot`, using a real
`permission.NewPermissionService()` with either `SetGlobalAutoApprove(true)` or a session handler
returning `false`.

## Registration

- `internal/llm/tools/builtin_names.go` — all 11 `Desktop*ToolName` constants added to
  `builtinToolNames` (verified by `TestDesktopToolsAreBuiltin` in `builtin_names_test.go`).
- `internal/llm/agent/tools.go` — a `if it.DesktopEnabled { ... }` block appending all 11
  constructors was added in **both** `CoderAgentTools()` and `CoderAgentToolsWithMesnada()`,
  mirroring the existing `it.BrowserEnabled` blocks exactly (same append pattern, same
  `permissions` argument passed to the five mutating constructors).
  `internal/llm/agent/desktop_tools_test.go`'s `TestDesktopToolsGatedByConfig` asserts zero
  `desktop_*` tools when `DesktopEnabled=false` and all 11 when true, via `CoderAgentTools`.
  `cmd/mcp_server.go` (external MCP gateway exposure) was deliberately **not** touched — that is
  Phase 8 per the plan (internal architecture stays a tool provider, not an external MCP server,
  until P8).

## Settings UI

- `internal/api/handlers_config.go` — `ToolsConfigResponse` DTO gained the 10 `Desktop*` fields
  (GET populates them from `cfg.InternalTools`; PUT already flows arbitrary `InternalToolsConfig`
  fields through by construction, plus an explicit `DesktopBackend` empty-string fallback to
  `"auto"` mirroring the existing `BrowserType` fallback).
- `internal/tui/page/settings.go` — 10 new fields in `buildInternalToolsSection` (toggle/select/
  text, following the Browser Automation block's shape) and matching `case` branches in
  `saveInternalTools`. Added two small helpers: `parseFloatValue` (didn't exist yet) and
  `splitCommaList` (comma-separated list parsing, mirroring the existing `Split(field.Value,
  ",")` pattern used by `outputFilterPaths`).
- `web-ui/packages/pando-client/src/types/index.ts` (`ToolsConfig`) and `stores/settingsStore.ts`
  (`TOOLS_DEFAULTS`) gained the 10 `desktop*` fields. `web-ui/src/components/settings/
  InternalToolsSettings.tsx` gained a "Desktop Controller (Accessibility Automation)" `ToolCard`
  with a backend `SelectInput`, a physical-input-fallback `Toggle`, five numeric `TextInput`s, and
  two comma-separated allow/deny-app `TextInput`s — same structure as the existing Browser card.

## Verification

- `go build ./...` — clean, whole repo, no regressions.
- `go vet ./internal/uiauto/... ./internal/llm/tools/... ./internal/llm/agent/...` — clean.
  (`go vet ./...` shows one pre-existing, unrelated finding in
  `internal/mesnada/agent/spawner_template.go`, not touched by this change.)
- `go test ./internal/uiauto/... ./internal/llm/tools/... ./internal/llm/agent ./internal/api
  ./internal/config` — all pass.
- `gofmt -l internal/uiauto internal/llm/tools internal/llm/agent internal/config internal/api`
  — empty for every file this change touched (3 pre-existing, untouched files elsewhere in
  `internal/llm/tools` were already gofmt-dirty before this change and are unrelated).
- `web-ui`: `bun x tsc -b` (the type-check step of `npm run build`) — clean, no errors.
  `vite build` itself was not run (not requested; typecheck is the meaningful gate here).
- No cgo introduced; verified `internal/uiauto` has zero `import "C"`.

## Manager API contract for P2-P5 backends

Platform packages implement `core.Backend` (`Name, Available, Apps, Windows, Find, Children,
Properties, Perform, Close` — unchanged from Phase 0) and register themselves in `internal/
uiauto`'s `Registry()` from their own `init()`, in a new per-GOOS build-tagged file (e.g.
`internal/uiauto/backends_linux.go` with `//go:build linux`, blank-importing `internal/uiauto/
platform/linux`). `Manager`, `backends.go` and every desktop_* tool need zero changes for a new
backend to light up — only `Options.Backend`/`DesktopBackend` needs to resolve to the new name
(or `"auto"`, already in the preference order).

## Deferred (out of Phase 1 scope, per the plan)

- No `internal/uiauto/platform/{linux,darwin,windows}`, `internal/uiauto/input`, `internal/
  uiauto/screen`, `internal/uiauto/browser` — P2-P6.
- `Manager.Screenshot` has no real capture path yet (P3); `ActionResolver`'s physical-input
  fallback is wired but `PhysicalInput` is `nil` (P3).
- No MCP-gateway external exposure of desktop tools (P8) — `cmd/mcp_server.go` untouched.
- Vision fallback and UI-event subscriptions are P7.