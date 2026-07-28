---
created_at: 2026-07-28T21:40:15.384562454Z
updated_at: 2026-07-28T21:40:15.384562454Z
tags:
    - feature
    - agui
    - copilotkit
    - hardening
    - cli
    - migration
    - implementation
---
# Feature: AG-UI adapter — P5 (durable threads, hardened discovery, dedicated listener)

**Date:** 2026-07-28
**Status:** P5 DONE. Builds on [[pando/features/agui_adapter_p4_hitl.md]], [[pando/features/agui_adapter_p2_p3.md]] and [[pando/features/agui_adapter_p0_p1.md]].
**Plan:** [[pando/analysis/copilotkit_agui_integration_analysis.md]] (rev. 2, §3.8–§3.10, phase P5)

## What was changed

### 1. Durable thread -> session mapping (`internal/agui/threads.go`, new + migration)

Until P5 the AG-UI `threadId -> sessionId` map lived only in memory, so a restart silently
started a **new** Pando session for a thread the browser still considered open. The client
resends its own transcript, but everything on the agent side (summaries, tool results,
compaction state) was lost.

* New migration `internal/db/migrations/20260728000001_add_agui_threads.sql`:
  `agui_threads(thread_id PK, session_id, agent, created_at, updated_at)` + index on `session_id`.
  **No FK to `sessions`** and no change to the `sessions` table — dropping the feature is
  `DROP TABLE agui_threads` (invariant I5). No sqlc regeneration: the adapter owns the raw SQL.
* `threadStore` moved out of `runtime.go` into `threads.go`, gaining an optional `*sql.DB`
  (new `Deps.DB`, nil = P1 behaviour). Write-through: the in-memory map stays the hot path.
* **Fails soft, never hard.** A read-only secondary connection, or a database that predates the
  migration, trips `degrade()` (an `atomic.Bool`, read on every call): one warning, then
  memory-only for the rest of the process. A run never fails because persistence failed.
* Writes use `context.WithoutCancel` (`detach`): the browser hanging up mid-run must not cost
  the binding the *next* request needs.
* `Runtime.sessionForThread` now takes the agent name, **validates the session still exists**
  (`Sessions.Get`) and rebinds + `forget`s the row when it does not — otherwise every message of
  a thread whose session was deleted would fail forever. It also (re)installs the per-session
  permission handler on **every** resolution path; previously a mapping that outlived the
  process (and the pre-existing "client reuses a session id as thread id" branch) left the
  session with no handler, i.e. no HITL.

### 2. Hardened `/info` discovery (`internal/agui/server.go`)

* Absolute agent URLs derived from the request, so CopilotKit's `HttpAgent` can use them
  verbatim. `X-Forwarded-Proto` is honoured for the scheme only; **`X-Forwarded-Host` is
  ignored** — an attacker-controlled header must not make discovery hand out URLs pointing at
  somebody else's server.
* New `capabilities` block (`frontendTools`, `humanInTheLoop`, `sharedState`, `interrupts`) so a
  client can tell "not supported" from "nothing happened" without probing, plus `version` and
  `path`.
* Per-agent `model` (`id`, `name`, `provider`, `contextWindow`, honouring
  `ContextWindowOverride`) read from **config**, never by instantiating the agent: `/info` stays
  cheap enough to poll and does not warm the pool for an agent nobody ran. It names the provider
  but never locates it, and carries no credentials.
* `Cache-Control: no-store`. Auth was already enforced and is now pinned by a test.

### 3. Dedicated listener — deployment shape 2 of invariant I7 (`internal/agui/listener.go`, new)

`Runtime.StartListener(ListenerOptions{Host, CertFile, KeyFile})` serves `Runtime.Handler()`
(AG-UI routes only) on `Config.Port`. The bind is **synchronous** so a port clash reaches the
caller instead of dying in a goroutine; `Listener` exposes `Addr/URL/Wait/Shutdown`.
Default host is `localhost`, never `0.0.0.0`. No write timeout (SSE), 30 s read timeout.

`internal/api/server.go`: when `Port > 0` the adapter is started on its own listener and
**`aguiPath` stays empty**, so `routes.go` does not mount it and the CORS/token middleware
exclusions do not carve a hole out of a path nothing answers on. A listener that fails to bind
**disables the adapter** rather than falling back to the shared mux — the fallback would
silently widen exactly the surface the operator asked to isolate. New `Server.aguiListener`,
shut down before `agui.Close()`.

### 4. CLI

* `pando serve --agui-port N [--agui-host H]` — enables the adapter and moves it to its own port.
* `pando agui-serve` (new, `cmd/agui_serve.go`) — deployment shape 3: a process serving AG-UI and
  nothing else (no Web-UI, no REST API, no static assets, no IPC bus). Flags: `--port` (8090),
  `--host`, `--cwd`, `--allow-origin` (repeatable), `--agent` (repeatable), `--token`/`--no-token`,
  `--no-tls`/`--tls-cert`/`--tls-key`, `--auto-approve`, `--debug`. Generates and prints a bearer
  token unless told otherwise; warns when started with `--no-token` or with no allowed origins.

### 5. Config + docs

New `[AGUI] Host` (default `"localhost"`) alongside the existing `Port`; `agui.Config` gained
`Port`, `Deps` gained `DB`. README gained an **AG-UI (CopilotKit and other Generative-UI
frontends)** section (both deployment commands, the TOML block, the `/info` description).

## Files touched

New: `internal/agui/threads.go`, `internal/agui/listener.go`, `internal/agui/threads_test.go`,
`internal/agui/listener_test.go`, `cmd/agui_serve.go`,
`internal/db/migrations/20260728000001_add_agui_threads.sql`.
Modified: `internal/agui/{runtime,server,deps,doc}.go`, `internal/agui/server_test.go`,
`internal/api/{server,routes}.go`, `internal/api/agui_isolation_test.go`,
`internal/config/config.go` (`Host` field + default), `cmd/serve.go`, `README.md`.

Existing-file diff outside `internal/agui` stays additive and gated by `[AGUI] Enabled=false`.

## Verification

* `gofmt` clean (except the pre-existing `cmd/test_ollama_main/main.go`), `go build ./...`,
  `go vet ./internal/agui ./internal/api ./internal/config ./cmd` clean.
* `go test -race ./internal/agui ./internal/api ./internal/config` — ok.
* Migration applied and rolled back against a real SQLite database via goose (temporary test).
* **Live smoke test** of `pando agui-serve --port 8099 --no-tls --allow-origin http://localhost:3000`:
  `/info` 200 with the capabilities/model payload and the right CORS headers, no token 401,
  disallowed origin 403, `/api/v1/sessions` **404** (the dedicated listener really carries
  nothing else).
* New tests: thread store survives a "restart", rebinds, forgets, persists under a cancelled
  context, works with no database, degrades on a broken schema; listener serves only AG-UI,
  refuses tokenless requests, shuts down cleanly; `/info` absolute URL, capabilities, `no-store`,
  forwarded-host ignored, token required; api-level "dedicated listener owns no path".
* Not run: an end-to-end agent run over the dedicated listener (needs provider credentials) and
  the CopilotKit frontend (that is P6).

## Remaining

P6 (`@pando-ai/sdk/agui` subpath + Next.js example under `examples/copilotkit/`), P7 (optional
Mastra-style embedded route; AG-UI for mesnada sub-agents).
