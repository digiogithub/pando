import { type Component, For, Show, createEffect, createSignal } from "solid-js";
import { useServer } from "@/hooks/useServer";
import type { Message, SSEEvent, ToolCall, ToolResult } from "@/types";
import MessageBubble from "./MessageBubble";
import ChatInput from "./ChatInput";

const ChatView: Component = () => {
  const { token } = useServer();
  const [messages, setMessages] = createSignal<Message[]>([]);
  const [isLoading, setIsLoading] = createSignal(false);
  let messagesEndRef: HTMLDivElement | undefined;

  const scrollToBottom = () => {
    messagesEndRef?.scrollIntoView({ behavior: "smooth" });
  };

  createEffect(() => {
    messages();
    scrollToBottom();
  });

  const updateAssistantMessage = (messageId: string, updater: (message: Message) => Message) => {
    setMessages((prev) => prev.map((message) => (message.id === messageId ? updater(message) : message)));
  };

  const upsertToolCall = (toolCalls: ToolCall[] | undefined, toolCall: ToolCall): ToolCall[] => {
    const current = toolCalls ?? [];
    const index = current.findIndex((item) => item.id === toolCall.id);
    if (index === -1) {
      return [...current, toolCall];
    }

    return current.map((item, idx) => (idx === index ? { ...item, ...toolCall } : item));
  };

  const upsertToolResult = (toolResults: ToolResult[] | undefined, toolResult: ToolResult): ToolResult[] => {
    const current = toolResults ?? [];
    const index = current.findIndex((item) => item.tool_call_id === toolResult.tool_call_id);
    if (index === -1) {
      return [...current, toolResult];
    }

    return current.map((item, idx) => (idx === index ? { ...item, ...toolResult } : item));
  };

  const parseSSEEvents = (chunk: string): SSEEvent[] => {
    const events: SSEEvent[] = [];

    for (const rawEvent of chunk
      .split("\n\n")
      .map((value) => value.trim())
      .filter(Boolean)) {
      const lines = rawEvent.split("\n");
      const eventLine = lines.find((line) => line.startsWith("event: "));
      const dataLines = lines.filter((line) => line.startsWith("data: "));
      if (!eventLine || dataLines.length === 0) {
        continue;
      }

      const type = eventLine.slice(7).trim() as NonNullable<SSEEvent["type"]>;
      const rawData = dataLines.map((line) => line.slice(6)).join("\n");

      try {
        events.push({
          type,
          ...(JSON.parse(rawData) as Omit<SSEEvent, "type">),
        });
      } catch {
        continue;
      }
    }

    return events;
  };

  const sendMessage = async (content: string) => {
    if (!content.trim() || isLoading()) return;

    const userMessage: Message = {
      id: crypto.randomUUID(),
      role: "user",
      content: content.trim(),
      timestamp: new Date(),
    };

    const assistantMessage: Message = {
      id: crypto.randomUUID(),
      role: "assistant",
      content: "",
      timestamp: new Date(),
      isStreaming: true,
      thinking: "",
      toolCalls: [],
      toolResults: [],
    };

    setMessages((prev) => [...prev, userMessage, assistantMessage]);
    setIsLoading(true);

    try {
      const t = token();
      const response = await fetch(`${(window as typeof window & { __PANDO_API_BASE__?: string }).__PANDO_API_BASE__ || ""}/api/v1/chat/stream`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(t ? { "X-Pando-Token": t } : {}),
        },
        body: JSON.stringify({ prompt: content.trim() }),
      });

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const reader = response.body?.getReader();
      const decoder = new TextDecoder();

      if (!reader) {
        throw new Error("No reader available");
      }

      let buffer = "";
      let accumulatedContent = "";
      let accumulatedThinking = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }

        buffer += decoder.decode(value, { stream: true });
        const lastBoundary = buffer.lastIndexOf("\n\n");
        if (lastBoundary === -1) {
          continue;
        }

        const completeChunk = buffer.slice(0, lastBoundary);
        buffer = buffer.slice(lastBoundary + 2);

        for (const event of parseSSEEvents(completeChunk)) {
          switch (event.type) {
            case "thinking_delta":
              if (event.text) {
                accumulatedThinking += event.text;
                updateAssistantMessage(assistantMessage.id, (message) => ({
                  ...message,
                  thinking: accumulatedThinking,
                }));
              }
              break;
            case "content_delta":
              if (event.text) {
                accumulatedContent += event.text;
                updateAssistantMessage(assistantMessage.id, (message) => ({
                  ...message,
                  content: accumulatedContent,
                }));
              }
              break;
            case "tool_call": {
              const { id, name, input } = event;
              if (id && name) {
                updateAssistantMessage(assistantMessage.id, (message) => ({
                  ...message,
                  toolCalls: upsertToolCall(message.toolCalls, {
                    id,
                    name,
                    input,
                  }),
                }));
              }
              break;
            }
            case "tool_result": {
              const { tool_call_id, name, content: resultContent, metadata, is_error } = event;
              if (tool_call_id && name && typeof resultContent === "string") {
                updateAssistantMessage(assistantMessage.id, (message) => ({
                  ...message,
                  toolResults: upsertToolResult(message.toolResults, {
                    tool_call_id,
                    name,
                    content: resultContent,
                    metadata,
                    is_error,
                  }),
                }));
              }
              break;
            }
            case "done":
              updateAssistantMessage(assistantMessage.id, (message) => ({
                ...message,
                isStreaming: false,
              }));
              break;
            case "error":
              updateAssistantMessage(assistantMessage.id, (message) => ({
                ...message,
                content: `Error: ${event.error ?? "Unknown error"}`,
                isStreaming: false,
              }));
              break;
          }
        }
      }

      if (buffer.trim()) {
        for (const event of parseSSEEvents(buffer)) {
          if (event.type === "done") {
            updateAssistantMessage(assistantMessage.id, (message) => ({
              ...message,
              isStreaming: false,
            }));
          }
        }
      }
    } catch (error) {
      console.error("Chat error:", error);
      updateAssistantMessage(assistantMessage.id, (message) => ({
        ...message,
        content: `Failed to send message: ${error}`,
        isStreaming: false,
      }));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div class="flex h-full flex-col">
      <div class="flex-1 overflow-y-auto p-4">
        <Show when={messages().length === 0}>
          <div class="flex h-full items-center justify-center">
            <div class="text-center text-muted-foreground">
              <h2 class="mb-2 text-2xl font-semibold text-foreground">Welcome to Pando</h2>
              <p>Start a conversation by typing a message below.</p>
            </div>
          </div>
        </Show>
        <For each={messages()}>
          {(message) => <MessageBubble message={message} />}
        </For>
        <div ref={messagesEndRef} />
      </div>
      <ChatInput onSend={sendMessage} disabled={isLoading()} />
    </div>
  );
};

export default ChatView;
