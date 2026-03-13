import { type Component, createSignal, onMount, onCleanup } from "solid-js";

interface ChatInputProps {
  onSend: (content: string) => void;
  disabled?: boolean;
}

const ChatInput: Component<ChatInputProps> = (props) => {
  const [input, setInput] = createSignal("");
  let textareaRef: HTMLTextAreaElement | undefined;

  const handleSubmit = (e?: Event) => {
    e?.preventDefault();
    const content = input();
    if (content.trim() && !props.disabled) {
      props.onSend(content);
      setInput("");
      if (textareaRef) {
        textareaRef.style.height = "auto";
      }
    }
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  const adjustHeight = () => {
    if (textareaRef) {
      textareaRef.style.height = "auto";
      textareaRef.style.height = `${Math.min(textareaRef.scrollHeight, 200)}px`;
    }
  };

  onMount(() => {
    textareaRef?.focus();
  });

  return (
    <div class="border-t border-border bg-background p-4">
      <form onSubmit={handleSubmit} class="flex gap-2">
        <div class="relative flex-1">
          <textarea
            ref={textareaRef}
            value={input()}
            onInput={(e) => {
              setInput(e.currentTarget.value);
              adjustHeight();
            }}
            onKeyDown={handleKeyDown}
            placeholder="Send a message... (Shift+Enter for new line)"
            disabled={props.disabled}
            rows={1}
            class="w-full resize-none rounded-lg border border-border bg-background px-4 py-3 pr-12 text-foreground placeholder:text-muted-foreground focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary disabled:cursor-not-allowed disabled:opacity-50"
          />
        </div>
        <button
          type="submit"
          disabled={props.disabled || !input().trim()}
          class="rounded-lg bg-primary px-4 py-2 font-medium text-primary-foreground hover:bg-primary/90 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Show
            when={!props.disabled}
            fallback={
              <svg
                class="h-5 w-5 animate-spin"
                fill="none"
                viewBox="0 0 24 24"
              >
                <circle
                  class="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  stroke-width="4"
                />
                <path
                  class="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
                />
              </svg>
            }
          >
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"
              />
            </svg>
          </Show>
        </button>
      </form>
      <p class="mt-2 text-center text-xs text-muted-foreground">
        Pando can make mistakes. Consider checking important information.
      </p>
    </div>
  );
};

export default ChatInput;
