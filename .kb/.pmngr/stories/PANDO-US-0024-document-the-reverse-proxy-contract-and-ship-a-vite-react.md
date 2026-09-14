---
id: PANDO-US-0024
type: story
title: Document the reverse-proxy contract and ship a Vite/React 18 example
status: done
priority: medium
parent: PANDO-EP-0004
milestone: PANDO-M-0001
author: claude
labels: [agui, docs, sdk]
estimate: 5
created: 2026-09-13T21:16:01Z
updated: 2026-09-13T21:16:01Z
---

## Description

As a developer putting Pando behind a product's own backend, I want the proxy contract written down and a worked SPA example that does not need CopilotKit, so that I stop discovering the rules by reading `internal/agui/server.go` and stop being told to stand up a Next.js process.

The only worked client example today is `examples/copilotkit/`, a Next.js App Router app that requires a Node runtime speaking GraphQL to CopilotKit's own runtime — a hop Pando deliberately refuses to implement (`internal/agui/doc.go`, "Deliberately not implemented"). A plain SPA has no reference at all.

**Part 1 — proxy contract**, as a section in `internal/agui/doc.go` and mirrored in the SDK README (`sdk/typescript/README.md`, which currently documents the agui subpath around lines 180-230 without any of this):

- `Origin` semantics: `authorize` (`server.go:47-53`) skips the allow-list entirely when the header is **absent**, so a server-to-server proxy that does not set `Origin` needs no `AllowedOrigins` entry and that is the recommended shape. If the proxy forwards the browser's Origin, the exact string must be listed — `originAllowed` (`server.go:85-92`) is exact case-insensitive match or `*`, with no wildcard subdomains or port patterns. State plainly that `agui-serve`'s "no allowed origins" startup warning is *correct* to ignore for a proxied deployment.
- Token: `Authorization: Bearer` or `?token=` (`bearerToken`, `server.go:73-79`). Document that `?token=` exists for `EventSource` and should never be used from a browser — it lands in access logs, `Referer` and history — and that a proxy should strip an inbound `?token=` and set the header itself, keeping the Pando token server-side.
- Streaming: Pando sets `X-Accel-Buffering: no` and flushes per event (`internal/agui/sse.go:38-45`) and sends a `: keep-alive` comment every 15 s (`deps.go:86`, `server.go:419-423`). A Go `httputil.ReverseProxy` therefore needs `FlushInterval: -1`, no response buffering, and a read/idle timeout above 15 s. The listener sets `WriteTimeout: 0` on purpose (`listener.go:88-92`); the proxy must match.
- `/info` URL rewriting: URLs are built from `req.Host`, honouring `X-Forwarded-Proto` for the scheme only and deliberately ignoring `X-Forwarded-Host` (`server.go:226-238`), so behind a path-rewriting proxy those URLs are wrong — rewrite `Host` upstream or ignore the URLs and construct your own from the configured origin plus the discovered path.
- TLS: `agui-serve` self-signs into the data dir unless `--no-tls` (`cmd/agui_serve.go`); on loopback behind a proxy, `--no-tls` is the pragmatic choice, otherwise pin the cert.
- Body limit: 8 MiB via `io.LimitReader` (`sse.go:13`, `input.go:169-182`).
- A copy-pasteable Go `httputil.ReverseProxy` snippet that does all of the above: strips `Origin`, strips inbound `?token=`, sets the bearer header, `FlushInterval: -1`.

**Part 2 — example**: `examples/vite-react/`, a minimal Vite + React 18 SPA on `@pando-ai/sdk/agui` showing streaming text, reasoning output, tool cards, a permission prompt and state rendering (todos, token budget, touched files), plus the Go proxy from Part 1 as the only backend hop. No CopilotKit, no Next.js, no Node server beyond the proxy.

Do NOT delete or deprecate `examples/copilotkit/` — it stays as the CopilotKit reference. Do NOT introduce `@ag-ui/client` in the new example.

## Acceptance Criteria

- [ ] `internal/agui/doc.go` has a proxy-contract section covering Origin, token handling, `FlushInterval: -1` / no buffering / timeouts, heartbeat, `/info` URL rewriting, TLS and the body limit, with the file:line facts above.
- [ ] The SDK README carries the same contract and the Go `httputil.ReverseProxy` snippet, which compiles as a Go example test so it cannot rot.
- [ ] `examples/vite-react/` builds and runs against `pando agui-serve --no-tls` on loopback with its proxy, and its README gives the exact commands.
- [ ] The example renders streaming text, reasoning, a tool call, a permission prompt answered through the SDK's HITL helpers, and the state document.
- [ ] Nothing in the example depends on CopilotKit, Next.js or `@ag-ui/client`.

## Notes

Size: M. Depends on PANDO-US-0006 (browser-safe package build) — the example imports the package from a Vite build — and consumes the `PandoThread` and HITL helpers from PANDO-EP-0001, so schedule it after those land. Decision already taken: CopilotKit is out for SPA consumers, and the recommended proxy shape strips `Origin` and keeps `AllowedOrigins` empty. Evidence: `report-agui-server.md` §4 and `report-sdk-agui-client.md` §4-§5.
