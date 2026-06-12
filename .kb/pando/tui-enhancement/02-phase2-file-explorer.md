# Phase 2: File Explorer Side Panel

## Objective
Implement an IDE-style side panel with a directory tree that allows browsing the project, viewing files, and detecting changes, inspired by superfile and crush's architecture.

## References

### Superfile (yorukot/superfile)
- TUI file manager with multiple panels
- Keyboard and mouse navigation
- File preview
- Advanced theming
- Uses bubbletea internally

### Crush - TreeNode (`internal/agent/tools/ls.go`)
```go
type TreeNode struct {
    Name     string      `json:"name"`
    Path     string      `json:"path"`
    Type     NodeType    `json:"type"` // "file" | "directory"
    Children []*TreeNode `json:"children,omitempty"`
}
// createFileTree(sortedPaths, rootPath) - builds tree from paths
// printTree(tree, rootPath) - renders tree as string
```

### Pando - Existing SplitPane (`internal/tui/layout/split.go`)
```go
func NewSplitPane(options ...SplitPaneOption) SplitPaneLayout
// A split pane system already exists that can be reused
```

## Implementation Plan

### 2.1 File Tree Data Model

```go
// internal/tui/components/filetree/node.go
type NodeType int
const (
    NodeTypeFile NodeType = iota
    NodeTypeDirectory
)

type FileNode struct {
    Name        string
    Path        string // path relative to project
    Type        NodeType
    Children    []*FileNode
    IsExpanded  bool
    Depth       int
    IsSelected  bool
    GitStatus   GitFileStatus // untracked, modified, staged, etc.
    IsVisible   bool          // for filtering
}

type GitFileStatus int
const (
    GitStatusClean GitFileStatus = iota
    GitStatusModified
    GitStatusAdded
    GitStatusDeleted
    GitStatusUntracked
    GitStatusRenamed
)
```

### 2.2 FileTree Component

```go
// internal/tui/components/filetree/filetree.go
type FileTree struct {
    root        *FileNode
    flatList    []*FileNode    // flat list of visible nodes
    cursor      int            // current position
    yOffset     int            // scroll offset
    width       int
    height      int
    projectPath string
    
    // Filtering
    filterQuery string
    showHidden  bool
    
    // Git integration
    gitStatuses map[string]GitFileStatus
    
    // Styling
    styles      FileTreeStyles
    
    // Key bindings
    keyMap      FileTreeKeyMap
}

type FileTreeKeyMap struct {
    Up, Down           key.Binding
    Expand, Collapse   key.Binding
    Open, Preview      key.Binding
    Search             key.Binding
    ToggleHidden       key.Binding
    Refresh            key.Binding
}
```

### 2.3 File Tree Functionalities

1. **Navigation**:
   - `j/k` or `↑/↓` - move cursor
   - `l/→` or `Enter` - expand directory / open file
   - `h/←` - collapse directory
   - `gg` - go to beginning
   - `G` - go to end

2. **Filtering**:
   - `/` - activate fuzzy search
   - `.` - toggle hidden files
   - Extension filters

3. **Git Integration**:
   - Status icons: ● modified, + added, - deleted, ? untracked
   - Colors: green (added), yellow (modified), red (deleted), gray (untracked)
   - Indicator in parent directory if it has modified children

4. **Rendering**:
```
 📁 src/
 ├── 📁 internal/
 │   ├── 📄 main.go          ●
 │   ├── 📄 config.go
 │   └── 📁 tui/
 │       ├── 📄 tui.go        ●
 │       └── 📄 keys.go       +
 ├── 📄 go.mod
 └── 📄 README.md
```

### 2.4 Integration with Main Layout

```go
// Modify internal/tui/tui.go
type appModel struct {
    // ... existing ...
    
    // Sidebar
    showSidebar   bool
    sidebarWidth  int  // default 30, adjustable
    fileTree      *filetree.FileTree
    
    // Editor tabs (preparation for Phase 3)
    openFiles     []OpenFile
    activeFileIdx int
}
```

Using existing SplitPane:
```go
func (a *appModel) View() string {
    if a.showSidebar {
        return a.splitPaneView()
    }
    return a.fullView()
}

func (a *appModel) splitPaneView() string {
    // Use layout.NewSplitPane with sidebar + main content
    sidebar := a.fileTree.View()
    main := a.mainContentView()
    // SplitPane with configurable ratio
}
```

### 2.5 Messages and Events

```go
// FileTree messages
type FileSelectedMsg struct {
    Path string
    Type NodeType
}

type FileTreeRefreshMsg struct {
    Root *FileNode
}

type GitStatusUpdateMsg struct {
    Statuses map[string]GitFileStatus
}
```

### 2.6 Data Loading

```go
// internal/tui/components/filetree/loader.go
func LoadFileTree(projectPath string, opts LoadOptions) tea.Cmd {
    return func() tea.Msg {
        // 1. Walk directory respecting .gitignore
        // 2. Build FileNode tree
        // 3. Get git status
        return FileTreeRefreshMsg{Root: root}
    }
}

func LoadGitStatus(projectPath string) tea.Cmd {
    return func() tea.Msg {
        // Execute `git status --porcelain`
        // Parse results
        return GitStatusUpdateMsg{Statuses: statuses}
    }
}

type LoadOptions struct {
    MaxDepth    int
    ShowHidden  bool
    IgnorePatterns []string // .gitignore patterns
}
```

### 2.7 Icons and Styles

```go
// Use Nerd Font or unicode icons
var fileIcons = map[string]string{
    ".go":   "󰟓",  // Go
    ".js":   "",  // JavaScript
    ".ts":   "",  // TypeScript
    ".py":   "",  // Python
    ".md":   "",  // Markdown
    ".json": "",  // JSON
    ".yaml": "",  // YAML
    ".toml": "",  // TOML
    ".sh":   "",  // Shell
    ".sql":  "",  // Database
    ".html": "",  // HTML
    ".css":  "",  // CSS
    "":      "",  // Default file
}

var dirIcons = map[bool]string{
    true:  "",  // Expanded
    false: "",  // Collapsed
}
```

## Files to Create
1. `internal/tui/components/filetree/node.go` - Data model
2. `internal/tui/components/filetree/filetree.go` - Main component
3. `internal/tui/components/filetree/loader.go` - Data loading and git status
4. `internal/tui/components/filetree/styles.go` - Styles and icons
5. `internal/tui/components/filetree/keys.go` - Keybindings

## Files to Modify
1. `internal/tui/tui.go` - Integrate sidebar with toggle
2. `internal/tui/styles/icons.go` - Add file icons

## Dependencies
- None new (uses existing bubbletea + lipgloss)
- Optional: `github.com/go-git/go-git/v5` for native git status (or shell out to `git`)

## Considerations
- **Performance**: Don't load the entire tree at once. Lazy loading per directory.
- **Gitignore**: Respect .gitignore to avoid showing node_modules, .git, etc.
- **Resize**: Sidebar must respond to terminal resize
- **Persistence**: Remember expanded/collapsed state between sessions
