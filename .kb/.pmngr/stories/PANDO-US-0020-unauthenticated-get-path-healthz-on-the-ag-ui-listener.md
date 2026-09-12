---
id: PANDO-US-0020
type: story
title: Unauthenticated GET {path}/healthz on the AG-UI listener
status: backlog
priority: high
parent: PANDO-EP-0004
milestone: PANDO-M-0001
author: claude
labels: [agui, ops]
estimate: 3
created: 2026-09-13T21:16:01Z
updated: 2026-09-13T21:16:01Z
---

## Description

As an operator, I want an unauthenticated health endpoint on the AG-UI listener, so that a load balancer, systemd unit or container orchestrator can probe `agui-serve` without holding the bearer token.

There is none today. `Runtime.Register` (`internal/agui/server.go:26-33`) mounts exactly `GET {path}/info`, `OPTIONS {path}/`, `POST {path}/{agent}` and `POST {path}`. The closest thing, `/info`, runs through `authorize()` (`server.go:159-161`) and its payload leaks the configured agent list and per-agent URLs (`server.go:226-238`). The main API's `/health` (`internal/api/routes.go:25`) is not registered on the dedicated listener — `Listener` mounts `Runtime.Handler()` only (`internal/agui/listener.go:88`).

Add `GET {path}/healthz`, registered in `Register` **before** the auth wrapper so it never consults the token, returning 200 and a minimal JSON body: status, Pando version, uptime. Later stories in this epic extend the payload with the concurrency gauge and the draining flag, so shape the response struct for that from the start.

Do NOT include the agent/profile list, the configured path, allowed origins, the token or anything token-derived, session ids, thread ids or counts that reveal user activity beyond the aggregate run gauge the concurrency story adds. Do NOT apply the `Origin` allow-list to it — an absent `Origin` already bypasses that check (`server.go:47-53`) and a probe has no Origin; a browser probing it is harmless given the payload contains nothing sensitive. Do NOT route it through `setCORSHeaders`.

## Acceptance Criteria

- [ ] `curl http://127.0.0.1:PORT{path}/healthz` with no `Authorization` header and no `?token=` returns 200 and a JSON body with status and version.
- [ ] The response body contains no agent names, no origins, no token, no session or thread identifiers; a test asserts the exact field set.
- [ ] `GET {path}/info` still requires the token (regression test), i.e. the new route did not widen `authorize`.
- [ ] The route is served by `pando agui-serve` on its dedicated listener and by the co-mounted `pando serve --agui-port` path, and is excluded from any auth middleware in both.
- [ ] Tests in `internal/agui/server_test.go`.

## Notes

Size: S. First story of PANDO-EP-0004 and a prerequisite for the observable parts of the concurrency-cap and graceful-drain stories, which extend this payload. Evidence: `report-agui-server.md` §5, Health row.
