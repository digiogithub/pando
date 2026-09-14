# Pando × Vite + React (AG-UI, no CopilotKit)

A minimal single-page app that drives a Pando agent from the browser: streaming text,
reasoning output, tool call cards, a permission prompt and the shared-state document
(todos, token budget, touched files) — with **no CopilotKit, no Next.js, and no Node
server beyond a small Go reverse proxy**.

```
browser ──HTTP/SSE──▶ Go proxy (proxy/) ──AG-UI/SSE──▶ pando agui-serve
        (never sees the Pando token)   (holds the token,
                                         strips Origin/?token=)
```

This is the pairing [`examples/copilotkit/`](../copilotkit/) does not cover: a plain SPA
consumer of `@pando-ai/sdk/agui/client`, fronted by the reverse proxy documented in
[`internal/agui/doc.go`](../../internal/agui/doc.go) ("Reverse-proxy contract") and
[`sdk/typescript/README.md`](../../sdk/typescript/README.md#reverse-proxy-contract). See
[`examples/copilotkit/`](../copilotkit/) for the CopilotKit reference instead — it stays
as-is, this example does not replace it.

## 1. Build the SDK

```bash
cd sdk/typescript
npm install
npm run build
```

This example links `@pando-ai/sdk` from source (`file:../../sdk/typescript`), exactly like
`examples/copilotkit/` does — rebuild it whenever you change the SDK.

## 2. Start Pando's AG-UI adapter

```bash
pando agui-serve --port 8090 --no-tls --cwd /path/to/project
```

It prints the bearer token to use. Two things worth knowing about this command line:

- **No `--allow-origin` is passed.** The browser never talks to Pando directly in this
  example — only the proxy does, and the proxy strips the `Origin` header before
  forwarding, so `authorize()` never checks the allow-list at all. `AllowedOrigins` stays
  empty on purpose; the "AG-UI server has no allowed origins" warning `agui-serve` prints
  is expected and safe to ignore here. (Point a browser at Pando *directly* instead — no
  proxy — and that warning means what it says: see `examples/copilotkit/`.)
- **`--no-tls`** is the pragmatic choice for a loopback hop behind a proxy. Across any
  other network boundary, drop `--no-tls` and pin the proxy to Pando's self-signed (or
  your own) certificate instead.

## 3. Run the reverse proxy

```bash
cd examples/vite-react/proxy
PANDO_AGUI_TOKEN='<token agui-serve printed>' go run .
```

It listens on `:8091` (override with `PROXY_ADDR`) and forwards to `http://localhost:8090`
(override with `PANDO_AGUI_URL`). This is the *only* backend hop the React app talks to;
it never sees `pando agui-serve` directly, and the token above never reaches the browser.
See [`proxy/main.go`](proxy/main.go) for what it does and why — it is the same
`newReverseProxy` snippet reproduced in `sdk/typescript/README.md`, compiled by
[`proxy/example_test.go`](proxy/example_test.go) so it cannot silently rot.

```bash
cd examples/vite-react/proxy
go build ./...   # or: go vet ./... && go test ./...
```

## 4. Run the example

```bash
cd examples/vite-react
npm install
npm run dev
```

Open http://localhost:5173. To point the app at a differently-addressed proxy, copy
`.env.example` to `.env` and set `VITE_PROXY_URL`.

For a production-style build:

```bash
npm run build     # vite build -> dist/
npm run preview   # serves dist/ locally
```

## What each piece demonstrates

| File | Shows |
|---|---|
| `proxy/main.go` | The reverse-proxy contract: strip `Origin`, strip inbound `?token=`, set the bearer header, `FlushInterval: -1`, `WriteTimeout: 0` |
| `src/App.tsx` | `PandoAguiClient` + `PandoThread` against the proxy's URL — the browser never has the Pando token |
| `src/components.tsx` → `MessageView` | Streamed text (`TEXT_MESSAGE_CONTENT`) and reasoning (`REASONING_MESSAGE_CONTENT`), plus tool-call cards opened by `TOOL_CALL_START`/`ARGS` |
| `src/components.tsx` → `PermissionCard` | `isPermissionRequest` narrowing a pending call, answered with the canonical `{"approved": true/false}` shape |
| `src/components.tsx` → `QuestionCard` | `isQuestionRequest` narrowing a pending `AskUserQuestion` call, answered with `answerQuestion`/single- and multi-select options |
| `src/components.tsx` → `StatePanel` | `PandoState` rendering: model, live token budget, todos, files touched, mesnada sub-agents — from `STATE_SNAPSHOT`/`STATE_DELTA` |

## Why a proxy, and why does it matter

Pointing this SPA at `pando agui-serve` directly would work too (see
`examples/copilotkit/`'s `.env.example` for that shape), but it means the browser has to
hold the Pando bearer token and Pando has to trust the browser's own `Origin`. Routing
through a backend you already run — this proxy stands in for it — means:

- the Pando token lives in one process's environment, never in browser JS or `localStorage`;
- Pando's own `AllowedOrigins`/token checks collapse to "trust this one proxy", and your
  proxy enforces whatever auth the rest of your product already uses toward the browser;
- you can put the proxy behind the same TLS termination, logging and rate limiting as the
  rest of your backend, instead of exposing `agui-serve` itself to the internet.

Nothing in this example depends on CopilotKit, Next.js, or `@ag-ui/client`.
