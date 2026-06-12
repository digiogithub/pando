# Pando TUI Enhancement - Master Implementation Plan

## Overview
Transform Pando from a basic conversational AI assistant to a complete TUI IDE, inspired by crush's architecture, with support for:
- Project navigation with file sidebar panel
- Code editor with syntax highlighting
- Glow-style markdown rendering
- Diff viewer for AI agent changes
- Full mouse support with bubblezone
- Advanced keybindings system and command palette

## Current State of Pando (TUI)

### Existing Architecture
- **Main model**: `internal/tui/tui.go` - `appModel` struct with page and dialog system
- **Pages**: `internal/tui/page/` - ChatPage, LogsPage
- **Components**: `internal/tui/components/` - chat/, dialog/, logs/, util/
- **Layout**: `internal/tui/layout/` - SplitPane, overlay, containers
- **Styles**: `internal/tui/styles/` - icons, theme
- **Existing dialogs**: Permission, Session, Command, Model, Init, Filepicker, Theme, MultiArguments, Completion, Help, Quit

### Current Keybindings (tui.go)
```go
keyMap struct {
    Logs, Quit, SwitchSession, Filepicker key.Binding
    // + helpEsc, returnKey
}
```

### Current Charmbracelet Dependencies
- bubbletea, bubbles, lipgloss, glamour (verify in go.mod)

## Crush Analysis (Reference)

### Crush UI Architecture
```
internal/ui/
├── AGENTS.md          # UI development guidelines
├── model/
│   ├── ui.go          # Main model (message routing, focus, layout, dialogs)
│   ├── keys.go        # KeyMap struct (Editor, Chat, Initialize, Global)
│   └── chat.go        # Chat logic with full mouse support
├── chat/              # Chat message item types and renderers
│   ├── messages.go    # MessageItem interfaces, caching, highlighting
│   ├── assistant.go   # AssistantMessageItem with KeyEventHandler
│   └── mcp.go         # MCP resource rendering
├── dialog/
│   ├── dialog.go      # Overlay system (stack-based dialogs)
│   ├── actions.go     # Action types (SelectSession, SelectModel, NewSession, etc.)
│   ├── sessions.go    # Session dialog (list, select, rename, delete)
│   ├── models.go      # Model selection dialog (large/small, tab switch)
│   ├── permissions.go # Permission dialog with diff view
│   ├── quit.go        # Quit confirmation
│   ├── filepicker.go  # File picker dialog
│   ├── oauth.go       # OAuth dialog
│   └── api_key_input.go
├── completions/
│   ├── completions.go # Command/file completion popup
│   └── keys.go        # Completion keybindings
├── diffview/
│   ├── diffview.go    # Full DiffView with split/unified modes
│   ├── split.go       # Split view rendering
│   └── style.go       # Diff styling with chroma
├── list/              # Generic list component with lazy rendering
│   ├── list.go
│   ├── item.go        # List item interfaces
│   └── highlight.go   # Content highlighting
├── common/
│   ├── markdown.go    # MarkdownRenderer using glamour
│   ├── highlight.go   # SyntaxHighlight using chroma
│   └── capabilities.go
├── styles/
│   └── styles.go      # All style definitions with semantic colors
├── anim/              # Animation system
└── logo/              # Logo rendering
```

### Key Crush Patterns
1. **Overlay System**: Stack of dialogs (`dialog.Overlay`) - last dialog receives events
2. **Action Pattern**: Dialogs return `Action` types that the main model processes
3. **Hierarchical KeyMap**: Editor > Chat > Initialize > Global bindings
4. **Dumb Components**: Don't handle tea.Msg directly, expose methods
5. **Native Mouse Support**: HandleMouseDown/Up/Drag in Chat with text selection
6. **Full DiffView**: Split/Unified with syntax highlighting via chroma
7. **Markdown**: glamour.TermRenderer with custom styles
8. **Syntax Highlighting**: chroma with lexer cache

## Implementation Phases

### Phase 1: Keybindings System and Command Palette (Priority: HIGH)
- Expand KeyMap with hierarchical structure (Editor, Chat, Global)
- Implement enhanced command palette (crush-style)
- Global shortcuts: Ctrl+P (commands), Ctrl+M (models), Ctrl+S (sessions)
- Help overlay with shortcuts list

### Phase 2: File Explorer Side Panel (Priority: HIGH)
- FileTree component with directory tree
- Toggle with keybinding (Ctrl+B or similar)
- Integration with existing SplitPane
- Keyboard and mouse navigation
- Modified file indicators (git status)

### Phase 3: TUI Editor with Syntax Highlighting (Priority: MEDIUM)
- File viewer/editor with chroma syntax highlighting
- Line numbers, scroll, search
- Tab system for multiple open files
- Integration with side panel

### Phase 4: Enhanced Markdown Rendering (Priority: MEDIUM)
- Integrate glamour to render AI responses
- Code blocks with syntax highlighting
- Support for tables, lists, links
- Customizable style (dark/light theme)

### Phase 5: Mouse Support with Bubblezone (Priority: MEDIUM)
- Integrate bubblezone for clickable zones
- Clicks on side panel to open files
- Clicks on editor tabs
- Scroll with mouse wheel
- Text selection in chat (like crush)

### Phase 6: Diff Viewer and Change Management (Priority: HIGH)
- Port DiffView from crush (split/unified modes)
- Panel of files modified by AI agent
- Inline diff view in chat (permissions)
- Navigation between changes
- Integrated git status

## Dependencies to Add
```
github.com/charmbracelet/glamour     # Markdown rendering
github.com/alecthomas/chroma/v2      # Syntax highlighting
github.com/lrstanley/bubblezone      # Mouse zones
```

## Architectural Considerations
1. **Maintain compatibility** with the current page/dialog structure
2. **Existing SplitPane** can be reused for the side panel
3. **Overlay system** in Pando already works, just needs expansion
4. **Components must be lazy** - don't render what's not visible
5. **Aggressive caching** of syntax highlighting (like crush)
6. **Responsive layout** - adapt panels to terminal size