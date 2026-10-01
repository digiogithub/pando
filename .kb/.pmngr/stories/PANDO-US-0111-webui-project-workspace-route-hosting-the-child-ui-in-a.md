---
id: PANDO-US-0111
type: story
title: "WebUI: project workspace route hosting the child UI in a keep-alive iframe"
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, webui]
estimate: 5
created: 2026-10-01T19:24:43Z
updated: 2026-10-01T19:24:43Z
---

## Description

Route `/projects/:id/workspace` (child of `MainLayout`, sidebar collapsed to rail by default) renders `components/projects/ProjectWorkspace.tsx`.

- An `<iframe>` per open tab with `src = webUrl` (`/api/v1/projects/{id}/web/`), all iframes kept mounted in a `ProjectFrameHost` rendered once in `MainLayout` and shown/hidden with `display`, so switching tabs preserves the child's React state, chat streams, terminal sessions and scroll. `sandbox` not used (same origin, needs storage/clipboard/downloads); `allow="clipboard-read; clipboard-write"`.
- Loading/error overlays driven by `projectTabsStore` state: `starting` (spinner + "Starting Pando in <path>"), `error` (stderr tail from the API + Restart/Close buttons), `stopped` (Reopen).
- `postMessage` bridge (`src/lib/projectFrameBridge.ts`) between parent and child UI, same origin, typed messages: child → parent `{type:'pando:title', title}` (session title for the parent title bar), `{type:'pando:busy', busy}` (animated logo / tab spinner), `{type:'pando:notification'}` (forward toasts/desktop notifications to the parent), `{type:'pando:shortcut', key}` (so `Ctrl+Alt+N` tab shortcuts work while the iframe has focus); parent → child `{type:'pando:focus'}`, `{type:'pando:theme', theme}`, `{type:'pando:language', lang}` to keep theme/language in sync with the parent. Child side lives in `packages/pando-client` behind `startupMode === 'project-child'`.
- Focus management: clicking a tab focuses the iframe's chat input; Esc inside the child does not close parent overlays.
- Desktop (Wails): verify the webview allows nested same-origin iframes with WebSocket (PTY) and that the title-bar drag region still works above the frame.

## Acceptance Criteria

- [ ] Switching tabs during a streaming reply keeps the stream running in the hidden frame; coming back shows the full text.
- [ ] Theme/language changes in the parent propagate to open child frames.
- [ ] Playwright: send a message in the child chat through the frame and assert the reply; open the child terminal (PTY over the proxy).
