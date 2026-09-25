---
created_at: 2026-09-25T11:18:39.216134836Z
updated_at: 2026-09-25T11:18:39.216134836Z
tags:
    - webui
    - orchestrator
    - change
---
# WebUI Orchestrator: column widths + drag-resizable task detail panel (2026-09-25)

## What changed
- Mesnada Tasks table (`web-ui/src/components/orchestrator/OrchestratorView.tsx`) now uses `table-layout: fixed` via new `.view-table--fixed` class with a `<colgroup>`: Status 120, Name auto (takes remaining width), Agent 120, Model 170, Progress 90 (was min 130/120), Tokens 100, Actions 80. Table `min-width: 720px` so the wrapper (`.view-table-wrap`, `overflow-x: auto`) scrolls horizontally when the detail panel is widened.
- `TaskRow.tsx`: removed per-cell inline widths; cells ellipsize (fixed-table rule) and expose full text via `title`.
- Detail panel is resizable: new `web-ui/src/components/shared/ResizeHandle.tsx` (vertical `role="separator"` handle, pointer capture drag, ArrowLeft/Right keyboard resize, body cursor/user-select lock while dragging) and `web-ui/src/hooks/useResizablePanel.ts` (width state clamped to `[minWidth=280, container - minMain=240]`, re-clamped on window resize, persisted in localStorage key `pando.orchestrator.detailWidth`, default 380).
- `TaskDetail.tsx` accepts optional `width` prop (inline style over `.view-detail` 320px default).
- `styles/views.css`: `.view-table--fixed`, `.resize-handle` (7px hit area overlapping the border, accent line on hover/focus/drag); mobile (≤768px) hides the handle and makes `.split-pane > .view-detail` full width.

## Why
User request: Progress column too wide, Name should take most space, Agent/Model narrower; detail panel should be widenable by dragging the separator, with horizontal scroll in the task table.

## Verification
- `npx tsc -p tsconfig.app.json --noEmit` OK; eslint on touched files clean.
- Visual check not done: pando browser tool navigation failed ("context canceled").
