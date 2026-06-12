# Crush - Deep Architectural Analysis

## Ultraviolet (UV) Rendering Framework
Crush does NOT use the traditional bubbletea pattern (View() → string). It uses **Ultraviolet**:

```go
// Canvas-based rendering
canvas := uv.NewScreenBuffer(width, height)
m.Draw(canvas, canvas.Bounds())
v.Content = canvas.Render()

// In components
uv.NewStyledString(view).Draw(scr, area)
```

**Difference with Pando**: Pando uses the traditional View() → string pattern with lipgloss. It doesn't need to migrate to UV, but it's good to know that crush's rendering is more advanced.

## UI States Machine
```
uiOnboarding → uiInitialize → uiLanding → uiChat
```
Each state has its own set of keybindings and rendering.

## Focus States
```go
const (
    uiFocusNone   // No focus (landing, onboarding)
    uiFocusEditor // Focus on textarea
    uiFocusMain   // Focus on chat (scroll, select)
)
```

## Key Event Routing (handleKeyPressMsg)
Priority order:
1. `Ctrl+C` → always Quit dialog
2. Open dialogs → route to dialog.HandleMsg()
3. `Esc` → cancel agent / clear queue
4. Based on UI state:
   - **Editor focus**: Completions → Attachments → Editor keys → Global
   - **Chat focus**: Chat navigation keys
5. **`@` detection**: trigger real-time completions (line 1664)
6. **`/` detection**: when textarea empty, open command palette (line 1649)

## Commands Dialog (3 tabs)
```
┌─ Commands ──────────────────────┐
│ [System] [User] [MCP Prompts]   │
│ ┌─────────────────────────────┐ │
│ │ 🔍 Filter...               │ │
│ │ ─────────────────────────── │ │
│ │ New Session      Ctrl+N    │ │
│ │ Toggle Help      Ctrl+G    │ │
│ │ Toggle Compact   Ctrl+T    │ │
│ │ External Editor  Ctrl+O    │ │
│ │ ...                        │ │
│ └─────────────────────────────┘ │
└─────────────────────────────────┘
```
- Tab/Shift+Tab between System/User/MCP
- Real-time fuzzy search
- Spinner during async loading of User/MCP commands

## Pills (Todo/Queue)
Section between chat and editor:
```
│ Chat messages...                    │
├─────────────────────────────────────┤
│ • ⋯ To-Do 2/5  Current task...     │
│   ✓ Completed item                 │
│   → In-progress item               │
│ • ▶▶▶▶ 3 Queued                    │
│   → First queued item              │
├─────────────────────────────────────┤
│ Editor (input area)                 │
```
- Toggle with Ctrl+T
- pillsExpanded boolean
- pillSectionTodos / pillSectionQueue

## Crush Sidebar
```
├── Logo (small or large based on height)
├── Session Title
├── Current Working Directory
├── Model Info (name, provider, reasoning)
├── Files Section (dynamic - max 10)
├── LSP Status Section (max 8)
└── MCP Status Section (max 8)
```
- `getDynamicHeightLimits()` distributes height between sections
- Compact mode: sidebar disappears, compact header + overlay with Ctrl+D

## Chat Message Types
```go
UserMessageItem       // User messages (with attachments)
AssistantMessageItem  // Responses (markdown rendered)
ToolCallItem          // Executed tools
TodoItem              // Todo items (status + name)
ReferencesItem        // File references
SearchItem            // Searches performed
DiagnosticsItem       // LSP errors
```

Each one:
- Implements `MessageItem` interface
- Render cache by width
- Can be animated (only if visible)
- Supports highlight (text selection)
- Supports focus

## Lazy-Loaded List (list/list.go)
```go
type List struct {
    items []Item
    offsetIdx, offsetLine int   // Virtualization
    selectedIdx int
    renderCallbacks []func()    // Pre-render hooks
}
```
- Only renders visible items
- Render callbacks to apply focus/highlight
- Supports gaps between items
- FilterableList with fuzzy search (sahilm/fuzzy)

## Gradient Styles
- Uses `charmtone` for color gradients
- Applied in Pills, tools, etc.
- Semantic styles: Primary, Secondary, Tertiary, BgBase, FgBase, etc.

## Patterns to Port to Pando

### 1. Action Pattern (RECOMMENDED)
Dialogs return typed Action types, the main model processes them.
Pando currently uses generic tea.Msg - could benefit from Actions.

### 2. Lazy List with Callbacks (RECOMMENDED)
Crush's list only renders visible items and uses callbacks to transform items.
Pando uses bubbles viewport - could improve performance with lazy rendering.

### 3. @ and / Detection (PARTIALLY IMPLEMENTED)
Pando already has @ for completions. Could improve / for slash commands.

### 4. Pills Todo/Queue (OPTIONAL)
Could be useful for showing agent progress in a more visual way.

### 5. Dynamic Sidebar Heights (RECOMMENDED)
Dynamic height distribution between sidebar sections is elegant.