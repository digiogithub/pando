/**
 * CopilotKit runtime endpoint backed by Pando.
 *
 * The browser talks to this route; this route talks AG-UI to Pando. Pando's
 * token never reaches the client, which is the reason the hop exists.
 *
 * `registerPandoCopilotKit` reads `GET /api/v1/agui/info` at startup and
 * registers every agent Pando advertises, so adding an agent to `[AGUI] Agents`
 * in `.pando.toml` is enough — no change here.
 */

import { registerPandoCopilotKit } from "@pando-ai/sdk/agui";
import { CopilotRuntime, ExperimentalEmptyAdapter, copilotRuntimeNextJSAppRouterEndpoint } from "@copilotkit/runtime";
import { HttpAgent } from "@ag-ui/client";

const route = await registerPandoCopilotKit({
  baseUrl: process.env.PANDO_URL ?? "http://localhost:8090",
  token: process.env.PANDO_TOKEN,
  endpoint: "/api/copilotkit",
  // Passed explicitly so Next.js can bundle them statically. Omit both and the
  // SDK imports them at runtime instead.
  HttpAgent,
  runtimeModule: { CopilotRuntime, ExperimentalEmptyAdapter, copilotRuntimeNextJSAppRouterEndpoint },
});

export const { POST, GET, OPTIONS } = route;

// The agent streams for as long as the task takes; the default serverless
// budget would cut it off mid-run.
export const maxDuration = 300;
export const dynamic = "force-dynamic";
