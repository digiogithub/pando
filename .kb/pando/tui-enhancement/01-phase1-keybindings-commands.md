# Phase 1: Keybinding System and Command Palette

## Objective
Implement a hierarchical keybinding system inspired by crush and an improved command palette that provides quick access to all of Pando's features.

## Current State in Pando

### Current Keybindings (`internal/tui/tui.go`)
```go
type keyMap struct {
    Logs          key.Binding  // "l" - switch to logs page
    Quit          key.Binding  // "q" - quit
    SwitchSession key.Binding  // "s" - switch session
    Filepicker    key.Binding  // ctrl+f - open filepicker
}
// Also: helpEsc ("?"), returnKey ("esc")
```

### Current Command Dialogs
- `CommandDialog` in `internal/tui/components/dialog/` - basic
- `CompletionDialog` in `internal/tui/components/dialog/complete.go` - for completions

## How Crush Does It

### Hierarchical KeyMap (`internal/ui/model/keys.go`)
```go
type KeyMap struct {
    Editor struct {
        AddFile, SendMessage, OpenEditor, Newline, AddImage key.Binding
        PasteImage, MentionFile, Commands key.Binding
        AttachmentDeleteMode, Escape, DeleteAllAttachments key.Binding
        HistoryPrev, HistoryNext key.Binding
    }
    Chat struct {
        NewSession, AddAttachment, Cancel, Tab, Details key.Binding
        TogglePills, PillLeft, PillRight key.Binding
        Down, Up, UpDown, DownOneItem, UpOneItem, UpDownOneItem key.Binding
        PageDown, PageUp, HalfPageDown, HalfPageUp key.Binding
        Home, End, Copy, ClearHighlight, Expand key.Binding
    }
    Initialize struct {
        Yes, No, Enter, Switch key.Binding
    }
    // Global
    Quit, Help, Commands, Models, Suspend, Sessions, Tab key.Binding
}
```

### Overlay System for Dialogs (`internal/ui/dialog/dialog.go`)
```go
type Overlay struct {
    dialogs []Dialog  // Dialog stack
}
// Methods: CloseFrontDialog, DialogLast, Update, StopLoading
// The most recent dialog (last in stack) receives events
```

### Action Pattern (`internal/ui/dialog/actions.go`)
```go
type ActionOpenDialog struct { DialogID string }
type ActionSelectSession struct { Session session.Session }
type ActionSelectModel struct {
    ModelType config.SelectedModelType
    ReAuthenticate bool
}
type ActionNewSession struct{}
type ActionToggleCompactMode struct{}
type ActionToggleThinking struct{}
type ActionRunCustomCommand struct {
    Content string
    Arguments []commands.Argument
    Args map[string]string
}
```

### Completions (Command Palette) (`internal/ui/completions/completions.go`)
```go
type Completions struct {
    width, height int
    open bool
    query string
    keyMap KeyMap
    list *list.FilterableList
    normalStyle, focusedStyle, matchStyle lipgloss.Style
}
// Open() loads files and MCP resources in parallel
// Update() handles Up/Down/Select/Cancel
// SetItems() configures files and resources as filterable items
```

## Implementation Plan

### 1.1 Expand KeyMap
**File**: `internal/tui/tui.go` (or create `internal/tui/keys.go`)

```go
type KeyMap struct {
    // Editor context
    Editor struct {
        Send        key.Binding // enter (send message)
        Newline     key.Binding // shift+enter
        OpenEditor  key.Binding // ctrl+e (external editor)
        AddFile     key.Binding // ctrl+a (attach file)
        MentionFile key.Binding // @ (mention file)
        Commands    key.Binding // / (slash commands)
        HistoryPrev key.Binding // up (history)
        HistoryNext key.Binding // down
        Escape      key.Binding // esc
    }
    
    // Chat/viewport context
    Chat struct {
        ScrollUp     key.Binding // k, up
        ScrollDown   key.Binding // j, down
        PageUp       key.Binding // ctrl+u, pgup
        PageDown     key.Binding // ctrl+d, pgdn
        Home         key.Binding // g, home
        End          key.Binding // G, end
        Copy         key.Binding // y (copy selection)
        Expand       key.Binding // enter (expand item)
        Details      key.Binding // d (view details)
    }
    
    // File explorer context
    FileExplorer struct {
        Open      key.Binding // enter
        Preview   key.Binding // space
        Expand    key.Binding // right, l
        Collapse  key.Binding // left, h
        Up        key.Binding // k, up
        Down      key.Binding // j, down
        Search    key.Binding // /
    }
    
    // Global shortcuts
    Quit          key.Binding // ctrl+c, q
    Help          key.Binding // ?
    Commands      key.Binding // ctrl+p (command palette)
    Models        key.Binding // ctrl+m (switch model)
    Sessions      key.Binding // ctrl+s (sessions)
    Logs          key.Binding // ctrl+l (logs)
    ToggleSidebar key.Binding // ctrl+b (sidebar)
    ToggleTheme   key.Binding // ctrl+t (theme)
    NewSession    key.Binding // ctrl+n (new session)
    Filepicker    key.Binding // ctrl+f (filepicker)
}
```

### 1.2 Improved Command Palette
**Create/Modify**: `internal/tui/components/dialog/command_palette.go`

Required functionality:
- Filterable list of all available commands
- Categories: General, Sessions, Models, Files, View
- Each command shows: name, description, shortcut
- Fuzzy matching for search
- Direct execution of selected command

```go
type CommandCategory string
const (
    CategoryGeneral  CommandCategory = "General"
    CategorySession  CommandCategory = "Sessions"
    CategoryModel    CommandCategory = "Models"
    CategoryFile     CommandCategory = "Files"
    CategoryView     CommandCategory = "View"
)

type Command struct {
    ID          string
    Name        string
    Description string
    Shortcut    string
    Category    CommandCategory
    Action      func() tea.Cmd
}
```

### 1.3 Quick Access Shortcuts
Implement handlers in the main model's `Update`:

| Shortcut | Action | Context |
|----------|--------|---------|
| `ctrl+p` | Command palette | Global |
| `ctrl+m` | Model selector | Global |
| `ctrl+s` | Sessions list | Global |
| `ctrl+n` | New session | Global |
| `ctrl+b` | Toggle sidebar | Global |
| `ctrl+l` | Logs page | Global |
| `ctrl+f` | Filepicker | Global |
| `ctrl+t` | Change theme | Global |
| `?` | Help overlay | Global |
| `/` | Slash commands | Editor |
| `@` | Mention file | Editor |
| `esc` | Close dialog/go back | Global |

### 1.4 Improved Help Overlay
Show all available shortcuts organized by context:
- Global shortcuts
- Editor shortcuts
- Chat navigation
- File explorer (when implemented)

## Files to Create/Modify
1. `internal/tui/keys.go` - New hierarchical KeyMap
2. `internal/tui/components/dialog/command_palette.go` - Improved command palette
3. `internal/tui/components/dialog/help.go` - Improved help overlay
4. `internal/tui/tui.go` - Integrate new keybinding system

## Dependencies
- None new (uses existing bubbles key.Binding)

## Complexity Estimation
- KeyMap: Low (refactoring existing structure)
- Command Palette: Medium (new component but similar pattern to existing)
- Help Overlay: Low (formatted list rendering)
- Integration: Medium (event routing in main model)
