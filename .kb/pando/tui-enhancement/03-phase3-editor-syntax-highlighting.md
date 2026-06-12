# Phase 3: TUI Editor with Syntax Highlighting

## Objective
Implement an integrated file viewer/editor in the TUI with syntax highlighting using chroma, a tab system for multiple files, and integrated search.

## Crush References

### SyntaxHighlight (`internal/ui/common/highlight.go`)
```go
func SyntaxHighlight(st *styles.Styles, source, fileName string, bg color.Color) (string, error) {
    // 1. Determine lexer by filename or content analysis
    l := lexers.Match(fileName)
    if l == nil { l = lexers.Analyse(source) }
    if l == nil { l = lexers.Fallback }
    l = chroma.Coalesce(l)
    
    // 2. Get terminal 16M colors formatter
    f := formatters.Get("terminal16m")
    
    // 3. Create chroma style with custom background
    style := chroma.MustNewStyle("crush", st.ChromaTheme())
    
    // 4. Tokenize and format
    it, _ := l.Tokenise(nil, source)
    var buf bytes.Buffer
    f.Format(&buf, s, it)
    return buf.String(), nil
}
```

### DiffView uses chroma with cache (`internal/ui/diffview/diffview.go`)
```go
// Cache lexer to avoid expensive matching on each line
cachedLexer chroma.Lexer
// Cache of highlighted lines
syntaxCache map[string]string
```

## Implementation Plan

### 3.1 FileViewer Component

```go
// internal/tui/components/editor/viewer.go
type FileViewer struct {
    // Content
    filePath    string
    content     string
    lines       []string
    totalLines  int
    
    // Viewport
    width       int
    height      int
    yOffset     int      // vertical scroll
    xOffset     int      // horizontal scroll
    
    // Line numbers
    showLineNumbers bool
    gutterWidth     int
    
    // Syntax highlighting
    highlightedLines []string  // cached highlighted lines
    lexer           chroma.Lexer
    chromaStyle     *chroma.Style
    
    // Cursor
    cursorLine  int
    cursorCol   int
    
    // Search
    searchQuery  string
    searchActive bool
    searchMatches []SearchMatch
    currentMatch  int
    
    // Selection (for copying)
    selectionStart Position
    selectionEnd   Position
    hasSelection   bool
    
    // State
    isReadOnly  bool
    isDirty     bool  // has unsaved changes
    
    // Styling
    styles      EditorStyles
    keyMap      EditorKeyMap
}

type Position struct {
    Line, Col int
}

type SearchMatch struct {
    Line, StartCol, EndCol int
}
```

### 3.2 Tab System

```go
// internal/tui/components/editor/tabs.go
type TabBar struct {
    tabs       []Tab
    activeIdx  int
    width      int
    scrollOff  int  // when there are many tabs
    styles     TabStyles
}

type Tab struct {
    FilePath  string
    FileName  string  // basename for display
    IsDirty   bool
    FileType  string  // extension for icon
}

func (t *TabBar) Render() string {
    // Render tabs horizontally
    // Active tab highlighted
    // Dirty indicator (●)
    // File type icon
    // [󰟓 main.go] [● 󰌛 style.css] [ config.yaml]
}
```

### 3.3 Editor Keybindings

```go
type EditorKeyMap struct {
    // Navigation
    Up, Down, Left, Right     key.Binding
    PageUp, PageDown          key.Binding
    Home, End                 key.Binding
    GoToLine                  key.Binding  // ctrl+g
    
    // Search
    Search      key.Binding  // ctrl+f or /
    SearchNext  key.Binding  // n
    SearchPrev  key.Binding  // N
    
    // Actions
    Copy        key.Binding  // y (copy selection)
    Close       key.Binding  // ctrl+w (close tab)
    NextTab     key.Binding  // ctrl+tab, gt
    PrevTab     key.Binding  // ctrl+shift+tab, gT
    Save        key.Binding  // ctrl+s (if editable)
    
    // View
    ToggleLineNumbers  key.Binding  // ctrl+l
    ToggleWordWrap     key.Binding  // alt+z
}
```

### 3.4 Viewer Rendering

```go
func (v *FileViewer) View() string {
    var sb strings.Builder
    
    visibleStart := v.yOffset
    visibleEnd := min(v.yOffset + v.height, v.totalLines)
    
    for i := visibleStart; i < visibleEnd; i++ {
        line := ""
        
        // 1. Gutter (line number)
        if v.showLineNumbers {
            lineNum := fmt.Sprintf("%*d", v.gutterWidth, i+1)
            if i == v.cursorLine {
                line += activeLineNumStyle.Render(lineNum) + " "
            } else {
                line += lineNumStyle.Render(lineNum) + " "
            }
        }
        
        // 2. Code content (highlighted)
        codeLine := v.getHighlightedLine(i)
        
        // 3. Search highlighting (overlay)
        codeLine = v.applySearchHighlight(codeLine, i)
        
        // 4. Cursor line highlight
        if i == v.cursorLine {
            codeLine = cursorLineStyle.Render(codeLine)
        }
        
        // 5. Horizontal scroll
        codeLine = horizontalSlice(codeLine, v.xOffset, v.width - v.gutterWidth)
        
        line += codeLine
        sb.WriteString(line + "\n")
    }
    
    return sb.String()
}

func (v *FileViewer) getHighlightedLine(lineIdx int) string {
    // Use cache
    if cached, ok := v.highlightedLines[lineIdx]; ok {
        return cached
    }
    // Highlight and cache
    highlighted := highlightLine(v.lexer, v.chromaStyle, v.lines[lineIdx])
    v.highlightedLines[lineIdx] = highlighted
    return highlighted
}
```

### 3.5 Syntax Highlighting Integration

```go
// internal/tui/components/editor/highlight.go
func NewHighlighter(filePath string) *Highlighter {
    lexer := lexers.Match(filePath)
    if lexer == nil {
        lexer = lexers.Fallback
    }
    lexer = chroma.Coalesce(lexer)
    
    style := getChromaStyle() // based on current theme
    
    return &Highlighter{
        lexer:   lexer,
        style:   style,
        cache:   make(map[int]string),
    }
}

// Highlight individual line (more efficient for scrolling)
func (h *Highlighter) HighlightLine(line string) string {
    it, _ := h.lexer.Tokenise(nil, line)
    var buf bytes.Buffer
    h.formatter.Format(&buf, h.style, it)
    return buf.String()
}

// Highlight block (for initial load)
func (h *Highlighter) HighlightBlock(content string) []string {
    // Tokenize all content at once (better accuracy)
    // Then split by lines
}
```

### 3.6 Integration with Main Layout

```go
// In appModel
type appModel struct {
    // ... existing ...
    
    // Editor
    editorTabs    *editor.TabBar
    activeEditor  *editor.FileViewer
    showEditor    bool
    
    // Layout modes
    layoutMode    LayoutMode
}

type LayoutMode int
const (
    LayoutChat       LayoutMode = iota  // Chat only
    LayoutSidebar                        // Sidebar + Chat  
    LayoutEditor                         // Sidebar + Editor
    LayoutSplit                          // Sidebar + Editor + Chat (3 panels)
)
```

### 3.7 File Opening

```go
// When a file is selected from the FileTree
func (a *appModel) openFile(path string) tea.Cmd {
    return func() tea.Msg {
        content, err := os.ReadFile(path)
        if err != nil {
            return util.NewErrorMsg(err)
        }
        return FileOpenedMsg{
            Path:    path,
            Content: string(content),
        }
    }
}

// In Update
case FileOpenedMsg:
    tab := editor.Tab{
        FilePath: msg.Path,
        FileName: filepath.Base(msg.Path),
    }
    a.editorTabs.AddTab(tab)
    a.activeEditor = editor.NewFileViewer(msg.Path, msg.Content)
    a.showEditor = true
    a.layoutMode = LayoutEditor
```

## Files to Create
1. `internal/tui/components/editor/viewer.go` - Main viewer
2. `internal/tui/components/editor/tabs.go` - Tab system
3. `internal/tui/components/editor/highlight.go` - Syntax highlighting
4. `internal/tui/components/editor/keys.go` - Keybindings
5. `internal/tui/components/editor/styles.go` - Styles
6. `internal/tui/components/editor/search.go` - Inline search

## Files to Modify
1. `internal/tui/tui.go` - Integrate editor and layout modes
2. `internal/tui/components/filetree/filetree.go` - Connect file opening

## New Dependencies
```
github.com/alecthomas/chroma/v2          # Syntax highlighting
github.com/alecthomas/chroma/v2/lexers   # Lexers per language
github.com/alecthomas/chroma/v2/styles   # Color themes
```

## Considerations
- **Read-only by default**: The editor starts as a viewer. Editing can be added later.
- **Highlighting cache**: Cache per line, invalidate on edit
- **Large files**: Lazy highlighting of visible lines only
- **Encoding**: Detect file encoding (UTF-8, Latin-1, etc.)
- **Binary files**: Detect and show "Binary file, cannot display" message
- **Tab width**: Configurable (default 4)
