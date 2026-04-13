import { type Component, For, Show } from "solid-js";
import type { Message, ToolResult } from "@/types";

interface MessageBubbleProps {
  message: Message;
}

const MessageBubble: Component<MessageBubbleProps> = (props) => {
  const isUser = () => props.message.role === "user";

  return (
    <div
      class={`mb-4 flex ${isUser() ? "justify-end" : "justify-start"}`}
    >
      <div
        class={`max-w-[80%] rounded-lg px-4 py-2 ${
          isUser()
            ? "bg-primary text-primary-foreground"
            : "bg-card text-card-foreground"
        }`}
      >
        <div class="prose prose-sm max-w-none dark:prose-invert">
          <Show when={isUser()}>
            <p class="m-0">{props.message.content}</p>
          </Show>
          <Show when={!isUser()}>
            <Show when={props.message.thinking}>
              <div class="mb-2 rounded-md border border-dashed border-border/70 bg-muted/40 px-3 py-2 text-xs text-muted-foreground whitespace-pre-wrap">
                {props.message.thinking}
              </div>
            </Show>
            <div
              class="m-0 whitespace-pre-wrap"
              innerHTML={formatContent(props.message.content)}
            />
            <Show when={props.message.toolCalls?.length}>
              <div class="mt-3 space-y-2">
                <For each={props.message.toolCalls}>
                  {(toolCall) => (
                    <div class="rounded-md border border-border bg-muted/30 px-3 py-2 text-xs">
                      <div class="font-medium text-foreground">{toolCall.name}</div>
                      <pre class="mt-2 overflow-x-auto whitespace-pre-wrap text-muted-foreground">{formatStructured(toolCall.input)}</pre>
                    </div>
                  )}
                </For>
              </div>
            </Show>
            <Show when={props.message.toolResults?.length}>
              <div class="mt-3 space-y-2">
                <For each={props.message.toolResults}>
                  {(toolResult) => <ToolResultBlock result={toolResult} />}
                </For>
              </div>
            </Show>
            <Show when={props.message.isStreaming}>
              <span class="ml-1 inline-block h-4 w-1 animate-pulse bg-current" />
            </Show>
          </Show>
        </div>
        <div
          class={`mt-1 text-xs ${
            isUser() ? "text-primary-foreground/70" : "text-muted-foreground"
          }`}
        >
          {formatTime(props.message.timestamp)}
        </div>
      </div>
    </div>
  );
};

const ToolResultBlock: Component<{ result: ToolResult }> = (props) => {
  return (
    <div class={`rounded-md border px-3 py-2 text-xs ${
      props.result.is_error ? "border-destructive/40 bg-destructive/5" : "border-border bg-muted/20"
    }`}>
      <div class="font-medium text-foreground">{props.result.name}</div>
      <pre class="mt-2 overflow-x-auto whitespace-pre-wrap text-muted-foreground">{props.result.content}</pre>
      <Show when={props.result.metadata !== undefined}>
        <pre class="mt-2 overflow-x-auto whitespace-pre-wrap rounded bg-background/70 p-2 text-[11px] text-muted-foreground">{formatStructured(props.result.metadata)}</pre>
      </Show>
    </div>
  );
};

function formatStructured(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }

  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function formatContent(content: string): string {
  let formatted = content
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  formatted = formatted.replace(
    /```(\w*)\n([\s\S]*?)```/g,
    (_, lang, code) =>
      `<pre class="bg-muted rounded p-2 my-2 overflow-x-auto"><code class="language-${lang}">${code.trim()}</code></pre>`
  );

  formatted = formatted.replace(
    /`([^`]+)`/g,
    '<code class="bg-muted px-1 rounded">$1</code>'
  );

  formatted = formatted.replace(
    /\*\*([^*]+)\*\*/g,
    '<strong>$1</strong>'
  );

  formatted = formatted.replace(
    /\*([^*]+)\*/g,
    '<em>$1</em>'
  );

  formatted = formatted.replace(
    /^- (.+)$/gm,
    '<li class="ml-4">$1</li>'
  );

  formatted = formatted.replace(/\n/g, "<br>");

  return formatted;
}

function formatTime(date: Date): string {
  return new Intl.DateTimeFormat("en-US", {
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  }).format(date);
}

export default MessageBubble;
