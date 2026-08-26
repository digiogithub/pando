# Designer always-on, auto-open, live-reload, external links — delivered

Date: 2026-08-28. Synthesis of swarm 37fdaf30 (workers: backend-core
task-0697b432, surfaces-go task-1464b48f, webui task-6455be3b; verifier
task-75c582a1 PASS). Implements [[pando/plans/goal_designer_autoopen_always_on.md]].

## What changed

### 1. Design Studio always active (no TOML gate)
- `DesignConfig.Enabled` removed entirely from `internal/config/config.go`,
  app wiring (`internal/app/app.go`), tool registration
  (`internal/llm/agent/tools.go`), API (`internal/api/handlers_design.go`),
  tests and `.pando.toml` `[Design]` section.
- `MCPServer.Design.Enabled` remains only as the independent MCP tool-exposure
  switch (`cmd/mcp_server.go`).
- Stale-schema safety net: `internal/design/provider.go` `NewProvider` checks
  `sqlite_master` for `design_artifacts` and returns an actionable
  outdated-schema error; app startup keeps running and logs a warning
  (tested in `internal/design/provider_test.go`).

### 2. Auto-open designer surface on `design.EventCreated`
- **Desktop** (`cmd/desktop.go` + new `internal/desktop/launcher.go`):
  non-blocking `desktop.LaunchWindow` subscription on `design.EventCreated`,
  per-artifact dedupe, warn-only failure.
- **TUI** (`internal/tui/tui.go` + new `internal/design/browser_open.go`,
  `internal/auth/browser.go`): persistent `design.Events()` channel seeded in
  `internal/tui.New`, loop started from `appModel.Init`, resolves preview via
  `design.ResolveCreatedArtifactPresentation`, opens once per artifact with
  headless/browser-availability gating.
- **ACP** (new `internal/mesnada/acp/design_events.go`, `session.go`,
  `agent.go`): session-scoped subscription filtered by Pando session id,
  auto-open once per artifact, `resource_link` update with preview URL/title,
  subscription cancelled on close/entity-release.
- **WebUI** (`web-ui/src/App.tsx`, new
  `web-ui/src/components/design/DesignRouteEffects.tsx`): designStore
  `connect()` at App/router level for the full SPA lifetime; on
  `design.created` auto-navigates to `/design/<artifactId>` deduped by
  artifactId+nonce (`lastCreated` marker in `designStore.ts`); skipped in the
  Wails desktop shell via `hasDesktopAppBindings()`
  (`window.go?.desktop?.App` presence, `desktopRuntime.ts`); `DesignView` no
  longer disconnects on unmount.

### 3. Live-reload for browser previews
- `internal/design/preview/preview.go`: per-artifact atomic revision behind
  `GET /preview/<token>/_live`; served HTML always gets an inline poller that
  reloads on revision change; bridge injection layers on top (`bridge.go`).
- `internal/design/events.go` bumps the revision on `EventVersion` /
  `EventRender` when a preview server is installed. Covered by
  `internal/design/preview/preview_test.go`.
- WebUI studio canvas continues to reload via `reloadNonce` on
  version/render events.

### 4. External chat links open in a new window
- New shared `web-ui/src/components/shared/MarkdownLink.tsx` ReactMarkdown
  anchor override: absolute http(s) → `openExternal()` (Wails OpenInBrowser
  in desktop shell, `window.open(_blank, noopener)` otherwise) +
  preventDefault; `/...` in-app paths keep router behaviour; `design://`
  inert. Applied in `MessageBubble.tsx` and other agent-text markdown
  renderings.
- Stale "design disabled" copy removed from
  `web-ui/src/components/settings/DesignSystemSettings.tsx`.

## Files touched
Go: internal/config/config.go, internal/app/app.go,
internal/llm/agent/tools.go, internal/api/handlers_design.go,
cmd/mcp_server.go, cmd/desktop.go, internal/desktop/launcher.go,
internal/design/provider.go (+test), internal/design/browser_open.go,
internal/design/events.go, internal/design/preview/preview.go (+test),
internal/design/preview/bridge.go, internal/auth/browser.go,
internal/tui/tui.go, internal/mesnada/acp/{design_events,session,agent}.go,
.pando.toml.
Web: web-ui/src/App.tsx, components/design/{DesignRouteEffects,DesignView}.tsx,
packages/pando-client/src/stores/designStore.ts,
components/chat/MessageBubble.tsx, components/shared/MarkdownLink.tsx,
services/desktopRuntime.ts, components/settings/DesignSystemSettings.tsx,
internal/api/webui/dist (regenerated via make web-ui-embedded).

## Verification (verifier task-75c582a1, all items PASS)
- `grep -rn 'Design.Enabled' --include='*.go' internal/ cmd/` → only allowed
  `MCPServer.Design.Enabled` references.
- `go build ./...` passes (re-confirmed by synthesizer); changed Go files
  gofmt-clean (re-confirmed).
- `go test ./internal/config ./internal/design ./internal/design/preview
  ./internal/llm/agent ./internal/api ./internal/app ./cmd ./internal/db
  -count=1` passed (incl. browser e2e with google-chrome).
- web-ui: `bun run typecheck` passed; lint clean except pre-existing
  react-refresh warnings; embedded dist rebuilt.
- jj: working-copy changes only, no new commits by workers.

## Follow-ups
- None blocking. Pre-existing unrelated global gofmt backlog outside touched
  files remains (out of scope).

## Handoff finalization (2026-08-28)

- Inspected all 5 swarm tasks (3 workers, verifier PASS 0.95, synthesizer).
- Restored accidental local-dev edit in `mprocs.yaml` (`VITE_PORT=5555`);
  all remaining working-copy changes are intentional design work.
- No duplicate KB entries: one change doc (this file) + the goal plan, linked.
- Re-validated on final state: `go build ./...` OK,
  `go test ./internal/config ./internal/design ./internal/design/preview
  ./internal/llm/agent ./internal/api ./internal/app ./internal/db
  ./internal/mesnada/acp ./internal/auth -count=1` all pass.
- Only `Design.Enabled` refs left in Go: `MCPServer.Design.Enabled` in
  `cmd/mcp_server.go` (intended). `.pando.toml [Design]` has no `Enabled`.
- Still uncommitted in the JJ working copy (`feat: Pando Designer v1`).
