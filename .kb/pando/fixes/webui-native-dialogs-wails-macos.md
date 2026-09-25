---
created_at: 2026-09-25T20:02:40.282595954Z
updated_at: 2026-09-25T20:02:40.282595954Z
tags:
    - fix
    - webui
    - desktop
---
# Fix: editor context menu actions do nothing in Pando Desktop (GitHub issue #13)

## Symptom
Pando Desktop on macOS, Code Editor explorer context menu: "Delete" does not remove the file. New File / New Folder / Rename and the header "New file" button were equally broken; closing a dirty tab could never be confirmed.

## Root cause
Wails v2 on macOS (WKWebView) does not implement the WKUIDelegate JS panels, so `window.confirm()` returns `false` and `window.prompt()` returns `null` without showing anything. Every handler returned early. The backend (`internal/api/handlers_files.go` handleDeleteFile/handleRenameFile) was fine.

## Change
- New `web-ui/src/components/shared/PromptDialog.tsx` (in-app input dialog on the `Dialog` primitive).
- New `web-ui/src/components/shared/useDialogs.tsx`: promise-based `confirm()` / `prompt()` hook rendering `ConfirmDialog` / `PromptDialog`; render `dialogs` once in the component. A replaced pending dialog is settled (false/null) so callers never hang.
- `components/editor/FileExplorer.tsx`: all context-menu and header actions use the hook; the menu is closed before the dialog opens (previously a cancelled prompt left it open); delete URL-encodes path segments, closes the tab of a deleted file, and warns when deleting a folder.
- `components/editor/CodeEditorView.tsx` (New file button), `components/editor/EditorTabs.tsx` (dirty tab close, click and middle-click), `components/orchestrator/CronJobsPanel.tsx` (delete cronjob) migrated too.
- Rule: never use `window.confirm/prompt/alert` in web-ui; use `useDialogs()`.

## Verification
- `npx tsc -b` clean; eslint on touched dirs: no new warnings.
- No web-ui unit tests exist; not exercised in the macOS desktop app (bug only reproduces in WKWebView).

Related: [[feature_webui_native_redesign]]
