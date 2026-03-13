import { type Component, Show } from "solid-js";
import type { Message } from "@/types";

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
            <div
              class="m-0 whitespace-pre-wrap"
              innerHTML={formatContent(props.message.content)}
            />
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
