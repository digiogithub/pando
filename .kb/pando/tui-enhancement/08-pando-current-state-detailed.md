# Pando TUI - Current State Detailed

## Charmbracelet Dependencies Already Present
```
github.com/charmbracelet/bubbles v0.21.0
github.com/charmbracelet/bubbletea v1.3.5
github.com/charmbracelet/lipgloss v1.1.0
github.com/charmbracelet/glamour v0.9.1        # ALREADY EXISTS
github.com/charmbracelet/x/ansi v0.8.0
github.com/lrstanley/bubblezone v0.0.0-...     # ALREADY EXISTS
```

**IMPORTANT**: glamour and bubblezone are already dependencies. Phases 4 and 5 don't need to add them.

## Current Keybindings (more than expected)
| Shortcut | Action | Context |
|----------|--------|----------|
| `ctrl+l` | Logs page | Global |
| `ctrl+m` | Orchestrator page | Global |
| `ctrl+c` | Quit dialog | Global |
| `ctrl+_`/`ctrl+h` | Toggle help | Global |
| `ctrl+g` | Settings page | Global |
| `ctrl+s` | Switch session | ChatPage |
| `ctrl+k` | Commands dialog | ChatPage |
| `ctrl+f` | File picker | Global |
| `ctrl+o` | Model selection | Global |
| `ctrl+t` | Theme switcher | Global |
| `ctrl+n` | New session | ChatPage |
| `ctrl+e` | External editor | Editor |
| `ctrl+r` | Delete mode attachments | Editor |
| `@` | Completion dialog | Editor |
| `esc` | Close dialogs | Global |
| `?` | Help | Global |
| `enter`/`ctrl+s` | Send message | Editor |

## Pages (4 complete pages)
1. **ChatPage** - Main chat with split pane (messages + editor + sidebar)
2. **LogsPage** - Logs table + details
3. **SettingsPage** - Settings with dynamic sections
4. **OrchestratorPage** - Mesnada task dashboard

## Dialogs (12 dialogs)
1. Permission, Session, Command, Model, Init
2. Filepicker, Theme, MultiArguments, Completion
3. Help, Quit, CustomCommands

## Sidebar ALREADY EXISTS (chat/sidebar.go - 379 lines)
- Session information
- LSP settings
- **Modified files with change statistics (+/-)**
- Calculation of diffs between initial and current version
- Tracking additions/removals per file

## Theme System (9 themes, 77 color methods)
Themes: OneDark, TokyoNight, Flexoki, Tron, Gruvbox, Monokai, Catppuccin, Dracula, OpenCode

Colors already defined for:
- **Diff**: DiffAdded, DiffRemoved, DiffContext, etc. (10 colors)
- **Markdown**: MarkdownText, MarkdownHeading, MarkdownLink, etc. (14 colors)
- **Syntax**: SyntaxComment, SyntaxKeyword, SyntaxFunction, etc. (8 colors)

## Markdown Rendering
- `internal/tui/styles/markdown.go` - Rendering already exists with glamour
- Integrated in `message.go` for chat messages

## Chat Components
- **Editor** (319 lines): TextArea, attachments, external editor
- **Message List** (488 lines): Viewport, spinner, cache
- **Messages** (660 lines): User/assistant/tools rendering, inline diffs for Edit
- **Sidebar** (379 lines): Modified files with diff stats

## Layout
- **SplitPaneLayout**: Left (messages) + Right (sidebar) + Bottom (editor)
- **Container**: Padding, borders, dynamic styles
- **Overlay**: For centered modal dialogs

## What's TRULY Missing vs Crush

| Feature | Status | Priority |
|---------|--------|-----------|
| File Tree navigator (expandable, not just list) | DOES NOT EXIST | High |
| File Editor/Viewer with syntax highlighting | DOES NOT EXIST | High |
| Tab system for files | DOES NOT EXIST | High |
| Complete DiffView (split/unified with scroll) | PARTIAL (inline in messages) | High |
| Mouse clicks in sidebar/chat | DOES NOT EXIST (bubblezone imported but not used) | Medium |
| Command palette with fuzzy search | BASIC (CommandDialog without fuzzy) | Medium |
| Hierarchical KeyMap | PARTIAL (has shortcuts but no hierarchical structure) | Low |
| Markdown with glamour | ALREADY EXISTS | - |
| chroma syntax highlighting | DOES NOT EXIST | High |
| Dialog overlay stack | PARTIAL (booleans, no stack) | Low |

## Conclusion
Pando is more advanced than expected. The main gaps are:
1. Interactive **File Tree navigator** (not the current modified files sidebar)
2. **Editor/Viewer** with syntax highlighting (chroma)
3. Complete **DiffView** as standalone component
4. Real **Mouse interaction** (bubblezone is imported but not actively used)
5. **Fuzzy search** in command palette