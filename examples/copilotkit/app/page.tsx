"use client";

/**
 * A Pando dashboard driven entirely by the AG-UI shared state.
 *
 * Nothing here parses chat text: the model, token budget, todo list, touched
 * files and delegated sub-agents all come from the state document Pando's
 * adapter publishes (STATE_SNAPSHOT + STATE_DELTA).
 */

import { CopilotKit, useCoAgent, useCopilotAction } from "@copilotkit/react-core";
import { CopilotSidebar } from "@copilotkit/react-ui";
import type { PandoState, PandoPermissionRequest } from "@pando-ai/sdk/agui";
import "@copilotkit/react-ui/styles.css";

const AGENT = "coder";

export default function Home() {
  return (
    <CopilotKit runtimeUrl="/api/copilotkit" agent={AGENT}>
      <main style={{ padding: 24, fontFamily: "system-ui, sans-serif" }}>
        <h1>Pando × CopilotKit</h1>
        <Dashboard />
      </main>
      <CopilotSidebar
        defaultOpen
        labels={{ title: "Pando", initial: "What should I work on?" }}
      />
    </CopilotKit>
  );
}

function Dashboard() {
  const { state } = useCoAgent<PandoState>({ name: AGENT });

  useFrontendTools();
  usePermissionPrompt();

  if (!state?.session) {
    return <p>Send a message to start a run.</p>;
  }

  const usage = state.tokenUsage;
  const used = usage ? usage.promptTokens + usage.completionTokens : 0;

  return (
    <section style={{ display: "grid", gap: 24, maxWidth: 720 }}>
      <div>
        <h2>Model</h2>
        <p>
          {state.model?.name ?? state.model?.id}
          {usage ? ` — ${used} / ${usage.contextWindow} tokens` : null}
        </p>
      </div>

      <div>
        <h2>Todos</h2>
        <ul>
          {state.todos?.map((todo, i) => (
            <li key={todo.id ?? i}>
              [{String(todo.status ?? "?")}] {String(todo.content ?? "")}
            </li>
          ))}
        </ul>
      </div>

      <div>
        <h2>Files touched</h2>
        <ul>
          {state.files?.map((file) => (
            <li key={file.path}>
              {file.action}: {file.name}
            </li>
          ))}
        </ul>
      </div>

      <div>
        <h2>Sub-agents</h2>
        {state.subAgents?.length ? (
          <ul>
            {state.subAgents.map((task) => (
              <li key={task.id}>
                <strong>{task.role ?? "task"}</strong> {task.id} — {task.status}
                {task.conclusion ? ` (${task.conclusion})` : null}
                {task.prompt ? <div style={{ opacity: 0.7 }}>{task.prompt}</div> : null}
              </li>
            ))}
          </ul>
        ) : (
          <p style={{ opacity: 0.7 }}>No delegated tasks yet.</p>
        )}
      </div>
    </section>
  );
}

/**
 * A frontend tool. The agent calling it suspends the run; CopilotKit sends the
 * handler's return value back on the next request, which resumes it.
 */
function useFrontendTools() {
  useCopilotAction({
    name: "highlight_in_page",
    description: "Scroll the page to a heading and highlight it for the user.",
    parameters: [{ name: "heading", type: "string", description: "Heading text" }],
    handler: ({ heading }: { heading: string }) => {
      const target = Array.from(document.querySelectorAll("h2")).find((node) =>
        node.textContent?.toLowerCase().includes(heading.toLowerCase()),
      );
      if (!target) return `No heading matching ${heading}`;
      target.scrollIntoView({ behavior: "smooth" });
      target.style.outline = "2px solid orange";
      return `Highlighted ${target.textContent}`;
    },
  });
}

/**
 * Human in the loop: Pando surfaces a permission prompt as a synthetic
 * `pando_permission_request` tool call and waits for the answer. No answer, or
 * anything that is not an explicit approval, denies the action.
 */
function usePermissionPrompt() {
  useCopilotAction({
    name: "pando_permission_request",
    description: "Approve or deny a tool Pando wants to run.",
    available: "remote",
    renderAndWaitForResponse: ({ args, respond, status }) => {
      const request = args as Partial<PandoPermissionRequest>;
      if (status === "complete") return <div>Answered.</div>;
      return (
        <div style={{ border: "1px solid #ccc", padding: 12, borderRadius: 8 }}>
          <p>
            <strong>{request.toolName}</strong> wants to {request.action}
            {request.path ? ` ${request.path}` : ""}.
          </p>
          {request.description ? <pre>{request.description}</pre> : null}
          <button onClick={() => respond?.({ approved: true })}>Approve</button>
          <button onClick={() => respond?.({ approved: false })}>Deny</button>
        </div>
      );
    },
  });
}
