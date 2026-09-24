---
created_at: 2026-09-25T07:29:17.959514323Z
updated_at: 2026-09-25T07:29:17.959514323Z
tags:
    - fix
    - webui
    - editor
    - monaco
---
# Fix: WebUI code editor crash ("Illegal value for token color: #fff") and half-height Monaco

**Date:** 2026-09-25 · Story PANDO-US-0058 · Epic [[pando/features/webui-native-redesign.md]]

## 1. Crash when opening a file
- Symptom: clicking a file in `/editor` showed the ErrorBoundary "Something went wrong — Illegal value for token color: #fff".
- Cause: the production CSS minifier (Lightning CSS via Tailwind 4/Vite) rewrites `--bg:#ffffff` to `--bg:#fff`. `monacoTheme.ts` read tokens with `getPropertyValue` and passed them to `monaco.editor.defineTheme`; Monaco's `ColorMap` only accepts `#?rrggbb(aa)` (regex in `monaco-editor/esm/vs/editor/common/languages/supports/tokenization.js`). `editor.foreground/background` are fed into the token colour map, so a 3-digit hex throws.
- Fix: new `web-ui/src/lib/cssColor.ts` (`toHexColor`, `cssVarHex`) expands short hex and resolves rgb()/named/color-mix() through a probe element into strict `#rrggbb[aa]`. Used by `components/editor/monacoTheme.ts` (replaces its local `cssVar`) and `components/chat/DiffViewer.tsx` (its `token()` rejected short hex silently and fell back).
- Rule: any code handing CSS token values to Monaco (or anything needing strict hex) must go through `cssVarHex`.

## 2. Monaco rendered in the top third, tabs hidden
- Cause: on desktop `.editor-explorer-panel` had no styles, so the explorer grew to the full tree height; the editor row (`overflow: hidden`) was then scrolled programmatically (525px) when a tree row took focus, pushing tabs + Monaco up off-screen.
- Fix: `styles/editor.css` — desktop `.editor-explorer-panel { display:flex; flex-direction:column; min-height:0 }` and `> .editor-explorer { flex:1; min-height:0 }` so the tree scrolls inside `.editor-tree`; editor row in `CodeEditorView.tsx` uses `overflow: 'clip'` (cannot be scrolled programmatically).

## Verification
Headless Chrome against vite + `pando serve`: open `/editor`, click `go.mod` → Monaco top=80 h=794, row scrollTop 0, tabs visible, 0 page errors. `bun run typecheck`, `bun run lint` (0 errors), `bun run build:embedded` OK.

Related: [[pando/features/webui-settings-unsaved-guard.md]]
