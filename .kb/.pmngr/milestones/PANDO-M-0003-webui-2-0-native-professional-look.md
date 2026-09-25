---
id: PANDO-M-0003
type: milestone
title: "WebUI 2.0: native, professional look"
status: done
author: mcp
labels: [webui, design, theme]
created: 2026-09-24T21:00:26Z
updated: 2026-09-25T10:49:19Z
closed: 2026-09-25T10:49:19Z
due: 2026-10-15
---

## Description

Delivery checkpoint for the WebUI visual overhaul. The UI should move from its improvised look to a clean, native desktop-style app, close to Claude Desktop and Zeron. Light and dark modes switch with one button, colour schemes stay configurable, and the mascot watermark is gone from the chat.

## Acceptance Criteria

- [ ] Epic "WebUI native redesign" is done.
- [ ] `bun run typecheck`, `bun run lint`, `bun run build` and `bun run build:embedded` pass in `web-ui/`.
- [ ] Light and dark screenshots of chat, settings and editor have been reviewed at desktop and mobile widths.

## Notes

Plan: KB `pando/plans/webui_native_redesign_plan.md`.
