---
created_at: 2026-09-25T15:47:54.185623298Z
updated_at: 2026-09-25T15:47:54.185623298Z
tags:
    - change
    - dependencies
    - security
---
# Dependabot alerts triage and easy bumps (2026-09-25)

## What changed
- go.mod/go.sum: google.golang.org/grpc 1.83.1 -> 1.83.2 (alert #111, xDS DoS); go.opentelemetry.io/otel/sdk, otlptrace, otlptracehttp -> 1.45.0 (alerts #119-121, endpoint URLs leaked in logs). Transitive bumps: golang.org/x/{crypto,net,sync,term,text}, genproto, proto/otlp 1.11.0.
- package.json / package-lock.json (root): sharp ^0.35.4 (alert #118, libheif).
- examples/copilotkit: next 16.3.6 (critical RCE alerts #115/#116), hono 4.13.9 (#112-114), sharp 0.35.4 (#117), qs + nested uuid via `npm audit fix` (#109/#110/#81). `npm audit` reports 0 vulnerabilities.

## Dependabot PRs
- #17 (grpc 1.83.2) and #16 (hono 4.13.7) were NOT already fixed on main; the local changes supersede them. They close automatically once pushed.

## Not fixed (no easy path)
- github.com/docker/docker v28.5.2+incompatible (#23, #29, #30, #31): no patched version in the docker/docker module; fix requires migrating to github.com/moby/moby/client modules.
- github.com/disintegration/imaging (#1, low): unmaintained, no patch.

## Verification
- `go build ./...` OK; `go test ./internal/telemetry/... ./internal/llm/agent ./internal/api` OK.
- Lockfiles updated with `--package-lock-only --ignore-scripts`; examples not built.
