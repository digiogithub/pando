---
created_at: 2026-08-21T21:48:56.487114065Z
updated_at: 2026-08-21T21:48:56.487114065Z
tags:
    - change
    - extensions
    - frontend
    - webui
    - enterprise
    - p4
---
# P4 — Frontend capability + shared `@pando/client` layer

Date: 2026-08-21
Status: IMPLEMENTED and verified end to end.
Roadmap: §7.3, §8.6-Q2 and §9 of [[pando/analysis/extension_system_enterprise_analysis]], phase P4.
Builds on [[pando/changes/extension-system-p3-xpando-build-matrix]],
[[pando/changes/extension-system-p2-http-events]].

## 1. `@pando/client` — the real P4 deliverable

§8.6-Q2 resolved that the enterprise frontend is *a different visual layer with
identical communications*, which makes extracting the API/state layer the
deliverable, not the panel loader.

`web-ui/packages/pando-client` is now a bun workspace package holding
`services/`, `stores/`, `types/` and the two hooks that are pure API/state
(`useChat`, `useGoal`) — about 6.5k LOC moved. `web-ui` depends on it with
`workspace:*`; both the Vite alias and the tsconfig path resolve
`@pando/client/*` **straight to source**, keeping Vite's dep optimiser out of a
package that changes as often as the app.

All ~200 import sites were rewritten to subpath imports
(`@pando/client/stores/sessionStore`). Subpaths rather than a barrel: the
rewrite stays a mechanical prefix swap, default exports survive, and the module
graph stays explicit. A root barrel (`src/index.ts`) exists for external
consumers and as the one place the whole surface is visible.

Facts that shaped it:

- `services/` and `stores/` already imported **no React and no components** —
  the layering was sound, only undeclared. `hooks/` splits on that same line:
  API/state hooks moved, presentation/platform hooks (`useTheme`,
  `usePWAInstall`, `useLanguageSync`, `useAnimatedLogo`,
  `useDesktopNotifications`) stayed.
- `services/desktop.ts` and `wailsBindings.ts` could **not** move: they import
  `wailsjs/`, generated into this repository by `wails build`, which a shared
  package cannot reach. Only the pure detection (`isDesktop`) moved, as
  `services/host.ts`; the app's `desktop.ts` re-exports it and keeps the
  binding-dependent half.
- The package README states the invariant: no presentation in, no frontend
  imports in, dependency direction one-way.

This also satisfies "generate the TS types from one source shared by both
frontends" in its meaningful sense — there is now exactly one source. Deriving
them from the Go structs is a stronger, separate step and was **not** done.

## 2. Go contract — `pkg/extension/frontend.go`

Three deliberately separate interfaces, because they fail differently:

| Interface | Scope |
|---|---|
| `FrontendProvider` (`AssetPath`, `Assets() fs.FS`, `Panels()`) | adds files under `/ext/<path>/` and declares panels |
| `FrontendOverlay` (`OverlayAssets`, `Overrides() []string`) | shadows a named list of core asset paths |
| `FrontendReplacer` (`ReplaceFrontend() fs.FS`) | replaces the whole asset root |

Plus `PanelManifest` and a closed slot set: `SlotSidebar`, `SlotSettings`,
`SlotChatSide`, `SlotStatusBar`.

Rules encoded and documented:

- **The build-tag asset swap is dead** (P0 finding, restated in the contract
  doc): `//go:embed` cannot cross module boundaries, so `FrontendReplacer` is
  the mechanism for an alternative frontend, not a tag.
- **Overlays shadow only declared paths.** A blanket shadow makes upgrades
  undebuggable. Two extensions claiming a path: refused, not layered.
- **Two replacers keep the core frontend** and log loudly — picking one would
  ship a customer the wrong product.
- **Extension assets are public** and the contract says so explicitly: they are
  served by the static layer, outside the API token check, because a browser
  cannot attach headers to a dynamic `import()`. Private data belongs behind an
  `/api/ext/` route.

## 3. Host wiring — `internal/extensions/frontend.go`

`Frontend(mgr, core fs.FS) fs.FS` is the single choke point; precedence reads
in one function: `overlay > base (replacement, else core) > extension subtrees`.
Helpers: `unionFS` (first layer wins, merged `ReadDir`), `restrictedFS`
(overlay limited to declared paths), `subtreeFS` (mount under `ext/<seg>`).
`Panels(mgr)` merges the manifest, namespaces IDs with the extension ID,
resolves `Entry` to a URL, and sorts by slot, order, ID.
`HasFrontendExtensions` lets a standard build skip composition entirely.
Every extension call is panic-contained (`safeFS`).

Wired in `internal/api/server.go` by composing `s.staticFS` **before** the
handler is built, so the precompressed lookup, the SPA fallback and the file
server all see the same tree. New endpoint `GET /api/v1/extensions/ui`
(`handlers_extensions.go`) always returns a list, never null.

Security bug caught by its own test: the first `cleanAssetPath` cleaned
`"/" + p`, and `path.Clean("/../../secret.js")` is `"/secret.js"` — an escape
attempt was being *normalised into a valid root path* instead of rejected. Now
the leading slash is stripped before cleaning and `fs.ValidPath` decides.

## 4. Shell integration

- `src/lib/pandoUI.ts` — the `window.__PANDO_UI__` contract, `PANDO_UI_VERSION = 1`.
  A panel's default export is a **mount function** `(el, ctx) => cleanup?`, not
  a React component: handing over a DOM node removes any React-version
  agreement between panel and shell, and `ctx.react` is the shell's own
  instance so React is never loaded twice.
- `components/extensions/ExtensionPanel.tsx` — dynamic `import()`, errors and
  cleanup failures contained to the panel's own box.
- `components/extensions/ExtensionSlot.tsx` — renders nothing when a slot is
  empty, so it can be dropped into a layout unconditionally.
- `ExtensionPanelPage.tsx` + route `/ext/:panelId` for sidebar panels.
- All four slots wired: Sidebar (appended after core items, never reordering
  them), SettingsView (own "Extensions" group, `ext:` prefixed category type),
  ChatInfoSidebar (last), StatusBar.
- `App.tsx` installs the contract and loads the manifest once at boot.

## Files touched

- new: `pkg/extension/frontend.go`, `internal/extensions/frontend.go` (+test),
  `internal/api/handlers_extensions.go` (+test),
  `web-ui/packages/pando-client/{package.json,tsconfig.json,README.md,src/index.ts}`,
  `…/src/services/{extensionUI,host}.ts`, `…/src/stores/extensionPanelsStore.ts`,
  `web-ui/src/lib/pandoUI.ts`, `web-ui/src/components/extensions/*`,
  `docs/extension-frontend.md`
- moved: `web-ui/src/{services,stores,types}` and `hooks/{useChat,useGoal}.ts`
  into the package
- modified: `internal/api/{routes,server}.go`, `web-ui/{package.json,vite.config.ts,tsconfig.app.json}`,
  `web-ui/src/App.tsx`, `Sidebar.tsx`, `StatusBar.tsx`, `ChatInfoSidebar.tsx`,
  `SettingsView.tsx`, `services/desktop.ts`, ~200 import rewrites,
  `docs/extension-builds.md`, `alchemai-agent/compat/compat.go`

## Verification

- `go build ./...`, `go vet`, `gofmt` clean. `go test ./internal/extensions
  ./internal/api ./pkg/extension ./internal/app` pass; 16 new Go tests.
- `tsc --noEmit` clean; `eslint .` 0 errors (4 pre-existing warnings);
  `bun run build:embedded` produces the embedded assets and `go build` links them.
- **End to end** with the xpando demo module extended with a `ui.demo.xp3`
  `FrontendProvider` (embedded `dist/panel.js`, one sidebar and one settings
  panel), composed via
  `xpando build --with …/tools=… --with …/ui=… --replace core=… --output pando-xp4`:
  - `extensions list` → both demo extensions `loaded`;
  - `GET /ext/demo/panel.js` (no token) → the panel source;
  - `GET /api/v1/extensions/ui` without a token → `{"error":"unauthorized"}`;
    with the token → both panels, IDs namespaced (`ui.demo.xp3.reports`),
    entries resolved to `/ext/demo/panel.js`, sorted.

Not exercised against a running binary: `FrontendOverlay` and
`FrontendReplacer` (unit-tested only), and a browser actually importing a panel.
An unknown `/ext/...` path answers with the SPA `index.html`, same as any
mistyped core asset, so a wrong `Entry` surfaces as a MIME-type error in the
panel's error box rather than a 404 — documented.

## Next

P5 — `MemorySink` + `RemembranceStoreWrapper` in core and the first enterprise
module. §8.6-Q4: core owns only the interfaces and emission points, and
`MemoryEvent` must be rich enough on the first pass (scope, key, path, content,
tags, embedding, project, user, timestamp, origin) because widening it later
means a coordinated release of both repositories.
