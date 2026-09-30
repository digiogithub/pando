# AG-UI (CopilotKit and other Generative-UI frontends)

Pando speaks the [AG-UI protocol](https://docs.ag-ui.com), the wire contract CopilotKit and
similar React frontends use to drive an agent backend. It is served by an **isolated side-car
adapter**: its own agent instances, its own permission service and its own auth/CORS policy,
so enabling it changes nothing for the TUI, Web UI or ACP editors.

It is **off by default** — it exposes a code-executing agent to a browser origin.

```bash
# Dedicated process: AG-UI and nothing else (recommended for anything a browser reaches)
pando agui-serve --port 8090 --allow-origin http://localhost:3000

# Or alongside the Web UI, on its own port
pando serve --agui-port 8090
```

`agui-serve` prints the bearer token on startup; point CopilotKit's `HttpAgent` at
`https://localhost:8090/api/v1/agui/coder` with an `Authorization: Bearer …` header.
`GET /api/v1/agui/info` reports the exposed agents, their model and which protocol
capabilities (frontend tools, human-in-the-loop, shared state, interrupts) are available.

```toml
[AGUI]
Enabled        = true
Port           = 8090             # 0 mounts it on the main API server instead
Host           = 'localhost'      # binding this elsewhere exposes the agent to the network
Agents         = ['coder']
AllowedOrigins = ['http://localhost:3000']   # empty = no browser may connect
RequireToken   = true
FrontendTools  = true             # proxy the client's useCopilotAction tools to the agent
HumanInTheLoop = true             # approvals and questions are asked in the browser
AutoApprove    = false            # adapter-local only; never touches the desktop surfaces
```

Thread ids sent by the client are bound to Pando sessions in the adapter's own
`agui_threads` table, so a reload — or a restart — continues the same conversation.

The run publishes a shared-state document (`STATE_SNAPSHOT` + RFC-6902 `STATE_DELTA`)
carrying the model, live token budget, todos, files touched and the **mesnada sub-agents**
the thread delegated — so a page renders a fan-out as cards with `useCoAgent`, instead of
parsing chat text.

**TypeScript client.** `@pando-ai/sdk` exposes it as a separate subpath, so nothing is
pulled in unless you import it; `@ag-ui/client` and `@copilotkit/runtime` are optional
peers of that subpath only.

```typescript
// Streaming, no CopilotKit involved
import { PandoAguiClient } from '@pando-ai/sdk/agui';

const client = new PandoAguiClient({ baseUrl: 'http://localhost:8090', token });
for await (const event of client.run({ prompt: 'Summarise the repo' })) {
  if (event.type === 'TEXT_MESSAGE_CONTENT') process.stdout.write(event.delta);
}
```

```typescript
// app/api/copilotkit/route.ts — reads /info and registers every agent Pando advertises
import { registerPandoCopilotKit } from '@pando-ai/sdk/agui';

export const { POST, GET, OPTIONS } = await registerPandoCopilotKit({
  baseUrl: process.env.PANDO_URL!,
  token: process.env.PANDO_TOKEN,
});
```

A runnable Next.js app — chat, state dashboard, a frontend tool and in-page approvals —
lives in [`examples/copilotkit/`](../examples/copilotkit/).
