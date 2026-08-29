# Desktop Controller (accessibility-tree UI automation)

The Desktop Controller (`internal/uiauto`) lets the agent observe and drive the graphical
desktop — click buttons, fill fields, read window content, take screenshots — by talking to the
OS accessibility APIs, not by staring at pixels. It is exposed to the agent as 12
`desktop_*` tools (`internal/llm/tools/desktop_*.go`) and, when enabled, through Pando's own MCP
server (`pando mcp-server`) for external MCP clients.

Off by default. Read [Security posture](#security-posture) before turning it on.

- [Philosophy: accessibility-first, not screenshot-first](#philosophy-accessibility-first-not-screenshot-first)
- [Backend routing: OS accessibility vs. the browser](#backend-routing-os-accessibility-vs-the-browser)
- [`desktop_*` vs. `browser_*`](#desktop_-vs-browser_)
- [The 12 tools](#the-12-tools)
- [Selector DSL](#selector-dsl)
- [Qualified refs and STALE_REF](#qualified-refs-and-stale_ref)
- [Structured errors](#structured-errors)
- [Configuration](#configuration)
- [Platform support matrix (honest status)](#platform-support-matrix-honest-status)
- [OS permission prerequisites](#os-permission-prerequisites)
- [Vision fallback](#vision-fallback)
- [Events](#events)
- [MCP exposure](#mcp-exposure)
- [Security posture](#security-posture)

## Philosophy: accessibility-first, not screenshot-first

Most "computer use" agent implementations work by screenshotting the screen, asking a
vision model to guess pixel coordinates, and clicking blind. Pando's Desktop Controller instead
reads the OS accessibility tree — the same semantic tree screen readers use — and gets back
structured elements: role, name, value, bounds, enabled/visible/focused state, and the actions
each element supports. The agent addresses elements as elements ("the Save button"), not as
coordinates, which is both far cheaper in tokens (a rendered tree/selector match is a few lines;
a screenshot is thousands of vision tokens) and far more reliable (accessibility actions don't
miss because a window moved or a dialog animated).

Screenshots and physical mouse/keyboard input are **fallbacks only**, used in this order:

1. **Native accessibility action** (e.g. AT-SPI `Action.DoAction`, UIA `InvokePattern`, AX
   `AXUIElementPerformAction`, CDP `Runtime.callFunctionOn(...click())`) — tried first for every
   mutating tool.
2. **Physical (synthetic) input** at the element's resolved bounds — used automatically when the
   native action is unsupported or fails, only if `DesktopAllowPhysicalInput` is true.
3. **Vision fallback** (`desktop_screenshot` + `desktop_click_at`) — a last resort for UI that
   exposes no usable accessibility semantics at all (canvas apps, games, broken a11y
   implementations, remote-desktop windows rendered as one opaque surface). Every result from
   this path is marked `source:"vision"` in its response so it's always distinguishable from a
   semantic action.

## Backend routing: OS accessibility vs. the browser

`internal/uiauto.Manager` holds the set of backends actually available in this session, not a
single resolved one, and routes each operation to whichever backend should serve it:

- A scope naming the connected browser's virtual app (`app_id: "browser"`, or a `window_id`
  that's one of its live pages) routes to the **CDP** backend.
- Everything else routes to the **OS accessibility backend** (`atspi`/`uia`/`ax`, or `null` when
  none is available).
- `DesktopBackend` set to anything other than `"auto"`/empty is a **hard pin**: routing is
  disabled entirely and every operation — browser-scoped or not — goes to that one backend. Set
  it to `"atspi"`/`"uia"`/`"ax"` to force native-only, or `"cdp"` to force browser-only.
- An element ref remembers which backend produced it (`desktop_observe`/`desktop_find`'s
  response), so a later `desktop_click`/`desktop_read`/etc. on that ref always routes back to the
  *same* backend that returned it, even across snapshots and regardless of what a fresh scope
  would currently resolve to.
- Resolving/holding the CDP backend is free of side effects: it never launches a browser. When no
  `browser_*` session is open, browser-scoped operations answer `APP_NOT_FOUND` (suggesting
  `browser_navigate`) exactly like every other "app not found" case — routing never changes that.
- `desktop_apps`/`desktop_observe` (with no `app_id`) surface the connected browser as one more
  app alongside native ones, when a session is open; nothing is added when it isn't.

This means an open browser window shows up in `desktop_apps` as app id `"browser"`, one window
per open page/tab, and can be driven with `desktop_observe`/`desktop_find`/`desktop_click`/etc.
exactly like a native application — see the next section for when to prefer that over `browser_*`.

## `desktop_*` vs. `browser_*`

Both tool families can now act on the same open web page, so pick based on what you're doing:

- **`browser_*`** (`browser_navigate`, `browser_click`, `browser_fill`, `browser_evaluate`,
  `browser_get_content`, `browser_screenshot`, `browser_scroll`, `browser_console_logs`,
  `browser_network`, `browser_pdf`) is **scripted page automation**: CSS-selector-precise,
  reads/writes actual DOM/HTML, runs arbitrary JavaScript, inspects network/console activity. Use
  it whenever you already know the selector/URL/JS you need, or need something only the DOM can
  give you (exact markup, computed values, network requests).
- **`desktop_*`** treats the browser as **one application among many on the desktop**, addressed
  semantically (accessibility role/name) exactly like any native app. Use it when the browser is
  part of a mixed workflow that also touches native windows/dialogs (a file picker, a native
  "Save As" dialog a page triggered, switching OS focus between the browser and another app), or
  when you don't have a CSS selector and want to find something by its accessible name/role
  instead.

Each tool's own description says explicitly which of these applies to it — see
[The 12 tools](#the-12-tools) below and the `browser_*` tool descriptions.

## The 12 tools

| Tool | Mutating? | Prompts for permission? | Purpose |
|---|---|---|---|
| `desktop_apps` | no | no | List running apps and their top-level windows. Cheap, no tree walk. Always call this first. |
| `desktop_observe` | no | no | Snapshot an app/window's accessibility tree (depth-capped), returns a compact indented tree with qualified refs. |
| `desktop_find` | no | no | Resolve a selector to matching elements, without rendering a whole tree. |
| `desktop_read` | no | no | Read one element's full details (role/name/value/description/bounds/actions/native data) by ref. |
| `desktop_click` | yes | yes | Click an element by ref: native Invoke, physical-click fallback. |
| `desktop_type` | yes | yes | Enter text into an element by ref: native SetValue, then native Type, then physical keyboard fallback. |
| `desktop_key` | yes | yes | Send a key or chord (`"Ctrl+S"`, `"Enter"`), targeted at an element or sent globally. |
| `desktop_scroll` | yes | yes | Scroll an element by a signed amount: native Scroll, physical-scroll fallback. |
| `desktop_focus` | yes | yes | Focus an element/window: native Focus, physical-click fallback. |
| `desktop_wait` | no | no | Wait for a selector to satisfy a condition (`exists`/`notexists`/`visible`/`enabled`/`focused`) instead of polling manually. Event-driven on backends that support it (AT-SPI, CDP), polling elsewhere. |
| `desktop_screenshot` | no (but reads the whole screen) | **yes** | Capture the screen, a window, or an element's crop. Optional `grid:true` overlays real-pixel coordinate labels for use with `desktop_click_at`. |
| `desktop_click_at` | yes | **yes**, worded as a blind action | Click a raw `(x,y)` screen coordinate — the vision fallback. Validated against real display bounds; response is always marked `source:"vision"`. |

Every read-only tool that doesn't touch the screen is prompt-free; `desktop_screenshot` prompts
even though it's non-mutating because it captures the user's whole screen. All five mutating
element-action tools and `desktop_click_at` go through `permission.Service.Request(...)` before
acting — see [Security posture](#security-posture).

### `desktop_apps`

No parameters. Returns `{ok:true, apps:[{id,name,pid,windows}], windows:[{id,appId,title,bounds,focused}]}`.

### `desktop_observe`

Parameters: `app_id` (optional), `window_id` (optional), `depth` (optional int, defaults to
`DesktopDefaultDepth`). Returns `{ok:true, snapshotId, tree}` where `tree` is a compact indented
render, one element per line:

```
WINDOW "Settings" (app: chrome)
  group
    button "Save" [enabled]
      @s8f3k2p9:e17 button "Save" [enabled]
```

### `desktop_find`

Parameters: `selector` (required, see [Selector DSL](#selector-dsl)), `app_id`/`window_id`
(optional scope), `limit` (optional). Returns `{ok:true, snapshotId, count, elements}` with
matches rendered the same way as `desktop_observe`.

### `desktop_read`

Parameters: `ref` (required, a qualified ref like `@s8f3k2p9:e17`). Returns
`{ok:true, element:{role,name,value,description,bounds,enabled,visible,focused,actions,native}}`.

### `desktop_click` / `desktop_focus`

Parameters: `ref` (required). Returns `{ok:true, method:"native"|"physical", notes:[...]}`.

### `desktop_type`

Parameters: `ref` (required), `text` (required). Returns `{ok:true, method, notes}`.

### `desktop_key`

Parameters: `key` (required, e.g. `"Enter"`, `"Escape"`, `"Ctrl+S"`, `"Alt+F4"`), `ref` (optional
— omit to send the key globally, which requires physical input to be allowed). Returns
`{ok:true, method, notes}`.

### `desktop_scroll`

Parameters: `ref` (required), `amount` (required int, signed — positive scrolls down/right,
negative scrolls up/left). Returns `{ok:true, method, notes}`.

### `desktop_wait`

Parameters: `selector` (required), `condition` (required, one of `exists`/`notexists`/
`visible`/`enabled`/`focused`), `app_id`/`window_id` (optional scope), `timeout` (optional
seconds, defaults to `DesktopActionTimeout`). Returns `{ok:true, element}` (element omitted for
`notexists`) or a `TIMEOUT` error.

### `desktop_screenshot`

Parameters: `target` (optional — `"screen"` default, `"window:<id>"`, or a qualified element ref
to crop to its bounds), `grid` (optional bool, default false — overlays a coordinate grid with
real-pixel axis labels). Returns an image content block (normalized through
`internal/imageopt`, the same pipeline as `browser_screenshot`).

### `desktop_click_at`

Parameters: `x`, `y` (both required ints, real unscaled screen pixels). Coordinates are validated
against the actual captured display bounds before anything reaches the OS. Returns
`{ok:true, method:"physical", source:"vision"}`.

## Selector DSL

A small CSS-inspired grammar (`internal/uiauto/core/selector.go`) — **not real CSS**:

```
role[attr op "value"] : pseudo [nth=N]  descendant-combinator (space)
role > role                              child-combinator
"bare quoted text"                       shorthand for [name="text"] on any role
```

- **Role**: a normalized role token (`button`, `textfield`, `link`, `menuitem`, `checkbox`,
  `group`, `window`, `dialog`, ...) or `*` for any role. `Role.Matches` also accepts common
  aliases (e.g. `textbox`/`edit`/`input` all match `textfield`; `push`/`pushbutton` match
  `button`).
- **Attribute predicates** `[attr op "value"]`: `attr` ∈ `{name, value, description, id, role,
  class}`; `op` ∈ `{=, ^= (prefix), $= (suffix), *= (contains), ~= (regexp)}`.
- **Pseudo-filters**: `:visible`, `:enabled`, `:focused`, `:disabled`, `:hidden`.
- **`[nth=N]`** (1-indexed): honored for child-combinator (`>`) steps, where sibling context is
  available during traversal; a descendant-combinator step with `[nth=N]` has no sibling context
  at that point and is left unfiltered — a documented limitation, not silently wrong.
- **Combinators**: a space is the descendant combinator (matches at any depth below), `>` is the
  direct-child combinator.
- **Bare-quoted shorthand**: `"Save"` alone means `[name="Save"]` on any role.

Examples:

```
button[name="Save"]
textfield[name^="Search"]
group > button
button:enabled:visible
app[name="Chrome"] window[name="Settings"] button[name="New Tab"]
"Save"
button[name~="^(OK|Yes)$"]
```

Traversal is selector-driven on every backend that implements it (AT-SPI, UIA, AX, CDP): a
branch that can no longer satisfy any pending selector step is pruned without even fetching its
children, so `desktop_find` never does a full accessibility-tree walk of a dense app.

## Qualified refs and STALE_REF

Every element handed back by `desktop_observe`/`desktop_find` is identified by a **qualified
ref**: `@<snapshotId>:<elementId>`, e.g. `@s8f3k2p9:e17`. The snapshot id is always part of the
ref specifically so that `e1` from one snapshot can never be confused with `e1` from another —
two different observations of the same window get two different snapshot ids even if their
element numbering overlaps.

Each ref stores the locator that produced it, so `desktop_click`/`desktop_type`/etc. re-resolve
the *live* element immediately before acting rather than trusting stale coordinates — the
snapshot is a reference for addressing, not a cached copy that's acted on directly.

A ref becomes invalid, and any tool that receives it responds
`{ok:false, error:{code:"STALE_REF", ...}}`, when:

- the snapshot has exceeded `DesktopSnapshotTTL` seconds since it was taken, or
- the snapshot store evicted it under LRU pressure (bounded snapshot count), or
- re-resolution can't find that element anymore (returns `ELEMENT_NOT_FOUND` instead — a live but
  no-longer-matching element vs. an unknown/expired snapshot are reported as different codes).

An unknown snapshot id (never issued, or evicted so completely its id itself is gone) reports
`SNAPSHOT_NOT_FOUND`; a malformed ref string reports `INVALID_ARGS`. The fix in all cases is the
same: call `desktop_observe`/`desktop_find` again to get a fresh ref.

## Structured errors

Every desktop tool failure is `{ok:false, error:{code, message, suggestion}}` — never a bare Go
error string. Codes (`internal/uiauto/core/errors.go`):

| Code | Meaning |
|---|---|
| `PERM_DENIED` | OS-level permission missing (accessibility bus disabled, macOS Accessibility/Screen Recording not trusted, Wayland portal consent refused) or the user declined the in-agent permission prompt. |
| `ELEMENT_NOT_FOUND` | The ref/selector no longer resolves to a live element. |
| `APP_NOT_FOUND` | No application/window matches the given `app_id`/`window_id` (or, for the CDP backend, no browser session is registered). |
| `STALE_REF` | The snapshot behind a ref expired (TTL) or was evicted. |
| `SNAPSHOT_NOT_FOUND` | The snapshot id itself is unknown. |
| `POLICY_DENIED` | Blocked by `DesktopAllowedApps`/`DesktopDeniedApps`, or physical input required but `DesktopAllowPhysicalInput` is false. |
| `ACTION_FAILED` | The native action failed and no physical fallback was possible/allowed. |
| `PLATFORM_NOT_SUPPORTED` | No backend, or this specific capability, is available on this platform/session. |
| `TIMEOUT` | `desktop_wait` exceeded its timeout. |
| `INVALID_ARGS` | Malformed selector, missing required parameter, or (for `desktop_click_at`) an out-of-bounds coordinate. |

## Configuration

All under `[InternalTools]` in `.pando.toml` (or the matching `internalTools.desktop*` JSON keys):

| Key | Default | Meaning |
|---|---|---|
| `DesktopEnabled` | `false` | Master switch. Registers the 12 tools with the agent (and, if `pando mcp-server` is run, with the MCP tool list) only when true. |
| `DesktopBackend` | `"auto"` | `auto`, `atspi` (Linux), `uia` (Windows), `ax` (macOS), `cdp` (browser), or `null`. `auto` resolves the OS backend (`atspi, uia, ax, null` in order) **and** the `cdp` backend independently, then routes each operation between them per scope — see [Backend routing](#backend-routing-os-accessibility-vs-the-browser). Any other value is a hard pin that disables routing: every operation uses that one backend. |
| `DesktopAllowPhysicalInput` | `true` | Lets the `ActionResolver` fall back to synthetic mouse/keyboard when a native accessibility action is unsupported or fails, and gates global (ref-less) `desktop_key` and `desktop_click_at` entirely. |
| `DesktopMaxNodes` | `500` | Caps how many elements a single `desktop_observe`/`desktop_find` renders; the rest are reported as a truncation notice, not silently dropped. |
| `DesktopDefaultDepth` | `3` | Default tree depth `desktop_observe` descends when `depth` isn't given. |
| `DesktopActionTimeout` | `10` (seconds) | Default timeout for a single action/`desktop_wait`. |
| `DesktopSnapshotTTL` | `60` (seconds) | How long a snapshot (and the refs into it) stay resolvable before `STALE_REF`. |
| `DesktopScreenshotScale` | `1.0` | Resize factor applied to `desktop_screenshot` output before it reaches the model (Lanczos resize); `1.0` = full resolution. |
| `DesktopAllowedApps` | `[]` (all apps) | When non-empty, restricts every desktop tool to these app ids/names (case-insensitive). |
| `DesktopDeniedApps` | `[]` | App ids/names blocked regardless of `DesktopAllowedApps` — deny always wins. |

Example `.pando.toml`:

```toml
[InternalTools]
DesktopEnabled            = false
DesktopBackend            = 'auto'
DesktopAllowPhysicalInput = true
DesktopMaxNodes           = 500
DesktopDefaultDepth       = 3
DesktopActionTimeout      = 10
DesktopSnapshotTTL        = 60
DesktopScreenshotScale    = 1.0
DesktopAllowedApps        = []
DesktopDeniedApps         = []
```

Also configurable from the TUI (`Settings → Internal Tools → Desktop Controller`) and the WebUI
(`InternalToolsSettings` → "Desktop Controller (Accessibility Automation)" card).

## Platform support matrix (honest status)

| Platform / backend | Implemented | Live-verified | Notes |
|---|---|---|---|
| **Linux — AT-SPI2** (`platform/linux`, godbus) | Yes | **Smoke-verified** against a real `org.a11y.Bus` session bus, but with **no GUI application registered** on the dev box (a bare tty session, `DISPLAY`/`WAYLAND_DISPLAY` unset) — `Available()`/capability detection and `Apps()` (correctly returning zero apps, not an error) were exercised live; `Find`/`Perform`/events against a live GUI app were not. Unit tests cover the traversal/action/event logic against a fake D-Bus connection. |
| **Windows — UIA** (`platform/windows`, go-ole + hand-built COM vtable calls) | Yes | **Compile-verified only.** `GOOS=windows go build`/`go vet` pass; never run against real Windows or a real COM implementation. Highest risk: vtable slot-index drift (every call site names its slot in a comment for a first-run audit). |
| **macOS — AX** (`platform/darwin`, purego dlopen of ApplicationServices/CoreFoundation) | Yes | **Compile-verified only.** `GOOS=darwin go build`/`go vet` pass; never run on real macOS. Highest risk: `CFRelease` discipline under sustained use. |
| **Browser — CDP** (`platform/browser`, reuses the existing `browser_*` tools' chromedp session) | Yes | **Fully live-verified** against real headless Chrome on this machine: `Find` (deduplicated), `Perform(invoke)` click, and `SetValue` all confirmed working end to end (`TestIntegrationLiveChromeFindAndClick`). This is the only backend with genuine live *action* verification. |
| **Events** (`internal/uiauto/events`) | AT-SPI: real D-Bus signal-driven. CDP: real, **live-verified** (`TestIntegrationLiveChromeEventSubscribe` against real Chrome). Windows/macOS: **not implemented** — no untested claim was made; `desktop_wait` transparently falls back to polling (`core.WaitFor`) wherever a backend doesn't support events. | | `Capabilities.Events` reports `false` honestly on Windows/macOS rather than a stub that would silently never fire. |
| **Input (mouse/keyboard)** (`internal/uiauto/input`) | Windows (raw `user32.dll` SendInput), Linux X11 (XTEST) + Wayland (portal `RemoteDesktop` + `ScreenCast`, one shared session with a persisted `restore_token`), macOS (CGEvent via purego) | X11/Wayland/macOS paths: **compile-verified only** (this dev box is a tty session with neither `DISPLAY` nor `WAYLAND_DISPLAY` set, so even the X11 path never ran live here) — honesty tests assert the correct `PLATFORM_NOT_SUPPORTED` behavior in that no-session state. Windows: compile-verified only. | Wayland absolute pointing maps the target point onto a real ScreenCast stream node; with no stream it reports `Capabilities.Mouse=false` and fails loudly instead of guessing. |
| **Screen capture** (`internal/uiauto/screen`) | Same four paths as input | Same caveat: compile-verified only on this box for the same no-display reason. | |
| **Vision fallback** (`internal/uiauto/vision`) | Yes, pure Go, no OS calls | Unit-tested (coordinate validation, grid overlay); the coordinate click itself rides the same input layer above, so its live status matches. | |

**Bottom line**: CDP/browser is the only backend with real, live, end-to-end action and event
verification. Linux AT-SPI is smoke-verified against a real accessibility bus (connection and
enumeration work) but has never driven a real GUI application. Windows UIA and macOS AX are
implemented to the same design and test rigor as the others but have **never run** outside
`GOOS=windows`/`GOOS=darwin` cross-compilation and `go vet`.

## OS permission prerequisites

- **macOS**: the process needs **Accessibility** permission (System Settings → Privacy &
  Security → Accessibility) for the AX backend and for `CGEventPost`-based physical input;
  without it, calls surface `PERM_DENIED` with that exact suggestion (checked via
  `AXIsProcessTrusted`, never silently prompted). Screen capture additionally needs **Screen
  Recording** permission; `CGDisplayCreateImage` returning an empty image surfaces as
  `PERM_DENIED` (macOS gives no cheap way to pre-check this permission the way
  `AXIsProcessTrusted` does for Accessibility).
- **Linux — Wayland**: there is no global input/screenshot API without the user's explicit,
  interactive consent through an XDG desktop portal. `RemoteDesktop`/`Screenshot` portal calls
  block on a real consent dialog (bounded by a 30s timeout) and any denial/timeout surfaces as
  `PERM_DENIED`/`TIMEOUT` — capabilities are never faked as available when they aren't.

  Two Wayland specifics worth knowing before enabling the feature there:

  - **Absolute pointing requires screen-cast consent, not just remote-control consent.** The
    portal's `NotifyPointerMotionAbsolute` addresses a *ScreenCast stream node*, so Pando opens
    one portal session that does `RemoteDesktop.SelectDevices` **and**
    `ScreenCast.SelectSources` on the same session handle, then maps a global `(x, y)` onto the
    stream whose rectangle contains it. If no stream can be obtained (consent refused, portal
    too old, compositor without the interface), `Capabilities.Mouse` reports `false` and pointer
    actions return `ACTION_FAILED` with an explanation — Pando does **not** guess a stream id and
    click somewhere unpredictable. Keyboard events still work in that state, since they need no
    stream.
  - **You consent once, not once per run.** The session is opened with `persist_mode = 2` and the
    portal's `restore_token` is stored in `<global config dir>/uiauto_wayland_portal.json`, then
    replayed on the next start. A token the portal rejects (expired, revoked) falls back to a
    fresh consent prompt rather than failing. Delete that file to force re-consent.

  Everything in this Wayland section is **implemented but never exercised against a live
  compositor** — the development machine had no Wayland session. The portal state machine and
  D-Bus wire decoding are unit-tested against fakes built from the XDG spec; the first real run
  should be treated as the actual verification.
- **Linux — accessibility bus**: the AT-SPI backend needs the desktop's accessibility bus
  enabled. If disabled, connecting surfaces `PERM_DENIED` suggesting
  `gsettings set org.gnome.desktop.interface toolkit-accessibility true` and restarting the
  target application (GTK/Qt apps only register with AT-SPI once this is on).
- **Windows**: UIA generally works without special permission grants for same-privilege-level
  processes; elevated target applications may still be inaccessible to a non-elevated Pando
  process (a normal Windows UIA limitation, not something this backend works around).

## Vision fallback

`desktop_click_at` is the deliberate last resort described in
[Philosophy](#philosophy-accessibility-first-not-screenshot-first): a raw coordinate click with
no semantic target. Guardrails:

- Its tool description explicitly tells the model to try `desktop_find`/`desktop_observe` first
  and only reach for it when accessibility genuinely can't describe the target.
- `desktop_screenshot(grid:true)` overlays a light coordinate grid with real, unscaled screen-pixel
  axis labels specifically so the model can read accurate `(x,y)` off the image instead of
  guessing.
- Coordinates are validated against the actual captured display bounds
  (`vision.ValidateCoordinates`) before anything reaches the OS — an out-of-bounds coordinate is
  `INVALID_ARGS`, not silently clamped.
- The response is unconditionally marked `{"source":"vision"}` — every caller, human or
  programmatic, can tell a vision-fallback action apart from a semantic one.
- It requires `DesktopAllowPhysicalInput` (otherwise `POLICY_DENIED`) and always prompts for
  permission with wording that states plainly this is a blind coordinate action.

## Events

`desktop_wait` uses `internal/uiauto/events` to avoid polling when the backend can push change
notifications: AT-SPI (`org.a11y.atspi.Event.Object` D-Bus signals) and CDP (`DOM`/
`Accessibility` domain events via `chromedp.ListenTarget`) both implement the optional
`events.Subscriber` interface, detected by type assertion so `core.Backend` itself needed no
changes. Where a backend doesn't implement it (Windows UIA, macOS AX today), `desktop_wait`
transparently falls back to the original polling `core.WaitFor` loop — same external behavior,
just slower to react. An event subscription is never fully trusted on its own: every wakeup
re-evaluates the real condition via `backend.Find` rather than trusting the event's content, and
a 2s fallback ticker covers changes the backend doesn't model as one of its known event kinds.

## MCP exposure

The Desktop Controller is an **internal tool provider only**; MCP is purely an external-facing
door onto the same 12 tools, never part of the internal wiring. Concretely:

- Inside Pando's own agent loop, the tools are constructed directly in
  `internal/llm/agent/tools.go` (`CoderAgentTools`/`CoderAgentToolsWithMesnada`) and called like
  any other builtin tool — there is no MCP hop in that path.
- `internal/llm/tools/builtin_names.go` lists all 12 `desktop_*` tool names as builtin. Pando's
  MCP **client** gateway (`internal/mcpgateway`, which lets the agent call *external* MCP
  servers) checks this list and redirects the model with a clear message
  (`"%q is a built-in agent tool, not an MCP catalog tool. Call %q directly..."`) if it ever tries
  to route a desktop tool through `mcp_call_tool` — it never shadows or intercepts them.
- Run `pando mcp-server` to expose Pando's own tools to **external** MCP clients (stdio and/or
  streamable HTTP). `cmd/mcp_server.go`'s `buildMCPServerTools` gates the same 12 constructors on
  the identical `InternalTools.DesktopEnabled` flag used internally — an external client sees
  the Desktop Controller present or absent exactly in step with the in-process agent's own tool
  list, with no separate `[MCPServer]` toggle to keep in sync.

```
pando mcp-server --no-http     # stdio transport only, for process-based MCP clients
```

with `.pando.toml`:

```toml
[InternalTools]
DesktopEnabled = true
```

**Caveat**: `pando mcp-server` calls `Permissions.SetGlobalAutoApprove(true)` at startup (the
same behavior every other mutating tool exposed by the MCP server already has — bash, file
write/edit/patch, etc.) because there is no interactive session to prompt in that mode. Over MCP,
every mutating desktop action and every screenshot still goes through the permission-request code
path, but it is auto-approved rather than interactively prompted. Anyone who can reach a
Desktop-Controller-enabled `pando mcp-server` process therefore has the same reach described in
[Security posture](#security-posture) below, without a human confirming each action — scope
exposure accordingly (bind to localhost, use `DesktopAllowedApps`, don't expose the MCP HTTP
transport to an untrusted network).

## Security posture

This feature lets the agent **read your screen and drive your mouse/keyboard**. Treat it like
any other capability that can act on your behalf, not like a passive read tool:

- **Off by default** (`DesktopEnabled = false`). Nothing in this subsystem runs, and none of the
  12 tools are even registered with the agent, until explicitly enabled.
- **Every mutating action prompts for permission**: `desktop_click`, `desktop_type`,
  `desktop_key`, `desktop_scroll`, `desktop_focus`, `desktop_click_at` all call
  `permission.Service.Request(...)` before touching anything, describing exactly what they're
  about to do (e.g. `"Click desktop element @s8f3k2p9:e17"`, or for the vision fallback,
  `"Blind coordinate click at screen position (x,y) -- no semantic element target..."`).
- **Screenshots prompt too**, even though they're read-only, because they capture the user's
  whole screen — `desktop_screenshot` is the one non-mutating tool in the group that still
  requests permission.
- **`DesktopAllowedApps`/`DesktopDeniedApps`** scope every desktop tool call to specific
  applications by id/name; deny always wins over allow, and an empty allow-list means "every app"
  (narrow it if you only ever want this on one or two known applications).
- **`DesktopAllowPhysicalInput`** is a separate gate on the synthetic-mouse/keyboard fallback and
  on any global (ref-less) action; turning it off keeps the agent limited to native accessibility
  actions only — no simulated hardware input at all.
- **In MCP server mode**, permission prompts are auto-approved (see the caveat at the end of
  [MCP exposure](#mcp-exposure)) — the human-in-the-loop protection that the interactive TUI/WebUI/
  ACP surfaces get is not present when driving Pando headlessly over MCP. Don't expose a
  Desktop-Controller-enabled MCP server to anyone you wouldn't hand mouse/keyboard control to
  directly.
