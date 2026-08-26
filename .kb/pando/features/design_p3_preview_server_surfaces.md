---
created_at: 2026-08-26T20:25:44.061620787Z
updated_at: 2026-08-26T20:25:44.061620787Z
tags:
    - feature
    - design
    - preview
    - api
    - sse
    - security
---
# Design Studio P3 — Preview server, surface plumbing, SSE and the external-access guard

Implemented 2026-08-26. Phase P3 of [[pando_design_studio_plan]], on top of
[[design_p2_tools_patch_engine]] (tools + patch engine) and the P0/P1 foundations.

## What was built

### 1. `internal/design/preview` — a standalone, design-agnostic preview server

New package, deliberately importing **nothing** from `internal/design`. It is handed a
directory plus an entry document and hands back a URL. That direction of dependency is what
lets `internal/design` import it (to mint URLs) while `internal/api` mounts the very same
handler on the main listener — no import cycle, one implementation, two deployments:

- **mounted**: `internal/api` registers `Server.ServeHTTP` under `/preview/` on the API
  listener, so previews live on the Pando origin and inherit its bind address and its
  basic-auth gate.
- **loopback**: processes with no API server (plain TUI, ACP, CLI) call `StartLoopback()`,
  which binds `127.0.0.1:0` and serves the same routes.

Files: `preview.go` (registry + handler), `bridge.go` (embed + injection), `bridge.js`,
`preview_test.go`.

Key types/symbols: `Prefix = "/preview/"`, `BridgePath = "/preview/_bridge.js"`,
`DefaultTTL = 12h`, `ErrForbidden`, `Grant{Token, ArtifactID, SessionID, Dir, Entry,
ExpiresAt}`, `Options{BaseURL func() string, Access func() error, TTL, Inject []byte}`,
`Server.Publish/URL/Revoke/RevokeSession/Grants/StartLoopback/Addr/Close/ServeHTTP`,
`URLOptions{Slide, Bridge, Doc}`.

Design decisions worth keeping:

- **Token in the path is the capability.** `/preview/<32-hex>/…`, bound to the session that
  published it and expiring with the grant. A wrong token returns **404, never 403**, so it
  cannot be used to confirm that some other artifact exists.
- **The token is stable across republishes.** `Publish` is idempotent per artifact, so an
  iframe already open survives every iteration instead of losing its URL on each render.
- **`http.ServeContent`, not `http.ServeFile`.** `ServeFile` 301-redirects any path ending
  in `/index.html` to `./`, which would rewrite the exact URL the grant token makes stable.
  This was caught by a failing test (`got 301`), not by review.
- **Strict CSP** on every preview response: `default-src 'self' data: blob:`,
  `connect-src 'none'`, `frame-ancestors 'self'`, `base-uri 'none'`, `form-action 'none'`,
  plus `nosniff`, `no-referrer`, `Cache-Control: no-store`. Inline script/style stay
  allowed on purpose — design artifacts are single-file HTML with inline `<style>`/`<script>`
  by construction — and `connect-src 'none'` is what actually matters: the document cannot
  reach `/api/*` and cannot exfiltrate.
- **Path-escape checks** on every request, including through symlinks (`filepath.EvalSymlinks`
  on both sides before the prefix comparison).
- Read-only: anything other than GET/HEAD is 405.
- Expired grants are swept on publish, so an idle process keeps no goroutine for it.

### 2. The selection bridge (`bridge.js`) and the `Inject` option

Injected **only** into `?bridge=1` requests, and only in the HTTP response — nothing is ever
written back to the artifact file. A plain preview, an export, or a directly-opened file
carries no instrumentation at all (asserted by `TestPlainPreviewIsUninstrumented` in a real
browser).

The bridge gives hover outline, click-to-select (reported to the parent frame as
`{source:"pando-design", type:"selected", selection:"design://<node_id>", …}`), a
`select`/`goToSlide`/`clearSelection` inbound message API, and `#slide-N` deep-link
resolution against the stamped slide index. v1 stays select-and-ask: it never mutates the
document beyond its own overlay.

**The hard problem it solves:** `data-pando-id` is stamped by the renderer in the *live DOM*
and never written to files, so a preview served straight off disk has no ids to select. The
fix is `Options.Inject`: `internal/design.previewStampScript()` builds a preamble from the
renderer's **own** `indexScript` (via `buildIndexScript`), so the id a user clicks in the
browser is the id the stored node index holds. A second, independent implementation would
number elements differently and silently break the selection protocol. The preamble also
stamps `data-pando-slide` (1-based) for the bridge, and registers on `DOMContentLoaded`
ahead of the deferred bridge tag so ids exist before the bridge indexes them.

### 3. `internal/design` glue

- `preview_link.go`: `SetPreviewServer/PreviewServer/EnsurePreviewServer/ClosePreviewServer`,
  `PreviewOptions(baseURL, access)`, `Service.PublishPreview`, `Service.publishPreviewOn`,
  `previewStampScript`.
  **`EnsurePreviewServer` (lazy loopback) is only called from `design_present`.** Render and
  export deliberately stay socket-free: they run in tests, batch exports and headless critic
  passes, none of which want a listener.
- `present.go`: `Presentation` now carries `URL` (served preview when one is running,
  `file://` otherwise), `FileURL` (always the file address) and `BridgeURL` (empty without a
  server).
- `events.go`: `EventCreated|EventVersion|EventRender|EventCritique`, flat `Event` struct,
  `Events() *pubsub.Broker[Event]` created on first use, `Service.publish`. Published from
  `Create`, `CommitVersion` and `Render`. Note the ordering: `Create` commits version 1
  before the artifact record is complete, so `design.version(v1)` legitimately precedes
  `design.created` — a test pins that sequence so a timeline built from the stream cannot
  drift silently.

### 4. `internal/api` — REST, SSE, mounting and the guard

`handlers_design.go` (new): `GET /api/v1/design/artifacts`, `GET …/artifacts/{id}`,
`GET …/{id}/versions`, `POST …/{id}/checkout`, `GET …/{id}/nodes`, `POST …/{id}/render`,
`GET …/{id}/screenshot` (PNG), `POST …/{id}/export`, `GET …/{id}/download`,
`GET /api/v1/design/events` (SSE with a 25s keep-alive comment). Registered in `routes.go`
only when `s.preview != nil`, i.e. only when `design.enabled` is on.

- `setupPreview()` runs next to `setupAGUI()` in `NewServer`; `Server.preview` field added.
- `previewBaseURL()`: scheme from the TLS config, a wildcard bind resolves to `127.0.0.1`
  (the absolute URL exists for opening a browser on this machine; remote clients use the
  path against their own origin).
- `resolveArtifactExport(absDir, workingDir, rel)`: extracted so the one endpoint that names
  a file cannot become a project-wide file-read primitive.

**The external-access guard (the security core of P3):**

- `previewAccess()` refuses with `preview.ErrForbidden` whenever the bind host is
  non-loopback **and** basic auth is not enabled with at least one user. It is consulted on
  every publish *and* every request, so flipping external access on closes the door on URLs
  already sitting in someone's browser.
- `basicAuthEnforced` was widened from `/api/` to `/api/` **plus** `/preview/`. Previews
  serve files out of the user's working tree; the unguessable token is a capability, not an
  authentication factor. Without this, turning external access on would have published every
  artifact directory to the whole network.
- `uiHandler` now routes `/preview/` to the API handler; otherwise the SPA fallback would
  have answered it with `index.html` and the user would see a blank frame.
- `authMiddleware` deliberately leaves `/preview/` alone: a browser loading an iframe cannot
  attach the API token, and the path token already gates it.

### 5. Tool surface

`design_present` gained `open: boolean` — ensures a preview server exists (starting the
loopback fallback if needed) and opens the address with `auth.OpenBrowser`. A browser that
fails to open is reported, never fatal.

`internal/app/app.go` calls `design.ClosePreviewServer()` before `design.CloseDefaultProvider()`
in `Shutdown`.

## Verification

- `go build ./...`, `go vet ./internal/design/... ./internal/api ./internal/llm/tools
  ./internal/llm/agent ./internal/app ./cmd` — clean. `gofmt` clean on every file touched
  (the repo's pre-existing unformatted files were not touched).
- `go test` green for `./internal/design`, `./internal/design/preview`, `./internal/api`,
  `./internal/llm/tools`, `./internal/llm/agent`, `./internal/config`, `./internal/snapshot`,
  `./internal/app`, `./cmd`.
- **17 preview tests**: entry+asset serving, unknown token indistinguishable from a missing
  file, path escape (`../`, encoded, nested, absolute), symlink escape, CSP/nosniff/no-store
  headers, bridge injected only on `?bridge=1` and ordered after the preamble, token-free
  bridge asset, grant expiry, token stability across republish, `RevokeSession`, access guard
  on publish *and* on request, URL shape with slide+bridge, loopback listener over real HTTP,
  405 on POST, publish validation, fragment documents.
- **6 API tests**: refused on `0.0.0.0` without basic auth (403, no content leak), served on
  `0.0.0.0` once basic auth is on, served on loopback with no auth, `basicAuthEnforced`
  covers `/preview/` but not `/health` or SPA assets, `previewBaseURL` wildcard→loopback and
  TLS→https, download path containment.
- **1 middleware-chain regression test**: `/preview/…` survives
  `cors(uiHandler(basicAuth(auth(mux))))` — not swallowed by the SPA fallback, not rejected
  for lack of an API token, CSP intact — while `/design` still reaches the SPA.
- **4 design tests**: presentation prefers the served preview and keeps `FileURL`/`BridgeURL`
  (and the URL actually answers 200 over HTTP), falls back to `file://` with no server, the
  stamp preamble is the renderer's own walker, lifecycle events published in the pinned order.
- **2 new browser E2E tests against real Chrome (both PASS)**:
  `TestEndToEndBridgedPreviewInARealBrowser` — create, render, index, publish, load the
  bridged URL in Chrome, assert the browser-stamped `data-pando-id` on `#hero h1` equals the
  id in the stored index, then drive the bridge with a `select` postMessage and read back the
  marked element. `TestPlainPreviewIsUninstrumented` — a non-bridged preview carries zero
  stamped elements.

## P3 exit criterion

Met: the URL opens in a real browser from `pando` TUI (lazy loopback server via
`design_present open:true` + `auth.OpenBrowser`), from `pando app` (mounted on the API
listener), and remotely with external access + basic auth on (guard allows it only then, and
`basicAuthEnforced` demands credentials).

## Known limits

- Grants expire on TTL (12h) or explicit revoke; they are not yet dropped when a session row
  is deleted — `Server.RevokeSession` exists but is not wired to session deletion.
- The absolute preview URL for a wildcard bind is loopback-based; a remote surface must build
  its own origin from the returned path.

## Next — P4

WebUI Design Studio: top-level "Design" nav entry and routes, Studio layout
(chat | canvas | inspector) consuming `/api/v1/design/*` and the SSE stream, artifact
gallery, version timeline with thumbnails, export menu, and deck mode (slide strip,
per-slide export) driving the bridge over `postMessage`.

Related: [[pando_design_studio_plan]] · [[design_p2_tools_patch_engine]] ·
[[feature_external_access_footer_toggle]] · [[feature_webui_basic_auth]] ·
[[project_webui_implementation_plan]] · [[project_snapshot_plan]]
