# Crush vs Pando Comparison - TUI Feature Analysis

## Comparison Table

| Feature | Crush | Pando | Gap |
|---------|-------|-------|-----|
| **Keybindings** | Hierarchical KeyMap (Editor/Chat/Init/Global) | Flat keyMap (4 bindings) | High |
| **Command Palette** | Completions with fuzzy, files + MCP resources | Basic CompletionDialog | Medium |
| **Sessions** | Complete dialog (list, select, rename, delete) | Basic SessionDialog | Low |
| **Models** | Dialog with Large/Small, tab switch, API key | Existing ModelDialog | Low |
| **Overlay System** | Dialog stack (`dialog.Overlay`) | Individual show* booleans | Medium |
| **DiffView** | Split/Unified with syntax highlight + cache | Does not exist | High |
| **Markdown** | glamour with custom styles | Basic or nonexistent | High |
| **Syntax Highlight** | chroma with lexer cache | Does not exist in TUI | High |
| **Mouse Chat** | HandleMouseDown/Up/Drag, selectWord/Line | No | High |
| **File Explorer** | FilePicker dialog (no permanent sidebar) | FilePicker dialog | Medium |
| **Sidebar** | Only logo (`sidebarLogo`) | No | High |
| **Editor** | No (uses external editor) | No | High |
| **Animation** | Animation system (`anim/`) | No | Low |
| **Permission Diff** | Inline diff in permission dialog | Basic permissionDialogCmp | Medium |
| **Help** | Help overlay with ShortHelp/FullHelp | Existing HelpCmp | Low |
| **Themes** | Semantic styles, chroma themes | Existing ThemeDialog | Low |
| **Layout** | Custom layout with `uv.Screen` | SplitPane, overlay, containers | Low |
| **Status Bar** | StatusLine with session/model info | Existing StatusCmp | Low |

## Unique Features Pando Must Have (Beyond Crush)

1. **Permanent file sidebar** - Crush doesn't have it, Pando needs it
2. **Integrated editor** - Crush uses external editor, Pando will have TUI editor
3. **Bubblezone mouse** - Crush does native mouse, Pando will use bubblezone
4. **Changes panel** - Dedicated view to see all agent changes
5. **Visual git status** - Integrated in file tree with icons

## Crush Patterns to Adopt

### 1. Action Pattern
Dialogs return `Action` types instead of generic tea.Msg:
```go
ActionOpenDialog{DialogID: "sessions"}
ActionSelectSession{Session: s}
ActionSelectModel{ModelType: "large"}
ActionNewSession{}
ActionToggleCompactMode{}
ActionRunCustomCommand{Content: "/help"}
```
**Benefit**: Clear decoupling between dialogs and main model.

### 2. Dialog Interface
```go
type Dialog interface {
    HandleMsg(msg tea.Msg) Action
    Render(width int) string
    ShortHelp() []key.Binding
    Cursor() *tea.Cursor
}
```
**Benefit**: All dialogs are interchangeable in the overlay.

### 3. Components Should Be Dumb
- Don't handle `tea.Msg` directly
- Expose methods for state changes
- Return `tea.Cmd` when side effects are needed
- Render via `Render(width int) string`

### 4. Cached Message Items
```go
type cachedMessageItem struct {
    cache    string
    cacheW   int
    isDirty  bool
}
```
**Benefit**: Avoids costly re-rendering of messages that haven't changed.

### 5. List with Lazy Rendering
- Only renders visible items
- Virtual scrolling
- Integrated fuzzy filtering

## Recommended Priority Order

1. **Phase 1** (Keybindings) + **Phase 6** (Diff) → Core functionality
2. **Phase 4** (Markdown) → Immediate visual improvement of chat
3. **Phase 2** (File Explorer) → Project navigation
4. **Phase 3** (Editor) → Editing capability
5. **Phase 5** (Mouse) → Polish and UX

## Dependencies Between Phases

```
Phase 1 (Keybindings) ──→ Phase 2 (File Explorer) ──→ Phase 3 (Editor)
                                     │                      │
                                     └──→ Phase 5 (Mouse) ←──┘
                                      
Phase 4 (Markdown) ──→ Phase 6 (Diff Viewer)
        │                    │
        └──→ Phase 5 (Mouse) ←┘
```

- Phase 1 is prerequisite for all (establishes the keybindings system)
- Phases 2 and 4 can be done in parallel
- Phase 3 depends on Phase 2 (needs sidebar to open files)
- Phase 5 benefits from all previous but can be integrated incrementally
- Phase 6 can start in parallel with Phase 2