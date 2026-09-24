---
created_at: 2026-09-24T21:40:17.805396011Z
updated_at: 2026-09-24T21:40:17.805396011Z
tags:
    - webui
    - redesign
    - feature
---
## WebUI native redesign — P6 "Secondary views", part A (2026-09-24)

Part of epic PANDO-EP-0010 / story PANDO-US-0056. Sibling part B covers other
secondary views; this document covers the area owned by this agent:
`web-ui/src/components/{editor,terminal,agentvcs,design,overlays,splash,auth}/*`.

### What changed

**Editor** (`components/editor/*`)
- `CodeEditor.tsx`: Monaco theme (`pando`) now defined at runtime from CSS
  tokens via new `components/editor/monacoTheme.ts` (`definePandoMonacoTheme`,
  `watchMonacoTheme`) — reads `--bg`, `--fg`, `--fg-muted`, `--fg-faint`,
  `--border`, `--accent`, `--danger`, `--success`, `--info`, `--warning` with
  `getComputedStyle`, builds translucent variants via an 8-digit hex alpha
  helper (`hexAlpha`) instead of relying on `color-mix()` resolution. Redefines
  + `monaco.editor.setTheme()` on every theme-store change (family/mode/accent).
  Font: `JetBrains Mono Variable`, 13px.
- `CodeEditorView.tsx`, `EditorStatusBar.tsx`, `EditorTabs.tsx`,
  `FileExplorer.tsx`: FontAwesome → lucide (`@/components/ui/icons`), all
  inline styles replaced by new `styles/editor.css` classes (`.editor-*`),
  buttons/inputs replaced by `Button`/`IconButton`/`Input` from `@/components/ui`.
  File-tree icons are monochrome (fg-muted; shape differentiates file type —
  no hardcoded per-language colours, to keep one restrained accent). File tree
  rows are 27px. Tabs: flat with accent underline on the active tab. Context
  menu is a fixed-position `.editor-context-menu` reusing `.ui-menu-item`.

**Terminal** (`components/terminal/*`)
- New `components/terminal/xtermTheme.ts`: `getXtermTheme()` builds an
  xterm.js `ITheme` from `--bg-card`/`--fg`/`--accent`/`--accent-soft` plus a
  16-colour ANSI palette; `watchXtermTheme()` re-applies live on theme change
  via `terminal.options.theme = ...` (xterm.js v5 live-option API).
- New `styles/terminal.css`: `--term-*` ANSI palette tokens under `:root` /
  `:root[data-theme="dark"]` (the one place besides itself allowed hardcoded
  hex, per the shared brief) + `.terminal-*` chrome classes.
  `TerminalView.tsx`, `TerminalPtyPane.tsx`, `TerminalOutput.tsx`,
  `TerminalInput.tsx`: FA → lucide, all colours from tokens, font
  `JetBrains Mono Variable`.

**Agent VCS** (`components/agentvcs/*`)
- New `styles/agentvcs.css`. `AgentVcsDiffViewer.tsx` reuses the same Monaco
  theme helper (`DiffEditor` themed as `pando`, live-updates). `AgentVcsView.tsx`
  (995→~470 lines): sessions/timeline/detail panels restyled with tokens;
  add/delete/modify indicators now `Badge` (`success`/`danger`/`warning`)
  instead of hardcoded Catppuccin hex; confirm-revert dialogs now use the
  already-redesigned `@/components/shared/ConfirmDialog` (built on `ui/Dialog`)
  instead of a bespoke modal.

**Design** (`components/design/*`)
- New `styles/design.css`. All 10 view files migrated: toolbar buttons →
  `Button`/`IconButton`, artifact/template tab switches → `SegmentedControl`,
  Studio side panel + Inspector structure/issues tabs → `Tabs`, `ExportMenu`
  rewritten on top of `Popover`'s `Menu`/`MenuItem` with a real `anchorRef`
  (was a hand-rolled absolutely-positioned dropdown). Severity colours
  (blocking/error/warning/info) are CSS modifier classes reading `--danger` /
  `--warning` / `--fg-faint`, no inline hex. `design.*` i18n keys were already
  present and unchanged — only the JSX/CSS changed, not copy or behaviour.

**Overlays** (`components/overlays/*`)
- New `styles/overlays.css` (`.ovl-*`): shared command-palette look (blurred
  scrim, `.ovl-panel`, `.ovl-item[data-selected]`) used by both `QuickMenu.tsx`
  and `ModelSwitcher.tsx`. Model badges (`fast`/`cost`/`capable`/`vision`/
  `reasoning`/`thinking`) now map to `Badge` tones instead of a hardcoded
  `BADGE_COLORS` hex map. `ConfigInitBanner.tsx` restyled with `Button` +
  `.ovl-banner`.

**Splash / Auth**
- New `styles/splash.css` + rewritten `SplashScreen.tsx`: dropped the large
  serif 木/"PANDO" letter-spaced wordmark for a quiet mark (accent-soft
  rounded square + `Bot` icon) + small wordmark + thin accent progress bar —
  matches the brief's "quiet: small mark + wordmark + subtle progress, on
  --bg" spec. Same status machine (`connecting`/`authenticating`/`ready`/
  `error`), same fade-out timing.
- New `styles/auth.css` + `LoginDialog.tsx` restyled with `Card`/`Button`
  (form fields already used the redesigned `TextInput` from
  `shared/FormInput.tsx`, untouched).

### Shared files touched (small, targeted edits only)
- `components/ui/icons.ts`: added `Crosshair`, `LogIn` exports (alphabetical,
  no FA additions).

### Verification
- `cd web-ui && bun run typecheck` → 0 errors (tsc -p tsconfig.app.json).
- `bunx --bun eslint <all touched dirs + new css + icons.ts>` → 0 errors,
  0 warnings on TS/TSX (CSS files just get the expected "no matching
  eslint config" warning, not a real issue).
- `grep -rl fortawesome` across the whole area → no matches (FA fully removed).
- `grep -rn '#[0-9a-fA-F]\{3,8\}'` across area TSX and non-terminal CSS → no
  matches (tokens only; the one exception, `styles/terminal.css`'s ANSI
  palette, is explicitly allowed by the brief).
- Cross-checked every `className`/`clsx(...)` class name used in each area's
  TSX against the classes actually defined in that area's CSS file (grep
  diff) → 0 missing definitions in all 7 areas.
- Dev server smoke test: started `vite --config vite.dev-https.config.ts
  --port 5614`; curled every rewritten `.tsx` module and every new `.css` file
  through Vite's dev transform endpoint — all returned HTTP 200 (Vite returns
  500 with an error overlay body on a TS/CSS syntax or resolution error, so a
  200 confirms clean esbuild/PostCSS transforms end to end).
- **Could not get live screenshots**: `mcp__pando__browser_navigate` /
  `browser_screenshot` returned `context canceled` on every attempt (~18
  retries over ~10 minutes, including against `https://example.com`, so the
  shared browser MCP session itself was unavailable this session, not a
  problem specific to this app). Visual light/dark verification in the actual
  browser is still outstanding and should be done in a follow-up pass.

### Leftovers / requests for other areas
- Live screenshot QA (light + dark + mobile) for `/editor`, `/terminal`,
  `/snapshots`, `/design` is still needed — blocked on browser tool
  availability, not on the code.
- `components/shared/ConfirmDialog.tsx`, `components/shared/EmptyState.tsx`
  and `components/shared/FormInput.tsx` were found already migrated to the
  new `ui/*` primitives by another concurrent agent — this area now depends
  on that being final (used as-is, not modified here).
