---
created_at: 2026-09-25T07:26:07.54232137Z
updated_at: 2026-09-25T07:26:07.54232137Z
tags:
    - feature
    - webui
    - settings
---
# WebUI Settings: unsaved-changes guard + Monaco #fff fix (PANDO-US-0058)

Part of the WebUI 2.0 redesign: [[pando/features/webui-native-redesign.md]]. Date: 2026-09-25.

## Part 1: Monaco crash "Illegal value for token color: #fff"
- Cause: Lightning CSS minifies `--bg:#ffffff` to `#fff` in the production CSS. The editor theme read the token colours from CSS, but Monaco only accepts 6- or 8-digit hex.
- Fix: `web-ui/src/lib/cssColor.ts` normalises CSS colours to 6/8-digit hex before they reach Monaco (done earlier).
- Verified 2026-09-25: opened /editor in headless Chrome and clicked go.mod. Monaco rendered with 0 pageerrors.

## Part 2: unsaved-changes guard in Settings
The user asked for this: Esc in Settings goes back to the chat. Any move between settings screens, or away from Settings, first warns about unsaved changes and offers to save or discard them.

### Design
- **Registry**: `web-ui/src/components/settings/unsavedChanges.ts`
  - zustand `useUnsavedChangesStore` with `entries`, `register`, `unregister`, `hasUnsaved()`, `dirtyEntries()`, `saveAll(): Promise<boolean>` and `discardAll()`.
  - Hook `useUnsavedChangesGuard({ id, dirty, save, discard, label? })` registers the panel while it is mounted and unregisters it on unmount. It always calls the latest `save`/`discard` through refs.
  - `save` resolves `false` on failure. Stores catch their own errors, so panels return `!useXStore.getState().error`.
- **Dialog**: `web-ui/src/components/settings/UnsavedChangesDialog.tsx`, built on the `Dialog` primitive.
  - Buttons: Save and continue (primary), Discard changes (danger), Cancel (ghost). Esc and a click outside also cancel.
  - No X button (`hideClose`).
  - If the save fails, the dialog stays open and shows an inline error, and nothing navigates.
  - CSS: `.settings-unsaved-text` / `.settings-unsaved-error` in `styles/settings.css`.
  - i18n: `settings.unsaved.{title,description,descriptionSections,saveAndContinue,discard,cancel,saveFailed}` in all 7 locales. pt uses pt-BR wording, like the rest of pt.json.
- **Router**: `App.tsx` moved from `<BrowserRouter>` to a data router (`createBrowserRouter(createRoutesFromElements(...))` + `<RouterProvider>`).
  - The router is created once at module level.
  - A pathless root route `RootLayout` holds what used to wrap `<Routes>`: Suspense, ErrorBoundary, DesignRouteEffects and `<Outlet/>`.
  - Every route is unchanged. The splash/login gating still renders the RouterProvider only when the app is ready.
- **SettingsView** guards these transitions:
  - (a) Category switch, including extension sections, and (b) the mobile back button: both go through `guarded(action)`, which defers the action while the dialog is open.
  - (c) Esc listener on `window`, which navigates to `/` (the sidebar "Chat" target). It is ignored when:
    - `defaultPrevented` is set, or a modifier key is held;
    - an Esc owner is open (`.ui-dialog-overlay`, `.ui-popover`, `.ovl-scrim`, `.shell-scrim`, `[aria-modal]`, `[role=menu]`, `[role=listbox]`);
    - the focused element has `aria-expanded=true`.
  - (d) `useBlocker` blocks any pathname change while `hasUnsaved()`. It covers sidebar links, header buttons, browser back and the Esc navigation. `blocker.proceed()` / `blocker.reset()` are driven by the dialog.
  - (e) A `beforeunload` prompt while there are unsaved changes.
- **Primitives**: Dialog and Popover (and so Menu) now call `preventDefault()` on Esc, in addition to `stopPropagation`. ModelCombobox does the same.

### Panels guarded (registration id = category id)
- general, appearance: `settingsStore`
- agents, tools, bash, sandbox, token-optimization: `settingsStore` sub-stores
- mcp-gateway, container-runtime
- lua, skills: `extensionsStore.extensions*`
- self-improvement: `extensionsStore.evaluator*`
- mesnada, remembrances, snapshots, api-server: `servicesSettingsStore`
- design-system: local draft state. A save counts as successful when the store's `system` object is replaced.

### Not guarded, on purpose
These panels save immediately through dialogs or list actions and have no page-level pending state:
- providers
- mcp-servers
- lsp (activation fields save on blur/change)
- webui-access

The in-dialog forms of those panels are not guarded.

## Verification
- `bun run typecheck`: 0 errors.
- `bun run lint`: 0 errors (4 warnings that were already there).
- `bun run build`: OK.
- Headless Chrome (playwright-core) against vite on :5630 with `pando serve` on :8765:
  - No changes: Esc goes to `/`.
  - General change + Esc: the dialog appears. Cancel stays on the page with the edit kept. Esc on the dialog cancels. Discard goes to `/` and the value is reverted. Save and continue goes to `/` and the value persisted.
  - The original value (nerd_fonts=true) was restored afterwards.
  - Sidebar Logs link while dirty: the dialog appears, then Discard goes to /logs.
  - Bash change + click Sandbox: the dialog names "Bash". Cancel keeps Bash. Discard switches to Sandbox and the change is not persisted.
  - /editor: clicked go.mod, Monaco rendered, 0 pageerrors.
