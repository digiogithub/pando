import { type Component } from "solid-js";

interface HeaderProps {
  sidebarOpen: boolean;
  onToggleSidebar: () => void;
}

const Header: Component<HeaderProps> = (props) => {
  return (
    <header class="flex h-12 items-center justify-between border-b border-border bg-card px-4">
      <div class="flex items-center gap-2">
        <button
          onClick={() => props.onToggleSidebar()}
          class="rounded p-1.5 hover:bg-muted"
          title={props.sidebarOpen ? "Hide sidebar" : "Show sidebar"}
        >
          <svg
            class="h-5 w-5"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <Show
              when={props.sidebarOpen}
              fallback={
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M4 6h16M4 12h16M4 18h16"
                />
              }
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M11 19l-7-7 7-7m8 14l-7-7 7-7"
              />
            </Show>
          </svg>
        </button>
        <h1 class="text-lg font-semibold">Pando</h1>
      </div>
      <div class="flex items-center gap-2">
        <select
          class="rounded-md border border-border bg-background px-2 py-1 text-sm"
          aria-label="Select model"
        >
          <option value="claude-sonnet-4.6">Claude Sonnet 4.6</option>
          <option value="claude-opus-4.6">Claude Opus 4.6</option>
          <option value="gpt-5.4">GPT-5.4</option>
          <option value="gemini-3-pro">Gemini 3 Pro</option>
        </select>
        <button
          class="rounded p-1.5 hover:bg-muted"
          title="Settings"
        >
          <svg
            class="h-5 w-5"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
            />
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
            />
          </svg>
        </button>
      </div>
    </header>
  );
};

export default Header;
