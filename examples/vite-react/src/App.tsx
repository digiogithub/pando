import { useCallback, useMemo, useRef, useState } from "react";
import type { AguiEvent } from "@pando-ai/sdk/agui/client";
import { PandoAguiClient, PandoThread, isPermissionRequest, isQuestionRequest } from "@pando-ai/sdk/agui/client";
import { MessageView, PermissionCard, QuestionCard, StatePanel, ToolCallCard } from "./components";

// The browser never holds the Pando bearer token: it talks to the Go proxy
// in ./proxy, which injects `Authorization` itself. See README.md and the
// reverse-proxy contract in internal/agui/doc.go / sdk/typescript/README.md.
const PROXY_URL = import.meta.env.VITE_PROXY_URL ?? "http://localhost:8091";

function useForceUpdate(): () => void {
  const [, setTick] = useState(0);
  return useCallback(() => setTick((t) => t + 1), []);
}

export default function App(): JSX.Element {
  const forceUpdate = useForceUpdate();
  const [busy, setBusy] = useState(false);
  const [input, setInput] = useState("");
  const [error, setError] = useState<string>();

  // One PandoThread for the lifetime of the page. It reduces the AG-UI
  // event stream into `.messages`, `.state`, `.isInterrupted` and
  // `.pendingToolCalls` as a side effect of draining the generators
  // `.send`/`.resume` return -- see PandoThread's doc comment.
  const threadRef = useRef<PandoThread>();
  if (!threadRef.current) {
    const client = new PandoAguiClient({ baseUrl: PROXY_URL });
    threadRef.current = new PandoThread({ client });
  }
  const thread = threadRef.current;

  const drain = useCallback(
    async (gen: AsyncGenerator<AguiEvent, void, undefined>) => {
      setBusy(true);
      setError(undefined);
      try {
        for await (const _event of gen) {
          forceUpdate();
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setBusy(false);
        forceUpdate();
      }
    },
    [forceUpdate],
  );

  const send = useCallback(() => {
    const prompt = input.trim();
    if (!prompt || busy) return;
    setInput("");
    void drain(thread.send(prompt));
  }, [input, busy, thread, drain]);

  const resume = useCallback(
    (toolCallId: string, result: string) => {
      void drain(thread.resume(toolCallId, result));
    },
    [thread, drain],
  );

  const state = useMemo(() => thread.state, [thread.state, busy]);

  return (
    <div className="app">
      <header>
        <h1>Pando AG-UI — Vite + React example</h1>
        <p>
          Talking to <code>{PROXY_URL}</code>, a Go reverse proxy in front of{" "}
          <code>pando agui-serve</code> (see <code>proxy/</code>). No CopilotKit, no Next.js.
        </p>
      </header>

      {error && <div className="error">Error: {error}</div>}

      <main>
        <section className="transcript">
          {thread.messages.map((message) => (
            <MessageView key={message.id} message={message} reasoning={thread.reasoning} />
          ))}
        </section>

        {thread.isInterrupted && thread.pendingToolCalls.length > 0 && (
          <section className="interrupts">
            {thread.pendingToolCalls.map((call) =>
              isPermissionRequest(call) ? (
                <PermissionCard key={call.id} call={call} onAnswer={(result) => resume(call.id, result)} />
              ) : isQuestionRequest(call) ? (
                <QuestionCard key={call.id} call={call} onAnswer={(result) => resume(call.id, result)} />
              ) : (
                <ToolCallCard key={call.id} call={call} onAnswer={(result) => resume(call.id, result)} />
              ),
            )}
          </section>
        )}

        <StatePanel state={state} />
      </main>

      <footer>
        <input
          value={input}
          disabled={busy}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") send();
          }}
          placeholder="Ask the agent something..."
        />
        <button disabled={busy || !input.trim()} onClick={send}>
          {busy ? "Running…" : "Send"}
        </button>
      </footer>
    </div>
  );
}
