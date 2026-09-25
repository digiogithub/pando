---
created_at: 2026-09-25T10:58:22.645227155Z
updated_at: 2026-09-25T11:07:01.98722609Z
tags:
    - fix
    - webui
    - agentvcs
    - diff
---
# Fix: WebUI chat diff viewer showed two empty panes

Date: 2026-09-25

## Symptom
Opening a changed file from the chat FileChangesBar showed the Monaco DiffEditor with both columns empty; the file content was never loaded. The viewer header (path + close button) was also hidden under the app title bar.

## Root causes
1. `web-ui/src/components/chat/DiffViewer.tsx` never read the file. It only rendered `FileChange.edits` (old_string/new_string snippets from tool inputs). Entries created from `patch` result metadata (`files_changed`), from agent-vcs hydration (`fromVcs`, `edits: []`), or from tools without snippets have empty edits, so both panes were `''`. With edits, only the snippet was shown.
2. The overlay (`.chat-diff`, `position: fixed; z-index: 1000`) rendered inside `.shell-main-inner` (`z-index: 1`, own stacking context), so `.shell-titlebar` (`z-index: 200`) covered its header.

## Fix
- `DiffViewer` resolves full content asynchronously via `loadDiffContent(file, sessionId, cwd)`:
  1. agent-vcs: `GET /api/v1/agentvcs/sessions/{id}/diff`, match entry by relative path (or suffix of absolute tool path), fetch `old_hash`/`new_hash` blobs from `/api/v1/agentvcs/blobs/{hash}` (session baseline vs HEAD).
  2. Fallback: `GET /api/v1/files/{rel}` for the current file; original rebuilt by `revertEdits` (undo edits newest-first; `write` with identical content gives an empty original).
  3. Last resort: previous snippet replay `buildDiffContent`.
  - `toRelativePath` strips workspace cwd (`useProjectStore.workspace.cwd`, `fetchWorkspace` if missing).
  - Loading placeholder `.chat-diff-loading` (`web-ui/src/styles/chat.css`), text `common.loading`.
- The viewer is rendered with `createPortal(..., document.body)` (same approach as `components/ui/Dialog.tsx`).

## Verification
- `bun run typecheck`, `bun run lint` (0 errors), `bun run build:embedded` pass.
- Live check: vite dev server proxied to the running instance (https://localhost:8765, basic auth + `/api/v1/token`), driven with playwright-core + Chrome. Session 08256fd4 (9 changed files): milestone .md showed baseline (26 lines, `status: backlog`) vs HEAD (27 lines, `status: done`, `closed:` added) with highlights; README.md full file both sides; header with path/close button visible after the portal change.
- Note: the SPA re-selects the session shortly after load; opening a diff during that window unmounts FileChangesBar (viewer closes). Pre-existing, only seen when clicking within ~1s of switching session.
- The pando binary must be rebuilt to embed the new dist.
