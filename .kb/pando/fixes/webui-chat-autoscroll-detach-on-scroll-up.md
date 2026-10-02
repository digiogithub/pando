---
created_at: 2026-10-02T14:53:31.062205168Z
updated_at: 2026-10-02T14:53:31.062205168Z
tags:
    - fix
    - webui
    - desktop
    - chat
    - scroll
---
# Fix: WebUI/desktop chat auto-scroll fought the reader while streaming (2026-10-02)

## Problem
While the agent streamed text, scrolling up in the chat was yanked back to the bottom.

Cause, in `web-ui/src/components/chat/MessageList.tsx`: auto-scroll was gated only on
"distance from bottom < `STICK_THRESHOLD` (80px)". A wheel/touchpad step smaller than 80px
left the view "at bottom", so the next streamed token called `scrollTo({behavior:'smooth'})`
again and cancelled the reader's scroll. The reader could never leave the 80px band.
The "Scroll to bottom" pill also only scrolled; it did not set the flag itself.

## Change
`MessageList` now tracks an explicit `following` flag (ref + state) instead of `atBottom`:

- `onScroll` compares `scrollTop` with the previous value. Any upward move turns
  `following` off immediately, regardless of the threshold. Programmatic scrolls only
  move down, so an upward move is the reader's.
- Guard: if `scrollHeight` shrank (collapsed block, browser clamps `scrollTop`), it is not
  treated as a reader scroll.
- `following` turns back on when the reader reaches the bottom (within `STICK_THRESHOLD`),
  on the pill (`jumpToBottom` sets the flag and scrolls), when a new user message is sent,
  and on session open.
- Mid-flight smooth-scroll positions far from the bottom no longer stop the follow
  (the threshold only re-enables, it never disables).
- The "new user message" scroll now fires only when `messages.length` grows with a user
  message last, not on every dependency change while the last message is the user's.
- The pill is shown while `!following`.

Used by both `ChatView` and `SimpleChatView`, so WebUI and desktop (Wails) are covered.
`RemoteSessionView` already had its own sticky scroll, see
[[pando/fixes/webui-instances-panel-sessions.md]]. Original behaviour came from
[[pando/features/webui_redesign_p4_chat_experience.md]].

## Files
- `web-ui/src/components/chat/MessageList.tsx`
- `web-ui/src/components/chat/MessageList.test.tsx` (new, 7 cases)

## Verification
- `npx vitest run src/components/chat/MessageList.test.tsx` — 7 passed.
- `npm run typecheck`, `eslint` on both files — clean.
- Not exercised in a real browser/desktop build (jsdom has no layout; geometry is mocked).
