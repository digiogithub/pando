# Extension frontends

Pando has more than one frontend. The core WebUI ships in this repository; an
enterprise build ships a different visual layer, under a different brand, whose
communications with the Pando API are *identical*. This document describes how
that works and what an extension may do to the UI.

## `@pando/client` — the shared layer

`web-ui/packages/pando-client` holds the transport and state layer: the REST
client, the SSE subscriptions, the Zustand stores, the hooks built directly on
them, and the TypeScript types shared by all of it. Both frontends import it.

That split is what keeps the two from drifting. An endpoint change, a new SSE
event, a renamed field: made once, and a mismatch is a TypeScript error in
every frontend rather than a runtime surprise in the one nobody ran.

The rule is one-way and simple: **the package contains no presentation** — no
JSX, no CSS, no i18n, no icons — and it imports nothing from any frontend. A
hook belongs there when it is state over the API (`useChat`, `useGoal`); it
stays in the frontend when it is about looks or the host platform (`useTheme`,
`usePWAInstall`, the Wails bridge).

```ts
import api from '@pando/client/services/api'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import type { Session } from '@pando/client/types'
```

See `web-ui/packages/pando-client/README.md` for the layout and the versioning
expectations.

## The three Go mechanisms

They are deliberately separate, because they fail in very different ways and
have to be debuggable apart.

| Interface | Scope | Use for |
|---|---|---|
| `FrontendProvider` | adds files under `/ext/<AssetPath>/` and declares panels | a feature panel inside the core UI |
| `FrontendOverlay` | shadows a **named list** of core asset paths | branding: logo, theme, favicon |
| `FrontendReplacer` | replaces the whole asset root | an entire alternative frontend |

Precedence is decided in one function (`internal/extensions.Frontend`):

```
overlay files  >  base (replacement, else core)  >  extension subtrees
```

Notes that are easy to get wrong:

- **A build-tag swap of the embedded assets is not an option.** `//go:embed`
  cannot reach files in another Go module, and an enterprise frontend lives in
  an enterprise module. `FrontendReplacer` exists because of that constraint.
- **An overlay may only shadow the paths it declares** in `Overrides()`. A
  blanket shadow would make upgrades undebuggable: a stale extension file would
  quietly beat a new core one with no way to see it happening. Two extensions
  claiming the same path is refused, not layered.
- **Two replacers keep the core frontend.** Picking one would ship a customer
  the wrong product; the conflict is logged loudly instead.
- **A replacement is a complete root.** Core assets it does not carry are gone,
  not merged in behind it.

## Panels

An extension declares panels in `Panels() []PanelManifest`; core merges them
into `GET /api/v1/extensions/ui`, resolving each `Entry` to a URL:

```json
{"panels":[{"id":"ui.acme.reports","extension":"ui.acme","title":"Reports",
            "slot":"sidebar","entry":"/ext/acme/panel.js","order":1}]}
```

Panel IDs are namespaced with the extension ID by core, so two extensions may
use the same local ID without knowing about each other.

Slots are a closed set — `sidebar`, `settings`, `chat-side`, `status-bar` —
because each one is a place the shell actually reserves. A panel asking for
anything else is dropped with a log line rather than mounted somewhere
plausible. A `sidebar` panel also gets a route, `/ext/<panel-id>`.

### Assets are public

Extension assets are served by the *static* layer, next to core's JavaScript,
**outside the API token check**. This is not an oversight: a browser cannot
attach an `Authorization` header to a dynamic `import()`, so a bundle behind
the token could never load.

Never serve anything from `Assets()` that is not safe to hand to an
unauthenticated client. Private data belongs behind an `HTTPEndpointProvider`
route under `/api/ext/`, which the panel then calls with the token — that is
what `ctx.api` is for.

### `window.__PANDO_UI__`

A panel is a built ES module compiled separately from the shell, possibly
against a different core version, so the surface it may rely on is small,
explicit and versioned (`PANDO_UI_VERSION`, currently `1`).

A panel's default export is a **mount function**, not a React component:

```js
export default function mount(el, ctx) {
  el.textContent = 'hello from ' + ctx.extensionId
  return () => { /* optional cleanup */ }
}
```

Handing over a DOM node keeps a panel free of any React-version agreement with
the shell. A panel that wants React uses `ctx.react`, which is the shell's own
instance, so hooks and context work and React is never loaded twice. Do not
bundle React into a panel.

The context carries `version`, `react`, `api` (the REST client, already
authenticated), `apiBase`, `panelId` and `extensionId`. The same object is
installed on `window.__PANDO_UI__` at boot, before any panel is imported.

Failures are contained: a panel that throws on import or on mount renders an
error message inside its own box and leaves the rest of the UI working. Note
that a wrong `Entry` produces a MIME-type error rather than a 404 — the SPA
fallback answers unknown paths with `index.html`, exactly as it does for a
mistyped core asset.

## Building

Extension assets are embedded in the *extension's* module with its own
`//go:embed`, and the binary is composed with `xpando` — see
[extension-builds.md](extension-builds.md).

```go
//go:embed dist
var assets embed.FS

func (u *UI) AssetPath() string { return "acme" }
func (u *UI) Assets() fs.FS     { sub, _ := fs.Sub(assets, "dist"); return sub }
```

A standard build has no frontend extensions: the manifest is an empty list, the
asset tree is core's own, and none of the code above runs.
