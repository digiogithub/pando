import { useState } from "react";
import type {
  AguiMessage,
  PandoQuestionAnswerEntry,
  PandoState,
  PendingToolCall,
} from "@pando-ai/sdk/agui/client";
import type {
  PandoPermissionPendingCall,
  PandoQuestionPendingCall,
} from "@pando-ai/sdk/agui/client";
import { answerQuestion } from "@pando-ai/sdk/agui/client";

/** One transcript entry: streamed text, reasoning and any tool calls it opened. */
export function MessageView({
  message,
  reasoning,
}: {
  message: AguiMessage;
  reasoning: Map<string, string>;
}): JSX.Element {
  const text = typeof message.content === "string" ? message.content : "";
  const think = reasoning.get(message.id);
  return (
    <div className={`message message-${message.role}`}>
      <div className="role">{message.role}</div>
      {think && <pre className="reasoning">{think}</pre>}
      {text && <div className="text">{text}</div>}
      {message.toolCalls?.map((call) => (
        <div key={call.id} className="tool-call">
          <strong>{call.function.name}</strong>
          {call.function.arguments && <pre>{call.function.arguments}</pre>}
        </div>
      ))}
    </div>
  );
}

/** Pando's synthetic `pando_permission_request` tool call, answered with the HITL helpers. */
export function PermissionCard({
  call,
  onAnswer,
}: {
  call: PandoPermissionPendingCall;
  onAnswer: (result: string) => void;
}): JSX.Element {
  const req = call.args;
  return (
    <div className="card permission-card">
      <h3>Permission requested</h3>
      <p>
        <strong>{req.toolName}</strong> wants to <strong>{req.action}</strong>
        {req.path ? ` on ${req.path}` : ""}
      </p>
      {req.description && <p className="description">{req.description}</p>}
      <div className="actions">
        <button onClick={() => onAnswer(JSON.stringify({ approved: true }))}>Approve</button>
        <button onClick={() => onAnswer(JSON.stringify({ approved: false }))}>Deny</button>
      </div>
    </div>
  );
}

/** A real `AskUserQuestion` call, substituted by the adapter to wait on the client. */
export function QuestionCard({
  call,
  onAnswer,
}: {
  call: PandoQuestionPendingCall;
  onAnswer: (result: string) => void;
}): JSX.Element {
  const [selections, setSelections] = useState<Record<number, Set<string>>>({});

  const toggle = (qIndex: number, label: string, multi: boolean) => {
    setSelections((prev) => {
      const current = new Set(prev[qIndex] ?? []);
      if (multi) {
        if (current.has(label)) current.delete(label);
        else current.add(label);
      } else {
        current.clear();
        current.add(label);
      }
      return { ...prev, [qIndex]: current };
    });
  };

  const submit = () => {
    const answers: PandoQuestionAnswerEntry[] = call.args.questions.map((q, i) => ({
      questionId: String(i),
      header: q.header,
      selected: Array.from(selections[i] ?? []),
    }));
    onAnswer(answerQuestion({ answers }));
  };

  return (
    <div className="card question-card">
      <h3>Question</h3>
      {call.args.questions.map((q, i) => (
        <fieldset key={i}>
          <legend>{q.question}</legend>
          {q.options.map((opt) => (
            <label key={opt.label}>
              <input
                type={q.multiSelect ? "checkbox" : "radio"}
                name={`q-${i}`}
                checked={selections[i]?.has(opt.label) ?? false}
                onChange={() => toggle(i, opt.label, Boolean(q.multiSelect))}
              />
              {opt.label} — <span className="option-description">{opt.description}</span>
            </label>
          ))}
        </fieldset>
      ))}
      <div className="actions">
        <button onClick={submit}>Answer</button>
        <button onClick={() => onAnswer(answerQuestion({ cancelled: true, answers: [] }))}>
          Cancel
        </button>
      </div>
    </div>
  );
}

/**
 * Fallback for a frontend-tool call that is neither the permission nor the
 * question prompt (only reachable if a caller declares its own
 * `RunAgentInput.tools`, which this example does not). Lets the operator
 * type an arbitrary tool-result string so the run is never stuck.
 */
export function ToolCallCard({
  call,
  onAnswer,
}: {
  call: PendingToolCall;
  onAnswer: (result: string) => void;
}): JSX.Element {
  const [value, setValue] = useState("");
  return (
    <div className="card">
      <h3>Frontend tool: {call.name}</h3>
      <pre>{call.argsText}</pre>
      <input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="Tool result to send back"
      />
      <div className="actions">
        <button onClick={() => onAnswer(value)}>Send result</button>
      </div>
    </div>
  );
}

/** Renders the AG-UI shared-state document: model, token budget, todos and touched files. */
export function StatePanel({ state }: { state: PandoState | undefined }): JSX.Element | null {
  if (!state) return null;
  const usage = state.tokenUsage;
  const pct =
    usage && usage.contextWindow > 0
      ? Math.min(100, Math.round(((usage.promptTokens + usage.completionTokens) / usage.contextWindow) * 100))
      : undefined;

  return (
    <section className="state-panel">
      <h2>
        {state.agent} · {state.model?.name ?? state.model?.id ?? "unknown model"}
      </h2>

      {usage && (
        <div>
          <div>
            {usage.promptTokens + usage.completionTokens} / {usage.contextWindow} tokens
            {usage.estimated ? " (estimated)" : ""}
          </div>
          {pct !== undefined && (
            <div className="token-bar">
              <div className="token-bar-fill" style={{ width: `${pct}%` }} />
            </div>
          )}
        </div>
      )}

      {state.todos.length > 0 && (
        <div>
          <strong>Todos</strong>
          {state.todos.map((todo, i) => (
            <div key={i} className={`todo todo-${todo.status}`}>
              <span className="todo-status">{todo.status}</span>
              <span className="todo-content">{todo.content}</span>
            </div>
          ))}
        </div>
      )}

      {state.files.length > 0 && (
        <div>
          <strong>Files touched</strong>
          <div className="files-list">
            {state.files.map((f, i) => (
              <div key={i}>
                <code>{f.action}</code> {f.path}
              </div>
            ))}
          </div>
        </div>
      )}

      {state.subAgents.length > 0 && (
        <div>
          <strong>Sub-agents</strong>
          <div className="subagents-list">
            {state.subAgents.map((sa) => (
              <div key={sa.id}>
                {sa.role ?? "worker"} — {sa.status}
                {sa.conclusion ? ` (${sa.conclusion})` : ""}
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
