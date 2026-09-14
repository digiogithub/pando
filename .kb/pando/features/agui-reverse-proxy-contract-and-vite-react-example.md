---
created_at: 2026-09-14T18:12:35.661928599Z
updated_at: 2026-09-14T18:12:35.661928599Z
---

# PANDO-US-0024: reverse-proxy contract + Vite/React 18 AG-UI example

Last open story of milestone PANDO-M-0001 / epic PANDO-EP-0004. Documents the AG-UI
surface built out earlier the same session (thread API `threads.go`, run durability
`run.go`, operability `PANDO-US-0020..0023` in `doc.go`) and ships the first
non-CopilotKit worked client.

## What was verified against source (not trusted from the story spec)

The story text cited approximate file:line facts from an earlier draft; the actual code
had moved (new routes/healthz added same session). Re-verified every citation against
current `internal/agui/*.go` and `cmd/agui_serve.go` before writing anything down:

- `authorize` is now `server.go:71-95` (not 47-53 as the story draft said);
  `originAllowed` is `109-116` (not 85-92); `bearerToken` is `97-103` (not 73-79).
- `NewSSEWriter`/`X-Accel-Buffering`: `sse.go:33-46`, header set at `sse.go:43`.
- `defaultHeartbeat` (15s): `deps.go:162`; ticker + `Comment("keep-alive")`:
  `server.go:790-791,801-803`.
- `requestBaseURL` (the `/info` URL builder): `server.go:346-358`, not 226-238.
- `defaultMaxRequestBytes` (8 MiB): `sse.go:13`; `DecodeRunAgentInput`'s
  `io.LimitReader`: `input.go:169-182` (this one matched the draft).
- `WriteTimeout: 0` / `ReadTimeout: 30s`: `listener.go:89,91`.
- TLS self-sign / `--no-tls`: `cmd/agui_serve.go:144-157`.
- Token precedence `--token > --token-file > PANDO_AGUI_TOKEN > generated`:
  `cmd/agui_serve.go`'s `resolveAGUIToken`, `262-304`.

## Files changed

- `internal/agui/doc.go` — added a "# Reverse-proxy contract (PANDO-US-0024)" section
  (doc-comment only, no code change) covering Origin semantics, token/`?token=` handling,
  streaming (`FlushInterval: -1`, no buffering, heartbeat, timeouts), `/info` URL
  rewriting, TLS, and the 8 MiB body limit — each pinned to the verified file:line above.
  Points at `examples/vite-react/proxy/main.go` as the canonical implementation.
- `examples/vite-react/proxy/{go.mod,main.go,example_test.go}` — a standalone Go module
  (own `go.mod`, like `examples/acp-client/go/`, so it never enters the root module's
  `go build ./...`) implementing `newReverseProxy` (strips `Origin`, strips inbound
  `?token=`, sets the bearer header, `FlushInterval: -1`) plus a `main()` HTTP server with
  its own browser-facing CORS policy. `example_test.go` has `Example_reverseProxy`,
  compiled/run by `go test` so the snippet mirrored into the SDK README cannot silently
  stop building.
- `examples/vite-react/` (new) — Vite + React 18 SPA: `PandoAguiClient` + `PandoThread`
  from `@pando-ai/sdk/agui/client` against the proxy's URL (browser never holds the Pando
  token). `src/components.tsx` renders streamed text, reasoning, tool-call cards, a
  `PermissionCard` (`isPermissionRequest`/canonical `{"approved":...}`), a `QuestionCard`
  (`isQuestionRequest`/`answerQuestion`, single+multi-select), and a `StatePanel`
  (model, token budget bar, todos, files touched, mesnada sub-agents from `PandoState`).
  `package.json` links the SDK via `file:../../sdk/typescript`, same pattern as
  `examples/copilotkit/`.
- `sdk/typescript/README.md` (separate repo, `sdk/` gitignored in this monorepo, branch
  `feat/pando-us-0006-browser-safe`) — new "Reverse-proxy contract" subsection under
  "Mode 4: AG-UI", mirroring doc.go's contract plus the `newReverseProxy` Go snippet
  (copied from `examples/vite-react/proxy/main.go`), and a pointer to the new example.
- `sdk/typescript/src/agui/thread.ts` — **fixed a stale doc comment**: it asserted "there
  is no thread-list, history-fetch or reattach endpoint on the server
  (`internal/agui/server.go:26-33` mounts only `/info`, `OPTIONS` and the run POST)". That
  was true before this session's thread-API work (`threads.go`, PANDO-US-0015/0018) and is
  false now — `Register` also mounts `GET {path}/threads`,
  `GET {path}/threads/{id}/messages`, `GET {path}/threads/{id}/stream` and
  `DELETE {path}/threads/{id}`. Reworded to state accurately that `PandoThread` itself
  just never calls those endpoints (it only reduces `PandoAguiClient.run`'s event stream),
  which is a different and narrower claim than "they don't exist".

## Verification performed

- `go build ./internal/agui/...`, `go vet ./internal/agui/...`,
  `go test ./internal/agui/...` — clean (doc.go is comment-only, no behavior change).
- `go test ./internal/llm/agent ./internal/api` (the project's designated command) — ok.
- `examples/vite-react/proxy`: own `go.mod`; `go build ./...`, `go vet ./...`,
  `go test ./...` all pass (`Example_reverseProxy` PASS); confirmed via `go list ./...`
  at repo root that the nested module is NOT swept into the root build.
- `gofmt -l` clean on all new/touched Go files.
- SDK: `npm run build` (tsup) succeeded; `npx tsc --noEmit -p tsconfig.agui-browser.json`
  clean after the thread.ts doc fix.
- Example app: `npm install` + `npm run build` (`vite build`) succeeded, zero warnings;
  `npx tsc --noEmit -p tsconfig.json` clean; confirmed the built bundle contains no
  `@copilotkit`/`@ag-ui/client` references and no real `node:` builtin imports (one
  harmless literal "CopilotKit" substring from the page's own UI copy, one false-positive
  `node:n` object-shorthand match in minified code — neither is a dependency).
- **Full end-to-end round trip**, not just a build: built `pando` from source, ran
  `pando agui-serve --port 8090 --no-tls` with a scratch project dir and NO
  `--allow-origin` (empty `AllowedOrigins`, the recommended shape), started the Go proxy
  from `examples/vite-react/proxy` with the printed token, and confirmed through the proxy:
  `GET /api/v1/agui/healthz` (unauthenticated, direct to Pando), `GET /api/v1/agui/info`
  (200, correct agent list/model, URLs rewritten to the proxy's own address), and
  `GET /api/v1/agui/threads` (200, empty list) — i.e. the proxy's Origin-stripping +
  token-injection contract actually works against a live adapter with no browser Origin
  allow-list configured. All background processes and the compiled `pando`/`proxy`
  binaries were cleaned up afterward (built into the session scratchpad, not committed).

## Acceptance criteria status

All five satisfied: doc.go proxy-contract section (file:line facts); SDK README mirror +
compiling Go example test; `examples/vite-react/` builds and was verified running
end-to-end against `pando agui-serve --no-tls` through its proxy, with exact commands in
its README; the example renders streaming text, reasoning, tool calls, a permission
prompt via the SDK's HITL helpers, and the state document; nothing in the example imports
CopilotKit, Next.js or `@ag-ui/client` (verified in the built bundle and `package.json`).

No commit was made — per instructions, the coordinator commits both repos (this monorepo
and the separate `sdk/typescript` repo) separately.