---
id: PANDO-EP-0004
type: epic
title: AG-UI operability for embedded deployments
status: done
priority: medium
milestone: PANDO-M-0001
labels: [agui, ops, docs]
created: 2026-09-13T21:11:26Z
updated: 2026-09-13T21:11:26Z
---

## Description

What an operator needs before putting `agui-serve` behind a product: today there is no unauthenticated health check on the AG-UI listener (`/info` needs the token and leaks the agent list), no metrics, no concurrency cap (`AgentPoolSize` caps pooled instances, not in-flight runs, so load turns into unbounded goroutines), and `Runtime.Close` hard-cancels every run on shutdown. The token is generated per process and printed to stdout; `PANDO_AGUI_TOKEN` appears only inside a `--help` example string and is never read. The only worked client example is a Next.js + CopilotKit app, and the reverse-proxy contract (absent `Origin` bypasses the allow-list, `?token=` accepted as a bearer fallback, `FlushInterval`, heartbeat comments) is undocumented.

## Acceptance Criteria

- [ ] `GET {path}/healthz` answers 200 plus version without a token and leaks no config, agent list or session data.
- [ ] `[AGUI] MaxConcurrentRuns` rejects a run over the cap with 503 and `Retry-After` before any session is created or agent built; the cap and current usage are observable in logs and the health payload.
- [ ] SIGTERM with runs in flight waits up to a configurable deadline for them to finish or checkpoint; their messages are persisted.
- [ ] `agui-serve` reads `PANDO_AGUI_TOKEN` and `--token-file`, precedence `--token` > `--token-file` > env > generated; the token is never logged; the `--help` text matches.
- [ ] `internal/agui/doc.go` and the SDK README document the proxy contract with a copy-pasteable Go `httputil.ReverseProxy` snippet, and a Vite/React 18 example without CopilotKit runs against `agui-serve --no-tls` on loopback.

## Notes

Evidence in `report-agui-server.md` §4-§5 and `report-sdk-agui-client.md` §4-§5. The embedding host imposes its own per-user in-flight cap in its proxy meanwhile.
