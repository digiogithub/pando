# Pando × CopilotKit (AG-UI)

A Next.js app that drives a Pando agent from the browser: streaming chat,
a live dashboard built from Pando's shared state, a frontend tool the agent can
call, and approval prompts answered in the page.

No Pando-specific frontend code is involved: the wire protocol is
[AG-UI](https://docs.ag-ui.com), which CopilotKit speaks natively.

```
browser ──HTTP──▶ Next.js route ──AG-UI/SSE──▶ pando agui-serve
        (CopilotKit)   (holds the token)        (runs the agent)
```

## 1. Start Pando's AG-UI adapter

The adapter is off by default. Serve it as its own process:

```bash
pando agui-serve --port 8090 --allow-origin http://localhost:3000
```

It prints the bearer token to use. Two things are enforced and worth knowing
before you change them:

- **The origin allow-list is not optional.** An empty list means no browser may
  connect at all — this endpoint executes code, so it never answers
  `Access-Control-Allow-Origin: *`.
- **The token is required** unless you pass `--no-token`. Don't, outside a
  throwaway local test.

For TLS, drop `--no-tls` (the default) and the listener serves HTTPS with
Pando's certificate; add `--tls-cert` / `--tls-key` for your own.

Alternatively, mount it on an existing server: `pando serve --agui-port 8090`.

## 2. Run the example

```bash
cp .env.example .env
# paste the token agui-serve printed into PANDO_TOKEN
npm install
npm run dev
```

Open http://localhost:3000.

The example links the SDK from source (`file:../../sdk/typescript`), so build it
first if you changed it: `cd ../../sdk/typescript && npm install && npm run build`.

Point `PANDO_URL` at whatever port you passed to `agui-serve`, and serve a
project directory the agent may write to — `agui-serve --cwd /path/to/project`.

## What each piece demonstrates

| File | Shows |
|---|---|
| `app/api/copilotkit/route.ts` | `registerPandoCopilotKit` — reads `/info` and registers every agent Pando advertises |
| `app/page.tsx` → `Dashboard` | `useCoAgent<PandoState>` — model, token budget, todos, files and sub-agents from `STATE_SNAPSHOT`/`STATE_DELTA` |
| `app/page.tsx` → `useFrontendTools` | a browser-side tool; the agent calling it suspends the run until the page answers |
| `app/page.tsx` → `usePermissionPrompt` | human in the loop: Pando's permission prompt arrives as a `pando_permission_request` tool call |

## Why the Next.js hop exists

CopilotKit's client speaks GraphQL to its own runtime, which then speaks AG-UI
to the agent. Pando implements AG-UI — the protocol every other backend
implements — and deliberately not CopilotKit's runtime protocol, so this route
is where the translation happens. It is also where the token lives: moving it to
the browser would hand any visitor an agent that can run commands on your
machine.

If you don't want CopilotKit at all, skip both and use the client directly:

```typescript
import { PandoAguiClient } from "@pando-ai/sdk/agui";

const client = new PandoAguiClient({ baseUrl: "http://localhost:8090", token });
for await (const event of client.run({ prompt: "Summarise the repo" })) {
  if (event.type === "TEXT_MESSAGE_CONTENT") process.stdout.write(event.delta);
}
```

## Troubleshooting

| Symptom | Cause |
|---|---|
| `401` from the route | `PANDO_TOKEN` does not match what `agui-serve` printed |
| `403` on every request | the origin is missing from `--allow-origin` |
| `advertises no AG-UI agents` | `[AGUI] Agents` is empty in `.pando.toml` |
| The run stops mid-answer | the agent called a frontend tool and is waiting: check the tool's handler returned |
| `agui-serve` starts but never listens | another Pando instance holds the project's `ipc.lock`; serve a different `--cwd` or stop it |
