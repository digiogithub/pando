# Phase 4: Improved Markdown Rendering (Glow Style)

## Objective
Improve AI response display with high-quality markdown rendering using glamour, with code blocks with syntax highlighting, formatted tables, and attractive visual styling.

## References

### Glow (charmbracelet/glow)
- TUI for rendering markdown in terminal
- Uses glamour internally for rendering
- Complete GFM (GitHub Flavored Markdown) support
- Customizable themes (dark/light)
- Pagination with viewport

### Glamour (charmbracelet/glamour)
- Go library for rendering markdown to ANSI text
- Based on goldmark (parser) + chroma (code highlighting)
- Configurable styles via JSON or programmatically
- Support: headers, lists, tables, code blocks, blockquotes, links, images (placeholder)

### Crush - MarkdownRenderer (`internal/ui/common/markdown.go`)
```go
func MarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer {
    r, _ := glamour.NewTermRenderer(
        glamour.WithStyles(sty.Markdown),  // custom styles
        glamour.WithWordWrap(width),        // word wrap to viewport width
    )
    return r
}
```

### Crush - Chat Message Rendering
- Assistant messages are rendered with markdown
- Code blocks have syntax highlighting via chroma
- A cache system is used to avoid re-rendering

## Implementation Plan

### 4.1 Glamour Configuration

```go
// internal/tui/components/chat/markdown.go
import "github.com/charmbracelet/glamour"

func NewMarkdownRenderer(theme *theme.Theme, width int) *glamour.TermRenderer {
    // Create styles based on current theme
    mdStyle := createMarkdownStyle(theme)
    
    r, _ := glamour.NewTermRenderer(
        glamour.WithStyles(mdStyle),
        glamour.WithWordWrap(width),
        glamour.WithEmoji(),  // Emoji support
    )
    return r
}

func createMarkdownStyle(t *theme.Theme) glamour.TermRendererOption {
    // Map theme colors to glamour styles
    return glamour.WithStyles(ansi.StyleConfig{
        Document: ansi.StyleBlock{
            Margin: uintPtr(0),
        },
        Heading: ansi.StyleBlock{
            StylePrimitive: ansi.StylePrimitive{
                Bold:  boolPtr(true),
                Color: stringPtr(t.Primary),
            },
        },
        H1: ansi.StyleBlock{
            StylePrimitive: ansi.StylePrimitive{
                Prefix: "# ",
                Bold:   boolPtr(true),
                Color:  stringPtr(t.Accent),
            },
        },
        // ... more styles
        CodeBlock: ansi.StyleCodeBlock{
            Theme:   t.ChromaTheme, // chroma theme for code blocks
            Chroma:  &ansi.Chroma{},
            Margin:  uintPtr(1),
        },
        Code: ansi.StyleBlock{
            StylePrimitive: ansi.StylePrimitive{
                Color:           stringPtr(t.CodeFg),
                BackgroundColor: stringPtr(t.CodeBg),
            },
        },
        Table: ansi.StyleTable{
            StyleBlock: ansi.StyleBlock{
                StylePrimitive: ansi.StylePrimitive{},
            },
            CenterSeparator: stringPtr("┼"),
            ColumnSeparator: stringPtr("│"),
            RowSeparator:    stringPtr("─"),
        },
        // Blockquotes
        BlockQuote: ansi.StyleBlock{
            Indent:      uintPtr(1),
            IndentToken: stringPtr("│ "),
            StylePrimitive: ansi.StylePrimitive{
                Color:  stringPtr(t.Muted),
                Italic: boolPtr(true),
            },
        },
        // Links
        Link: ansi.StylePrimitive{
            Color:     stringPtr(t.Link),
            Underline: boolPtr(true),
        },
        // Lists
        List: ansi.StyleList{
            LevelIndent: 2,
        },
        // Task lists
        Task: ansi.StyleTask{
            Ticked:   "[✓] ",
            Unticked: "[ ] ",
        },
    })
}
```

### 4.2 Rendering Cache

```go
// internal/tui/components/chat/render_cache.go
type RenderCache struct {
    mu      sync.RWMutex
    entries map[string]CacheEntry
    maxSize int
}

type CacheEntry struct {
    rendered  string
    width     int       // width at which it was rendered
    timestamp time.Time
}

func (c *RenderCache) Get(content string, width int) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    entry, ok := c.entries[hashKey(content)]
    if !ok || entry.width != width {
        return "", false
    }
    return entry.rendered, true
}

func (c *RenderCache) Set(content string, width int, rendered string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    // Evict if maxSize exceeded
    c.entries[hashKey(content)] = CacheEntry{
        rendered:  rendered,
        width:     width,
        timestamp: time.Now(),
    }
}
```

### 4.3 AI Message Rendering

```go
// Modify internal/tui/components/chat/message.go
func (m *uiMessage) renderAssistantMessage(width int) string {
    // 1. Try cache
    if cached, ok := m.cache.Get(m.content, width); ok {
        return cached
    }
    
    // 2. Render markdown
    renderer := NewMarkdownRenderer(m.theme, width)
    rendered, err := renderer.Render(m.content)
    if err != nil {
        // Fallback: plain text
        rendered = m.content
    }
    
    // 3. Clean trailing whitespace
    rendered = strings.TrimRight(rendered, "\n")
    
    // 4. Cache
    m.cache.Set(m.content, width, rendered)
    
    return rendered
}
```

### 4.4 Streaming with Partial Markdown

Problem: The AI sends tokens incrementally. We need to render partial markdown.

```go
// internal/tui/components/chat/streaming.go
type StreamingRenderer struct {
    buffer     strings.Builder
    lastRender string
    renderer   *glamour.TermRenderer
    width      int
    
    // To avoid re-rendering on each token
    debounceTimer *time.Timer
    minInterval   time.Duration // e.g: 50ms
}

func (s *StreamingRenderer) AppendToken(token string) {
    s.buffer.WriteString(token)
    // Debounce: don't re-render on each individual token
}

func (s *StreamingRenderer) Render() string {
    content := s.buffer.String()
    
    // If content ends in the middle of a code block, temporarily close it
    if isInCodeBlock(content) {
        content += "\n```"
    }
    
    rendered, err := s.renderer.Render(content)
    if err != nil {
        return content // fallback
    }
    
    s.lastRender = rendered
    return rendered
}
```

### 4.5 Improved Visual Elements

```
┌─ Assistant ──────────────────────────────────────┐
│                                                    │
│  I have modified the file `main.go`:               │
│                                                    │
│  ```go                                             │
│  func main() {                                     │
│      fmt.Println("Hello, World!")                   │
│  }                                                 │
│  ```                                               │
│                                                    │
│  │ Note: This change requires Go 1.21+             │
│                                                    │
│  Changes made:                                     │
│  • ✓ Updated main.go                               │
│  • ✓ Added test                                    │
│  • Pending: documentation                          │
│                                                    │
│  | Column 1 | Column 2 | Column 3 |               │
│  |-----------|-----------|-----------|              │
│  | value     | data      | info      |             │
│                                                    │
└────────────────────────────────────────────────────┘
```

## Files to Create
1. `internal/tui/components/chat/markdown.go` - Markdown renderer
2. `internal/tui/components/chat/render_cache.go` - Rendering cache
3. `internal/tui/components/chat/streaming.go` - Streaming markdown renderer

## Files to Modify
1. `internal/tui/components/chat/message.go` - Use new renderer
2. `internal/tui/components/chat/list.go` - Integrate cache
3. `internal/tui/theme/theme.go` - Add markdown colors

## Dependencies
```
github.com/charmbracelet/glamour  # Should already be there, if not add
```

## Considerations
- **Performance**: Markdown rendering is expensive. Aggressive caching is essential.
- **Streaming**: Debounce re-rendering during streaming to avoid flicker.
- **Code blocks**: Must have syntax highlighting with chroma (included in glamour).
- **Width responsive**: Re-render when terminal width changes.
- **Themes**: Markdown styles must follow Pando's global theme.
- **Clickable links**: Prepare for bubblezone integration (Phase 5).