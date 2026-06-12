# External Libraries Reference for Pando TUI

## 1. Useful Bubbles Components

### viewport (ALREADY USED in Pando)
- `SetContent()`, `ScrollUp/Down()`, `PageUp/Down()`
- **`LeftGutterFunc`** → Perfect for line numbers in the editor
- **`StyleLineFunc`** → Per-line styling (current line highlight, search matches)
- **`SetHighlights()`** + `HighlightNext/Previous` → For search
- Native mouse wheel support
- High performance mode for alt-screen

### list (CONSIDER for File Tree)
- Navigable list with integrated fuzzy filtering
- Pagination, spinner, status messages
- Could be used as a base for the file tree or command palette

### key/help (ALREADY USED in Pando)
- Keybinding remapping
- Auto-generated help view

## 2. Bubblezone - Implementation Details

### Internal Mechanism
- Uses **zero-width** ANSI markers (invisible, don't affect `lipgloss.Width()`)
- `Scan()` registers positions and removes markers
- ONLY call Scan() in the root model

### Essential API
```go
zone.NewGlobal()                         // Init global
zone.Mark("id", content)                 // Mark zone
zone.Scan(view)                          // Scan (ROOT ONLY)
zone.Get("id").InBounds(mouseMsg)        // Check click
zone.Get("id").Pos(mouseMsg)             // Relative coordinates
zone.NewPrefix()                         // Unique prefix for reusable components
zone.AnyInBoundsAndUpdate()              // Batch process
```

### Requirements
```go
tea.WithAltScreen()
tea.WithMouseCellMotion()
```

### Best Practices
- Scan ONLY in root model
- Use `lipgloss.Width()` (not `len()`)
- Avoid MaxHeight/MaxWidth from lipgloss (breaks bounds)
- `NewPrefix()` for components reused in the same view

## 3. Chroma - Syntax Highlighting

### Quick Usage
```go
quick.Highlight(writer, code, "go", "terminal256", "monokai")
```

### Programmatic Usage (like crush)
```go
lexer := lexers.Match("main.go")        // By filename
if lexer == nil { lexer = lexers.Analyse(code) } // By content
lexer = chroma.Coalesce(lexer)           // Optimize tokens
style := styles.Get("monokai")
formatter := formatters.Get("terminal16m")  // True color 24-bit
iterator, _ := lexer.Tokenise(nil, code)
var buf bytes.Buffer
formatter.Format(&buf, style, iterator)
result := buf.String()
```

### Terminal Formatters
- `"terminal16"` → 8/16 ANSI colors (compatible with everything)
- `"terminal256"` → 256 colors (most terminals)
- `"terminal16m"` → True color 24-bit (modern terminals)

### Integration with Pando Themes
Pando's 9 themes already define 8 syntax colors:
- SyntaxComment, SyntaxKeyword, SyntaxFunction, SyntaxString
- SyntaxNumber, SyntaxOperator, SyntaxType, SyntaxVariable

A custom `chroma.Style` can be created to map these colors:
```go
func ThemeToChromaStyle(t theme.Theme) *chroma.Style {
    return chroma.MustNewStyle("pando", chroma.StyleEntries{
        chroma.Comment:     chroma.StyleEntry{Colour: toChromaColor(t.SyntaxComment())},
        chroma.Keyword:     chroma.StyleEntry{Colour: toChromaColor(t.SyntaxKeyword())},
        chroma.NameFunction: chroma.StyleEntry{Colour: toChromaColor(t.SyntaxFunction())},
        chroma.LiteralString: chroma.StyleEntry{Colour: toChromaColor(t.SyntaxString())},
        // ... etc
    })
}
```

## 4. Glamour - Already in Pando

### Advanced Configuration
```go
renderer, _ := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),            // Auto dark/light
    glamour.WithWordWrap(width),        // Word wrap
    glamour.WithEmoji(),                // Emoji support
    glamour.WithStyles(customStyle),    // Custom styles
    glamour.WithChromaFormatter("terminal16m"), // True color code blocks
)
```

### Chroma Integration
Glamour uses chroma internally for code blocks. The formatter can be configured:
- `WithChromaFormatter("terminal16m")` for better quality

## 5. Superfile - Architecture Patterns

### Panel System
```go
type fileModel struct {
    FilePanels      []FilePanel
    FocusedPanelIndex int
}
```
- Dynamic panel array
- Width calculation: `fileModelWidth = fullWidth - sidebarWidth - borders`
- Focus as enum with toggle pattern

### Handler Structure (separation of concerns)
```
handle_panel_movement.go     # Movement between panels
handle_panel_navigation.go   # Navigation within a panel
handle_file_operations.go    # Copy, move, delete, rename
handle_modal.go              # Modal management
```

### Applicability to Pando
- The `FilePanels[]` with `FocusedPanelIndex` pattern is elegant
- Could be used for the panel system: Sidebar, Editor, Chat
- The focus toggle pattern is clean

## 6. Recommended Integration in Pando

### For the File Tree
- Use `bubbles/list` as a base with custom items
- Or implement custom with viewport (more control)
- Viewport `LeftGutterFunc` for indent indicators

### For the Editor/Viewer
- Use `bubbles/viewport` with:
  - `LeftGutterFunc` → line numbers
  - `StyleLineFunc` → current line, search matches
  - `SetHighlights` → search
- Chroma for syntax highlighting of content

### For Mouse
- Bubblezone already imported in go.mod
- Add `zone.Mark()` to each interactive component
- `zone.Scan()` in the final View() of appModel
- `zone.NewPrefix()` for file tree items (many sharing the same pattern)

### For Markdown
- Glamour already imported
- Configure `WithChromaFormatter("terminal16m")` for code blocks
- Map MarkdownText/MarkdownHeading/etc colors from the theme
