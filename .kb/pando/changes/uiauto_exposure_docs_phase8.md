---
created_at: 2026-08-29T19:51:10.769236048Z
updated_at: 2026-08-29T19:51:10.769236048Z
---
# Pando Desktop Controller — Phase 8 (MCP exposure + docs + e2e) COMPLETE (2026-08-29)

Implements Phase 8 (P8), the FINAL phase, of [[pando/plans/desktop_controller_uiauto_plan.md]],
building on Phases 0-7 ([[pando/changes/uiauto_core_phase0.md]],
[[pando/changes/uiauto_tools_phase1.md]], [[pando/changes/uiauto_linux_atspi_phase2.md]],
[[pando/changes/uiauto_input_screen_phase3.md]], [[pando/changes/uiauto_windows_uia_phase4.md]],
[[pando/changes/uiauto_macos_ax_phase5.md]], [[pando/changes/uiauto_cdp_browser_phase6.md]],
[[pando/changes/uiauto_cdp_browser_phase6_followup.md]],
[[pando/changes/uiauto_vision_events_phase7.md]]). Three deliverables: MCP exposure as an
EXTERNAL interface only, documentation, and real e2e tests under `tests/`.

## 1. MCP exposure

`cmd/mcp_server.go`'s `buildMCPServerTools` gained a `if it.DesktopEnabled { ... }` block
(mirroring the existing `it.BrowserEnabled` block exactly) that appends all 12 desktop tool
constructors (`NewDesktopAppsTool` ... `NewDesktopClickAtTool`, the five mutating ones plus
`desktop_screenshot`/`desktop_click_at` taking `appSvc.Permissions`) to the tool list `pando
mcp-server` exposes over stdio/HTTP to **external** MCP clients, gated on the identical
`InternalTools.DesktopEnabled` flag used by `internal/llm/agent/tools.go`'s internal
registration. This is a pure *external exposure* addition — no internal call path was touched:
Pando's own agent loop still constructs and calls these tools directly, with zero MCP hop.

Verified (not modified, confirmed already correct) that the internal architecture stays a tool
provider and MCP never shadows it: `internal/llm/tools/builtin_names.go` already lists all 12
`Desktop*ToolName` constants (including `desktop_click_at`, added in Phase 7) in
`builtinToolNames`, and `internal/mcpgateway/proxy_tools.go`'s `tools.IsBuiltinTool(...)` check
already redirects any model attempt to route a desktop tool through `mcp_call_tool` with a clear
message instead of a confusing "tool not found" — this pre-existing gateway behavior needed no
change for the desktop tools to be handled correctly.

Files touched: `cmd/mcp_server.go` (one new block in `buildMCPServerTools`, ~25 lines). No changes
to `internal/mcpgateway/*`, `internal/llm/agent/tools.go`, `internal/uiauto/**`, or any
`desktop_*.go` tool file.

**Caveat documented in the new docs page**: `pando mcp-server` calls
`Permissions.SetGlobalAutoApprove(true)` at startup (pre-existing behavior, shared with every
other mutating MCP-exposed tool — bash, write/edit/patch), so over MCP every desktop permission
request is auto-approved rather than interactively prompted.

## 2. Documentation

New `docs/desktop-controller.md`, following the repo's existing `docs/<feature>.md` + linked
README bullet convention (matched against `docs/mcp-authentication.md`/`docs/output-filters.md`
and their README.md references). Covers: the accessibility-first philosophy and 3-tier
native-action -> physical-fallback -> vision-fallback design; all 12 tools with parameters and
return shapes; the selector DSL grammar (role/attr predicates/pseudo-filters/nth/combinators)
with examples; qualified refs (`@snapshotId:elementId`) and the STALE_REF/SNAPSHOT_NOT_FOUND/
ELEMENT_NOT_FOUND distinction; the full `DesktopError` code table; every `InternalTools.Desktop*`
config key with defaults and a `.pando.toml` example; a per-platform support matrix with an
HONEST status column (Linux AT-SPI smoke-verified against a live a11y bus but no GUI app
registered; Windows UIA and macOS AX compile-verified only, never run; Browser/CDP fully
live-verified; events real on AT-SPI/CDP, polling fallback on Windows/macOS); OS permission
prerequisites (macOS Accessibility + Screen Recording, Wayland portal consent, Linux
`toolkit-accessibility`); the vision fallback's `source:"vision"` marking and coordinate-bounds
guardrail; and a plain security-posture section (off by default, every mutating action and every
screenshot prompts, `DesktopAllowedApps`/`DesktopDeniedApps` scoping, the MCP auto-approve
caveat). Added a matching one-paragraph bullet to `README.md`'s Features list linking to the doc.

## 3. E2E tests

New `tests/test_desktop_controller_mcp_e2e.py`, following the repo's actual `tests/` convention
(confirmed: Python `unittest`, some purely logic-simulation like `test_tool_brave_search.py`,
others (`test_cronjob_cli.py`) genuinely build the `pando` binary via `go build` and drive it as
a real subprocess — this new test follows the latter, real-binary pattern since the task asked
for the real interface).

The test builds the real `pando` binary once (`setUpModule`), then for every test spawns
`pando mcp-server --no-http --cwd <tmpdir>` against a temp project with its own `.pando.toml`
and drives the real newline-delimited JSON-RPC 2.0 stdio transport (`initialize`, `tools/list`,
`tools/call`) — the exact interface an external MCP client uses, discovered by reading
`internal/mesnada/server/server.go`'s `runStdio`/`handleRequest` and confirmed against
`server_pando_tools_test.go`'s existing JSON-RPC shape.

14 tests, two classes:

- `TestDesktopControllerMCPGating` (6 tests): `desktop_*` tools are completely absent from
  `tools/list` with `DesktopEnabled=false` (both explicit and the field-omitted default), all 12
  present with `DesktopEnabled=true`, `desktop_click`/`desktop_find` input schemas require
  `ref`/`selector`, and enabling Desktop doesn't crowd out unrelated builtin tools
  (`cache_read` still present).
- `TestDesktopControllerStructuredErrors` (8 tests): real tool calls against the real backend
  resolved on this dev box (AT-SPI, since a real `org.a11y.Bus` is reachable here) —
  `desktop_apps` returns a well-formed `{ok:true, apps:[], windows:[]}` (honest empty result, no
  GUI app registered) or an honest `PLATFORM_NOT_SUPPORTED`; `desktop_find` against a nonexistent
  app reports `APP_NOT_FOUND`; a malformed selector reports `INVALID_ARGS`; an unknown qualified
  ref reports `SNAPSHOT_NOT_FOUND`; `desktop_click_at` on this display-less tty session never
  reports `ok:true` (asserted explicitly — it must not lie about a coordinate click landing
  anywhere real) and instead surfaces `ACTION_FAILED`/`PLATFORM_NOT_SUPPORTED`/`POLICY_DENIED`;
  an out-of-range coordinate and an unknown tool name both fail cleanly. Every assertion accepts
  the honest range of codes a headless/tty CI box can actually produce (documented in the file's
  module docstring) rather than asserting a single happy path — per the task, this never
  pretends a headless box can click a real GUI button.

**Real output** (`python3 -m unittest tests.test_desktop_controller_mcp_e2e -v`): all 14 tests
pass in ~28s. Confirmed live against this machine's real AT-SPI bus: `desktop_apps` returned
`ok:true` with empty `apps`/`windows` (correct — no GUI app registered); `desktop_find` against a
bogus app id returned `APP_NOT_FOUND` with a suggestion to call `desktop_apps` first;
`desktop_click_at` with `DesktopAllowPhysicalInput=true` on this display-less tty returned
`ACTION_FAILED` wrapping `"PLATFORM_NOT_SUPPORTED: no X11 or Wayland display session is
available (DISPLAY/WAYLAND_DISPLAY unset)"` — exactly the honest Phase 3 behavior.

**Incidental finding (not a bug fixed in this phase, documented for future reference)**: a
project `.pando.toml` `[InternalTools]` table that sets only `DesktopEnabled = true` without also
setting `DesktopAllowPhysicalInput` observably behaves as if physical-input fallback were
disabled (`POLICY_DENIED` on `desktop_click_at`), even though `viper.SetDefault(...,
"desktopAllowPhysicalInput", true)` is registered — apparently because `mergeLocalConfig`'s
`viper.MergeConfigMap(local.AllSettings())` merges the local file's partial `internalTools`
table in a way that doesn't reliably preserve unrelated global defaults for sibling keys in that
same nested table. This is pre-existing `internal/config` merge behavior (not specific to
Desktop, not introduced or touched by this phase) — worth a project's `.pando.toml` explicitly
setting every `Desktop*` key it cares about rather than relying on partial-table defaults, and
worth a future config-package investigation. The e2e test's structured-error class sets every
`Desktop*` key explicitly to sidestep this and get deterministic behavior.

## Verification

- `go build ./...` — clean, whole repo.
- `go vet ./internal/uiauto/... ./internal/mcpgateway/...` — clean.
- `go test ./...` — all packages pass, no FAIL anywhere (grepped the full run).
- `GOOS=windows go build ./internal/uiauto/...` / `GOOS=darwin go build ./internal/uiauto/...` —
  both clean.
- `GOOS=windows|darwin go build ./cmd/...` — fails only for the pre-existing, unrelated
  `github.com/madeindigio/go-tree-sitter` cgo-only reason (confirmed identical failure signature
  to every prior phase's note); `cmd/mcp_server.go` itself is not the cause (pure Go/no OS-
  specific code was added).
- `gofmt -l internal/uiauto internal/llm/tools internal/mcpgateway cmd` — only the three
  pre-existing unformatted files already called out in every prior phase
  (`aliases.go`, `lua_tools.go`, `remembrances_code_test.go`) plus one pre-existing, untouched
  `cmd/test_ollama_main/main.go` — nothing new from this phase.
- `python3 -m unittest tests.test_desktop_controller_mcp_e2e -v` — **14/14 pass**, ~28s, against
  the real built binary and the real MCP stdio transport.

## Honest end-to-end status of the whole 9-phase (P0-P8) feature

- **Fully live-verified**: Browser/CDP backend (Find/Perform/SetValue against real headless
  Chrome, plus real event subscription) — the only backend with genuine live *action*
  verification; Phase 8's MCP exposure and the new e2e suite (both genuinely exercised against a
  real built binary and a real stdio JSON-RPC transport, not simulated).
- **Smoke-verified, not action-verified**: Linux AT-SPI (real `org.a11y.Bus` connection and
  `Apps()` enumeration confirmed live on this box; no GUI application was ever available on this
  session to click/type against).
- **Compile-verified only, never run**: Windows UIA, macOS AX, and the Windows/macOS/X11/Wayland
  input and screen-capture paths (this dev box has neither Windows/macOS hardware nor a live
  X11/Wayland display session — even the Linux X11 input/screen paths never ran live here).
- **Unimplemented, honestly reported as such**: Windows UIA and macOS AX event subscriptions
  (`Capabilities.Events` correctly `false`; `desktop_wait` falls back to polling).