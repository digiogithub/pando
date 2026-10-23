---
created_at: 2026-10-27T14:56:30.400049119Z
updated_at: 2026-10-27T14:56:30.400049119Z
tags:
    - fix
    - webui
    - chat
---
# Fix: chat scroll jumps with fast-streaming models (web + desktop)

## Date
2026-10-27

## Problem
With models that stream very fast, the chat thread jumped up and down instead of growing smoothly while following the bottom.

## Root cause
`web-ui/src/components/chat/MessageList.tsx` called `el.scrollTo({ top: scrollHeight, behavior: 'smooth' })` on every streamed chunk. Each new smooth scroll restarts the animation from the current position before the previous one lands, so with many chunks per second the viewport lagged and lurched. Height changes not covered by the effect deps (markdown/highlight settling, images, tool rows) caused extra jumps.

## Fix
- Streamed growth pins instantly (`behavior: 'auto'`); only a newly sent user message animates (`'smooth'`). Jump-to-bottom button still smooth.
- `ResizeObserver` on `.chat-thread` re-pins at most once per animation frame while following (guarded when `ResizeObserver` is unavailable, e.g. jsdom).
- Follow/stop-follow logic (`onScroll`, movedUp guard) unchanged.
- Test: `MessageList.test.tsx` asserts the follow call uses `{ top, behavior: 'auto' }`.

## Verification
vitest full suite (27 files, 131 tests), tsc, eslint pass. Not measured in a browser with a real fast model.
