---
created_at: 2026-07-17T13:03:18.064198368Z
updated_at: 2026-07-17T13:07:36.073153031Z
tags:
    - fix
    - webui
    - responsive
    - chat
---
# Fix: mobile layout broken in the advanced chat view by the chat info sidebar (2026-07-17)

## Symptom

On mobile widths (<=768px) the WebUI advanced chat view (`/chat`, `ChatView`) rendered
unusable: the chat area was crushed to almost nothing by the new `ChatInfoSidebar`
(see [[project_tui_chat_info_sidebar_plan]] and the WebUI counterpart in
`web-ui/src/components/chat/ChatInfoSidebar.tsx`). The simple chat view
(`/chat/simple`, `SimpleChatView`) looked fine.

## Root cause

`MainLayout` shipped a global rule inside its mobile media query:

```css
@media (max-width: 768px) {
  aside { width: 100% !important; }
}
```

It was meant for the nav drawer, but it is an unscoped element selector, so it also
matched the `<aside>` rendered by `ChatInfoSidebar`, forcing the info panel to 100% of
the main content area. Measured in the browser at ~948px viewport with the rule applied
manually: the info aside grew from 264px to 774px (the full width of `.main-content`),
leaving no room for `MessageList` / `ChatInput`.

`SimpleChatView` escaped the bug only because its route is declared **outside**
`MainLayout` (`web-ui/src/App.tsx:117` vs the `MainLayout` route at 121-123), so the
media query never applied to it.

## Final behaviour (after user feedback)

- **Collapsed (any width)**: no rail in the flex flow at all. Only a 34x26 tab floats over
  the chat's top-right corner (`position: absolute`, `top: 0.5rem`, `right: 0`,
  rounded on the left side). The chat keeps 100% of the width.
- **Expanded, desktop**: unchanged, the 264px panel takes its own column.
- **Expanded, mobile**: overlays the chat instead of shrinking it, anchored top-right,
  `width: min(280px, 85vw)`, `max-height: 65%` so the message input and the interface
  footer stay visible and tappable; the backdrop is clipped to the same 65% for the same
  reason.

## Changes

- `web-ui/src/components/layout/MainLayout.tsx`: scoped the mobile rule to
  `.sidebar-container aside` so only the nav drawer stretches.
- `web-ui/src/components/chat/ChatInfoSidebar.tsx`:
  - collapsed state returns a floating `<button>` (`railTabStyle`) instead of an
    `<aside>` rail; `railStyle` removed, `RAIL_WIDTH` now sizes the tab.
  - expanded state gained `.chat-info-panel` + a `.chat-info-backdrop` sibling
    (`display: none` by default, `block` on mobile, click calls `toggleInfoSidebar`).
  - `<style>` media block (<=768px): panel `position: absolute`, `top: 0`, `right: 0`,
    `bottom: auto`, `max-height: 65%`, `z-index: 100`, `width: min(280px, 85vw)`;
    backdrop `bottom: auto`, `height: 65%`.
- `web-ui/src/components/chat/ChatView.tsx` and
  `web-ui/src/components/chat/SimpleChatView.tsx`: the flex row that holds the panel is
  now `position: relative` — it is the containing block for both the floating tab and the
  absolute drawer. `position: fixed` with hardcoded offsets was rejected because the two
  views have different headers/footers, so one of them would always be misaligned.

`layoutStore` already defaults `infoSidebarOpen` to `window.innerWidth > 1100`, so on
mobile the panel starts collapsed; no store change was needed.

## Verification

- `npx tsc --noEmit -p tsconfig.app.json` — clean (run after each round of changes).
- Live browser checks against the Vite dev server (`http://localhost:5173/chat`). The
  browser tooling cannot resize the viewport, so responsive states were probed either in a
  390x800 iframe (media queries follow the iframe viewport) or at a 400px window:
  - collapsed at 400px: `document.querySelectorAll('aside').length === 0`, floating tab
    34x26 at `rightGap: 0`, chat inner width = 400 (full viewport).
  - expanded at 400px: panel `position: absolute`, 280px wide, `rightGap: 0`, `top: 48`,
    `bottom: 243`; backdrop height 327 (=65%); textarea at `top: 470` — clear of both.
  - screenshot at 400px confirms full-width chat, watermark, input and status bar with the
    tab floating at the top-right.
