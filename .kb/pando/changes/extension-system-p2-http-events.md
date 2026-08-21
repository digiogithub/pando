---
created_at: 2026-08-21T14:45:28.59123895Z
updated_at: 2026-08-21T14:45:28.59123895Z
tags:
    - change
    - extensions
    - api
    - events
---
# P2 — Extension HTTP endpoints, HTTP middleware and event subscribers

Date: 2026-08-21
Status: DONE
Plan: [[pando/analysis/extension_system_enterprise_analysis]] (phase P2)
Builds on: [[pando/changes/extension-system-p1-tools-commands]]

## What was done

Phase P2 gives extensions a server surface and a way to observe what happens in
Pando: private REST endpoints, request middleware (the hook an enterprise SSO or
audit module needs), and lifecycle events for sessions, messages and
permissions — the feed a corporate memory sink consumes.

### Contract additions — `pkg/extension`

`pkg/extension/http.go` (new):

- `Route{Pattern, Handler}` — `Pattern` is relative to the extension's base path
  and may carry a method prefix (`"GET reports/{id}"`).
- `HTTPEndpointProvider{BasePath() string; Routes() []Route}`.
- `HTTPMiddlewareProvider{Priority() int; WrapHTTP(next http.Handler) http.Handler}`.

This contract uses `net/http` directly. Unlike tools and commands there is
nothing to abstract: `net/http` is the standard library, both sides already
agree on it, and a bespoke handler type would only cut the extension off from
every existing middleware.

`pkg/extension/event.go` (new):

- `EventType` (`EventCreated`/`EventUpdated`/`EventDeleted`), topics
  `TopicSession`, `TopicMessage`, `TopicPermission`.
- `Event{Topic, Type, ID, SessionID, Payload map[string]any, Time}`.
- `EventSubscriber{Topics() []string; HandleEvent(ctx, Event)}` — an empty topic
  list means every topic, including ones added later.

### Host wiring

| File | Role |
|---|---|
| `internal/extensions/http.go` | `ExtRoutePrefix`, `RegisterRoutes(mgr, mux)`, `WrapHTTP(mgr, next)` |
| `internal/extensions/events.go` | `HasEventSubscribers`, generic `Forward[T](ctx, mgr, topic, src pubsub.Suscriber[T])` |
| `internal/api/routes.go` | Mounts extension routes first, before core routes |
| `internal/api/server.go` | `routed := extensions.WrapHTTP(app.Extensions, mux)` inside core's auth/CORS |
| `internal/app/app.go` | `startExtensionEventFanout` forwards Sessions/Messages/Permissions |

## Design decisions worth recording

- **Everything lives under `/api/ext/<base>/`.** A base path must be a single
  ID-shaped segment; `""`, `"/"`, `"acme/sub"`, `"Acme"` and `"../etc"` are all
  refused. Core routes and extension routes can therefore never collide, and a
  reverse proxy can treat the whole extension surface as one prefix.
- **Extension routes are registered before core routes** so that an extension
  claiming a core pattern makes `ServeMux` complain at startup rather than
  silently winning.
- **A duplicate pattern panics in `ServeMux`; that panic is contained.** The
  first route wins, the second is logged and dropped, and the server still
  starts with the rest of the API. Same for a duplicate base path between two
  extensions: refused, not resolved.
- **Extension HTTP middleware runs *inside* core's CORS, basic-auth and token
  checks.** Adding an extension must not be able to weaken the API, so a request
  that fails core auth never reaches an extension. Static WebUI assets are
  outside the chain too. The contract documents this explicitly, because the
  first draft claimed the opposite.
- **Highest priority is outermost** for HTTP middleware (it sees the request
  first), which is the mirror image of the tool-filter rule where lower runs
  closer to the tool. Ties break on extension ID.
- **Events carry the JSON form of the resource, not the internal type.** Core's
  brokers are generic over `session.Session`, `message.Message` and friends,
  which an out-of-tree module cannot name. `Forward` marshals through JSON — the
  same shape the REST API already exposes, so nothing new becomes public, and
  the subscriber cannot mutate host state through the payload. `ID` and
  `SessionID` are lifted out of the payload by trying the usual key spellings.
- **Events are dropped, never queued, for a slow subscriber.** The alternative
  is unbounded memory growth in the host because an extension misbehaves. An
  extension that must not lose events buffers them itself; the contract says so.
- **No subscriber means no goroutine.** `HasEventSubscribers` gates the whole
  fan-out, so a standard build pays nothing. The fan-out context is registered
  in `watcherCancelFuncs`, so `Shutdown` stops it like every other loop.
- **Panic containment**: a subscriber that panics is logged and the fan-out
  survives for the others; a middleware that panics while wrapping (or returns
  nil) is dropped rather than taking the server down.

## Verification

- `go build ./...`, `go vet ./internal/extensions ./internal/api ./internal/app ./pkg/extension` — clean.
- `go test ./internal/extensions ./internal/api ./internal/app ./pkg/extension ./internal/commands ./internal/llm/agent` — all pass.
- New tests:
  - `internal/extensions/http_test.go` (8): routes mount under the prefix and
    serve (including `{id}` wildcards and method prefixes), bad base paths
    refused, duplicate base path refused, duplicate pattern panic contained, nil
    manager, middleware order, middleware can reject a request, panicking/nil
    middleware contained.
  - `internal/extensions/events_test.go` (5): delivery with ID/SessionID/payload
    extraction, topic filtering, panicking subscriber does not stop the fan-out,
    no-op without subscribers, subscription released on context cancel.
- **End-to-end** with the composed enterprise binary (demo extension extended
  with an endpoint): `GET /api/ext/demo/ping` with a valid `X-Pando-Token`
  returns `pong from https://corp.internal` — i.e. the route is mounted *and*
  the handler sees provisioned state; an unknown path under the same base
  returns 404; without the token core auth returns 401 before the extension is
  reached, which is the intended layering.
- `alchemai-agent/compat` asserts `HTTPEndpointProvider`,
  `HTTPMiddlewareProvider` and `EventSubscriber`; the module builds and vets.

## Not in this phase

- Event fan-out was verified by unit tests rather than in the composed binary:
  creating a session over REST requires a configured provider (`POST
  /api/v1/sessions` is read-only; sessions are born from a chat turn).
- No `EventPublisher` in the other direction — an extension cannot inject events
  into core's brokers. Nothing needs it yet.
- Frontend assets (`FrontendProvider` / `FrontendReplacer`) remain P4, and
  `xpando` + the build matrix remain P3.
