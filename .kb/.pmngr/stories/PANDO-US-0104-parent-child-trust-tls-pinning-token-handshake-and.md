---
id: PANDO-US-0104
type: story
title: "Parent–child trust: TLS pinning, token handshake and instanceregistry fields for web children"
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, backend, security]
estimate: 5
created: 2026-10-01T19:23:08Z
updated: 2026-10-01T19:23:08Z
---

## Description

`pando serve` always listens HTTPS with a self-signed cert generated under the project's `.pando/` (`tlsutil.EnsureCert`), and mints a per-process API token (`internal/api/server.go`). The parent must talk to the child securely without the browser ever seeing the child's credential.

- Parent passes its own certificate to the child: `--tls-cert/--tls-key` from the parent's `ServerConfig` (or, when the parent runs plain HTTP in desktop mode, a dedicated cert generated once under the parent's data dir). The proxy HTTP client pins that cert (`tls.Config.RootCAs` with only that cert, `ServerName` fixed) — no `InsecureSkipVerify`.
- Token: after `/health` is green the parent calls `GET /api/v1/token` on the child (allowed on loopback binds, see `tokenEndpointAuthenticated`) and stores it in `WebInstance.Token` in memory only. On re-adoption the token is re-read the same way.
- `instanceregistry.Entry` gains `WebPort int` and `ParentInstanceID string` (JSON `web_port`, `parent_instance_id`); `cmd/serve.go`, `cmd/app.go`, `cmd/desktop.go` fill `WebPort` with the HTTP port and `ParentInstanceID` from `PANDO_PARENT_INSTANCE`. `GET /api/v1/instances` exposes both.
- Child must bind loopback only: parent refuses to adopt/open a child whose `Entry.Path` or bind host is not `127.0.0.1`/`localhost`.
- The child's basic-auth config (`[Server.BasicAuth]`, AGE users) is bypassed by the proxy because the parent already authenticated the browser; document this and make sure the child enforces basic auth only for non-parent callers if it is ever reachable directly (it is loopback-only, so this is informational).

## Acceptance Criteria

- [ ] Proxy client connects to the child with a pinned cert; a child presenting another cert is rejected (test with two generated certs).
- [ ] Child token is held in memory, never written to DB, never returned by any `/api/v1/projects*` endpoint.
- [ ] `instanceregistry` entries of serve/app/desktop carry `web_port` and `parent_instance_id`; TUI instances browser still parses old entries.
- [ ] Tests in `internal/instanceregistry` and `internal/project`.
