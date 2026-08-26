---
created_at: 2026-08-27T22:09:47.982671469Z
updated_at: 2026-08-27T22:09:47.982671469Z
tags:
    - plan
    - design
    - goal
---
# Goal: Pando Designer — always-on, auto-open surfaces, real-time canvas

Date: 2026-08-27. Follow-up to [[pando_design_studio_plan]] (P0–P8 implemented).

## User request
1. When the `design_*` tools are used, a designer screen must appear at once,
   showing the artifact rendered in real time (Claude Design / OpenDesign style).
2. The feature must be ALWAYS ACTIVE — no `design.enabled` TOML gate.
3. Desktop mode → designer opens in a separate window; TUI/ACP → default browser.
4. External URLs in WebUI/desktop chat must open in a new window, not navigate
   the SPA away (Pando UI disappears today).

## Diagnosis (verified 2026-08-27)
- Core pipeline works: `TestEndToEndRenderInspectPatchRender` passes; a full
  `pando -p` run with design_create+design_render succeeds when enabled.
- Generation error root cause observed in user session history:
  `no such table: design_artifacts` — stale DB schema (missing design
  migration) in the instance that served the user; plus the feature being
  off (`[Design] Enabled = false` in .pando.toml) means tools are not even
  registered and the provider is never wired (`internal/app/app.go:688`).
- `designStore.connect()` (SSE `/api/v1/design/events`) is only called when
  `/design` is mounted → chat sessions never see `design.created` → no
  auto-open in WebUI.
- No auto-open anywhere: TUI/ACP/desktop never react to `design.EventCreated`.
- `MessageBubble.tsx` ReactMarkdown has no `a` override → links navigate the
  SPA away.

## Work decomposition (swarm)
- W1 backend-core: remove `DesignConfig.Enabled` everywhere; always wire the
  provider; clear "stale schema" error in the provider; desktop second-window
  launch (`internal/desktop` detached launcher) wired from cmd/desktop.go on
  `design.EventCreated` (once per artifact) → `<base>/design/<id>`.
- W2 surfaces-go: TUI + ACP subscribe to `design.Events()` EventCreated →
  `auth.OpenBrowser` (dedup per artifact); preview server live-reload
  injection so plain browser previews refresh on render/version.
- W3 webui: global design SSE at App level; auto-navigate to `/design/<id>`
  on `design.created` (skip inside Wails shell — backend opens the window);
  external chat links open in new window via `openExternal`; stale
  DesignSystemSettings text fixed; `make web-ui-embedded`.

## Decisions
- `DesignConfig.Enabled` removed (not ignored). `MCPServer.Design.Enabled`
  stays (separate MCP-exposure switch).
- Auto-open triggers on `design.EventCreated` only (not per render).
- Desktop detection in WebUI: `window.go?.desktop?.App` presence.
- Preview live-reload: small polling script injected by the preview server;
  design bumps a per-artifact revision on render/version.

Verification: go build ./..., gofmt, go test (config, design incl. browser
e2e, api, tui, acp, app, cmd), web-ui embedded build.
