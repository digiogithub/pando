import { type Component, type JSX, For, createSignal, Show, onMount } from "solid-js";
import { useServer } from "@/hooks/useServer";
import type { Session, FileNode } from "@/types";

interface SidebarProps {
  width: number;
}

const Sidebar: Component<SidebarProps> = (props) => {
  const { fetchAPI } = useServer();
  const [sessions, setSessions] = createSignal<Session[]>([]);
  const [files, setFiles] = createSignal<FileNode[]>([]);
  const [activeTab, setActiveTab] = createSignal<"sessions" | "files">("files");
  const [expandedDirs, setExpandedDirs] = createSignal<Set<string>>(new Set());

  onMount(async () => {
    try {
      const sessionsData = await fetchAPI<Session[]>("/api/v1/sessions");
      setSessions(sessionsData || []);
    } catch {
      console.error("Failed to load sessions");
    }

    try {
      const projectData = await fetchAPI<{ files: FileNode[] }>("/api/v1/files");
      setFiles(projectData?.files || []);
    } catch {
      console.error("Failed to load files");
    }
  });

  const toggleDir = (path: string) => {
    const expanded = new Set(expandedDirs());
    if (expanded.has(path)) {
      expanded.delete(path);
    } else {
      expanded.add(path);
    }
    setExpandedDirs(expanded);
  };

  const renderFileTree = (nodes: FileNode[], depth = 0): JSX.Element => {
    return (
      <For each={nodes}>
        {(node) => (
          <div>
            <button
              class="flex w-full items-center gap-1 truncate rounded px-1 py-0.5 text-left text-sm hover:bg-muted"
              style={{ "padding-left": `${depth * 12 + 4}px` }}
              onClick={() => node.is_dir && toggleDir(node.path)}
            >
              <Show when={node.is_dir}>
                <svg
                  class="h-4 w-4 text-muted-foreground"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                  style={{
                    transform: expandedDirs().has(node.path)
                      ? "rotate(90deg)"
                      : "rotate(0deg)",
                  }}
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M9 5l7 7-7 7"
                  />
                </svg>
              </Show>
              <Show when={!node.is_dir}>
                <svg
                  class="h-4 w-4 text-muted-foreground"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                  />
                </svg>
              </Show>
              <span class="truncate">{node.name}</span>
            </button>
            <Show when={node.is_dir && expandedDirs().has(node.path) && node.children}>
              {renderFileTree(node.children!, depth + 1)}
            </Show>
          </div>
        )}
      </For>
    );
  };

  return (
    <aside
      class="flex h-full flex-col border-r border-border bg-card"
      style={{ width: `${props.width}px` }}
    >
      <div class="flex border-b border-border">
        <button
          class={`flex-1 px-3 py-2 text-sm font-medium ${
            activeTab() === "files"
              ? "border-b-2 border-primary text-primary"
              : "text-muted-foreground hover:text-foreground"
          }`}
          onClick={() => setActiveTab("files")}
        >
          Files
        </button>
        <button
          class={`flex-1 px-3 py-2 text-sm font-medium ${
            activeTab() === "sessions"
              ? "border-b-2 border-primary text-primary"
              : "text-muted-foreground hover:text-foreground"
          }`}
          onClick={() => setActiveTab("sessions")}
        >
          Sessions
        </button>
      </div>

      <div class="flex-1 overflow-y-auto p-2">
        <Show when={activeTab() === "files"}>
          <div class="space-y-0.5">{renderFileTree(files())}</div>
        </Show>
        <Show when={activeTab() === "sessions"}>
          <div class="space-y-1">
            <For each={sessions()}>
              {(session) => (
                <button class="w-full truncate rounded px-2 py-1.5 text-left text-sm hover:bg-muted">
                  <div class="font-medium truncate">{session.title || "New Chat"}</div>
                  <div class="text-xs text-muted-foreground">
                    {session.message_count} messages
                  </div>
                </button>
              )}
            </For>
          </div>
        </Show>
      </div>
    </aside>
  );
};

export default Sidebar;
