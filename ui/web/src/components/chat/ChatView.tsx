import { type Component, For, createSignal, onCleanup, createEffect, onMount, Show } from "solid-js";
import { useServer } from "@/hooks/useServer";
import type { Message, SSEEvent } from "@/types";
import MessageBubble from "./MessageBubble";
import ChatInput from "./ChatInput";

const ChatView: Component = () => {
  const { token } = useServer();
  const [messages, setMessages] = createSignal<Message[]>([]);
  const [isLoading, setIsLoading] = createSignal(false);
  const [streamingContent, setStreamingContent] = createSignal("");
  let messagesEndRef: HTMLDivElement | undefined;

  const scrollToBottom = () => {
    messagesEndRef?.scrollIntoView({ behavior: "smooth" });
  };

  createEffect(() => {
    messages();
    scrollToBottom();
  });

  const sendMessage = async (content: string) => {
    if (!content.trim() || isLoading()) return;

    const userMessage: Message = {
      id: crypto.randomUUID(),
      role: "user",
      content: content.trim(),
      timestamp: new Date(),
    };

    setMessages((prev) => [...prev, userMessage]);
    setIsLoading(true);
    setStreamingContent("");

    const assistantMessage: Message = {
      id: crypto.randomUUID(),
      role: "assistant",
      content: "",
      timestamp: new Date(),
      isStreaming: true,
    };

    setMessages((prev) => [...prev, assistantMessage]);

    try {
      const t = token();
      const response = await fetch("/api/v1/chat/stream", {
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

      let accumulatedContent = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value, { stream: true });
        const lines = chunk.split("\n");

        for (const line of lines) {
          if (line.startsWith("data: ")) {
            try {
              const data = JSON.parse(line.slice(6)) as SSEEvent;

              switch (data.type) {
                case "content_delta":
                  if (data.content) {
                    accumulatedContent += data.content;
                    setStreamingContent(accumulatedContent);
                    setMessages((prev) =>
                      prev.map((m) =>
                        m.id === assistantMessage.id
                          ? { ...m, content: accumulatedContent }
                          : m
                      )
                    );
                  }
                  break;
                case "complete":
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantMessage.id
                        ? { ...m, isStreaming: false }
                        : m
                    )
                  );
                  break;
                case "error":
                  console.error("SSE Error:", data.error);
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantMessage.id
                        ? {
                            ...m,
                            content: `Error: ${data.error}`,
                            isStreaming: false,
                          }
                        : m
                    )
                  );
                  break;
              }
            } catch {
              // Ignore JSON parse errors for incomplete chunks
            }
          }
        }
      }
    } catch (error) {
      console.error("Chat error:", error);
      setMessages((prev) =>
        prev.map((m) =>
          m.id === assistantMessage.id
            ? {
                ...m,
                content: `Failed to send message: ${error}`,
                isStreaming: false,
              }
            : m
        )
      );
    } finally {
      setIsLoading(false);
      setStreamingContent("");
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
