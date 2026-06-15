---
created_at: 2026-06-15T21:55:26.731488895Z
updated_at: 2026-06-15T21:55:26.731488895Z
tags:
    - opencode
    - research
    - tui
    - sidebar
    - reference
---
# opencode TUI — Lateral Info Sidebar (Reference)

Analysis of how opencode's TUI renders the right-hand information sidebar that
appears when the terminal is wide enough, showing session info, the current
task plan (Todo), modified files, and more.

opencode's TUI is built in **TypeScript + SolidJS** (OpenTUI-style renderer with
`<box>`, `<text>`, `<scrollbox>` intrinsic elements), NOT Go. Sidebar content is
provided by a **plugin slot system**, so each section is an independent builtin
plugin.

## 1. Responsive visibility — when the sidebar shows

File: `packages/tui/src/routes/session/index.tsx`

```ts
const dimensions = useTerminalDimensions()
const [sidebar, setSidebar] = kv.signal<"auto" | "hide">("sidebar", "auto")   // persisted pref
const [sidebarOpen, setSidebarOpen] = createSignal(false)                     // manual override

const wide = createMemo(() => dimensions().width > 120)        // KEY BREAKPOINT: width > 120 cols
const sidebarVisible = createMemo(() => {
  if (session()?.parentID) return false   // hidden for sub-sessions / sub-agents
  if (sidebarOpen()) return true          // manual force-open (works even when narrow)
  if (sidebar() === "auto" && wide()) return true
  return false
})
```

Rules:
- **Breakpoint = 120 columns.** With `width > 120` and pref `auto`, the sidebar auto-shows.
- It is **hidden for child sessions** (`session.parentID` set, e.g. sub-agents).
- `sidebar` pref (`"auto" | "hide"`) is persisted via `kv.signal`; `sidebarOpen` is a transient manual toggle.

### Rendering: docked vs overlay
At the bottom of the layout the sidebar renders in two modes depending on width:

```tsx
<Show when={sidebarVisible()}>
  <Switch>
    <Match when={wide()}>
      <Sidebar sessionID={route.sessionID} />          {/* docked column on the right */}
    </Match>
    <Match when={!wide()}>
      <box position="absolute" top={0} left={0} right={0} bottom={0}
           alignItems="flex-end" backgroundColor={RGBA.fromInts(0,0,0,70)}>
        <Sidebar sessionID={route.sessionID} />          {/* overlay drawer over a dim scrim */}
      </box>
    </Match>
  </Switch>
</Show>
```

- **Wide terminal:** sidebar is a real docked column to the right of the chat.
- **Narrow terminal (manually opened):** sidebar slides in as an absolute-positioned overlay (right-aligned) over a translucent black scrim.

### Toggle
- Keybind: `sidebar_toggle: keybind("<leader>b", "Toggle sidebar")` (`packages/tui/src/config/keybind.ts`), command `session.sidebar.toggle`.
- Command palette entry "Hide/Show sidebar" flips `setSidebar(auto|hide)` + `setSidebarOpen`.
- (Desktop app mirrors this with `Cmd+B` "Toggle Sidebar".)

## 2. The Sidebar shell

File: `packages/tui/src/routes/session/sidebar.tsx`

```tsx
export function Sidebar(props: { sessionID: string; overlay?: boolean }) {
  return (
    <Show when={session()}>
      <box backgroundColor={theme.backgroundPanel} width={42} height="100%"
           paddingTop={1} paddingBottom={1} paddingLeft={2} paddingRight={2}
           position={props.overlay ? "absolute" : "relative"}>
        <scrollbox flexGrow={1} scrollAcceleration={...}>
          <box flexShrink={0} gap={1} paddingRight={1}>
            <pluginRuntime.Slot name="sidebar_title" mode="single_winner" ... >
              {/* default: session title, session id (non-latest channel), workspace label, share url */}
            </pluginRuntime.Slot>
            <pluginRuntime.Slot name="sidebar_content" session_id={props.sessionID} />
          </box>
        </scrollbox>
        <box flexShrink={0} gap={1} paddingTop={1}>
          <pluginRuntime.Slot name="sidebar_footer" mode="single_winner" ...>
            {/* default: "• OpenCode <version>" */}
          </pluginRuntime.Slot>
        </box>
      </box>
    </Show>
  )
}
```

Key facts:
- **Fixed width = 42 cols**, full height, with a `backgroundPanel` color and 1/2 padding.
- Three slot regions: `sidebar_title` (single_winner), `sidebar_content` (stacked, all plugins render), `sidebar_footer` (single_winner).
- The content area is a `<scrollbox>` so sections scroll independently; footer is pinned at the bottom.

## 3. Sidebar sections = builtin plugins (slot `sidebar_content`)

Each section lives in `packages/tui/src/feature-plugins/sidebar/` and registers
into the `sidebar_content` slot with an `order` (lower = rendered higher up).
Registered via `createBuiltinPlugins` in `feature-plugins/builtins.ts`.

| order | file        | section          | shows when |
|-------|-------------|------------------|------------|
| 100   | context.tsx | **Context**      | always (tokens, % context used, $ spent) |
| 200   | mcp.tsx     | **MCP** servers  | when MCP servers present |
| 300   | lsp.tsx     | **LSP** servers  | when LSP servers present |
| 400   | todo.tsx    | **Todo** (plan)  | todos exist AND at least one not completed |
| 500   | files.tsx   | **Modified Files** | session diff list non-empty |

### Context (order 100) — `context.tsx`
Reads last assistant message tokens (`input+output+reasoning+cache.read+cache.write`),
computes `percent = tokens / model.limit.context * 100`, and `session.cost`.
Renders: bold "Context", `<tokens> tokens`, `<percent>% used`, `$<cost> spent`.

### Todo / current task plan (order 400) — `todo.tsx`
```ts
const list = createMemo(() => props.api.state.session.todo(props.session_id))
const show = createMemo(() => list().length > 0 && list().some((i) => i.status !== "completed"))
```
- Section only shows while there are **incomplete** todos (auto-hides when plan done).
- Header "Todo"; if `list.length > 2`, a `▼/▶` toggle (click via `onMouseDown`) collapses/expands the list.
- Each item rendered by `<TodoItem status content />` (`component/todo-item`), styled by status (pending/in_progress/completed).

### Modified Files (order 500) — `files.tsx`
```ts
const list = createMemo(() => props.api.state.session.diff(props.session_id))
```
- Shows only when `list.length > 0`.
- Header "Modified Files"; collapsible (`▼/▶`) when more than 2 files.
- Per row: left-truncated file path (`Locale.truncateLeft(file, max(2, 36 - changeCountWidth))`) on the left,
  `+additions` (theme.diffAdded green) and `-deletions` (theme.diffRemoved red) right-aligned.
- `changeCountWidth` reserves space for the +/- counts so the path truncation fits the 42-col panel.

## 4. Data source & live updates

The sidebar is fully reactive off the TUI sync store.

- Plugin API surface (`packages/tui/src/plugin/adapters.tsx`):
  - `state.session.diff(sessionID)` → reads `sync.data.session_diff[sessionID]` (filters out items without `file`).
  - `state.session.todo(sessionID)` → reads `sync.data.todo[sessionID]`.
- Store shape (`packages/tui/src/context/sync.tsx`):
  - `session_diff: { [sessionID]: SnapshotFileDiff[] }`
  - `todo: { [sessionID]: Todo[] }`
- Populated two ways:
  1. **Initial load** when a session opens: `sdk.client.session.todo(...)` and `sdk.client.session.diff(...)` fill `draft.todo[...]` / `draft.session_diff[...]`.
  2. **Live SSE server events** in the event reducer:
     - `case "todo.updated": setStore("todo", event.properties.sessionID, event.properties.todos)`
     - `case "session.diff": setStore("session_diff", event.properties.sessionID, event.properties.diff)`

So as the agent edits files or updates its plan, the server emits `session.diff` /
`todo.updated`, the store updates, SolidJS memos recompute, and the sidebar
sections re-render (and auto show/hide) without manual refresh. The diff data
ultimately comes from the project VCS/snapshot layer (`project/vcs.ts`,
`SnapshotFileDiff`).

## 5. Relevant types
- `packages/plugin/src/tui.ts`: `TuiSidebarFileItem` (`file`, `additions`, `deletions`), `TuiSidebarTodoItem` (`content`, `status`), `TuiSidebarLspItem`, `TuiSidebarMcpItem`, `TuiHostSlotMap` (slot prop contracts), and `TuiState.diff/todo` signatures.

## 6. Summary of behavior
1. Terminal `width > 120` (pref `auto`) → sidebar auto-docks as a 42-col right column; otherwise hidden unless force-opened with `<leader>b` (then overlay drawer).
2. Hidden entirely for sub-sessions/sub-agents (`parentID`).
3. Content = ordered stack of builtin plugins: Context, MCP, LSP, Todo, Modified Files; title + footer are single-winner slots.
4. Todo section = the live task plan, visible only while incomplete tasks remain.
5. Modified Files = live per-file +/- diff for the session, both collapsible.
6. Everything reactive via the sync store, fed by initial SDK fetch + live `todo.updated` / `session.diff` SSE events.

### Ideas for porting to pando (Go/Bubble Tea TUI)
- A width breakpoint gate (analogous to `width > 120`) to auto-show a right panel; manual toggle keybind otherwise.
- A small registry/order system so panel sections (context/tokens, todos/plan, modified files, MCP/LSP) are pluggable and independently show/hide.
- Drive sections off the existing event stream (pando already has `AgentEventTypeTokenUsage`, file-change tracking) so they update live.
- Collapsible sections + left-truncated paths with right-aligned +/- counts for the modified-files list.
