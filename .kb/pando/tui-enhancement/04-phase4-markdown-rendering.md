# Fase 4: Markdown Rendering Mejorado (Estilo Glow)

## Objetivo
Mejorar la visualización de respuestas AI con rendering de markdown de alta calidad usando glamour, con code blocks con syntax highlighting, tablas formateadas, y estilo visual atractivo.

## Referencias

### Glow (charmbracelet/glow)
- TUI para renderizar markdown en terminal
- Usa glamour internamente para el rendering
- Soporte completo de GFM (GitHub Flavored Markdown)
- Temas personalizables (dark/light)
- Paginación con viewport

### Glamour (charmbracelet/glamour)
- Librería Go para rendering de markdown a texto ANSI
- Basada en goldmark (parser) + chroma (code highlighting)
- Estilos configurables vía JSON o programáticamente
- Soporte: headers, listas, tablas, code blocks, blockquotes, links, imágenes (placeholder)

### Crush - MarkdownRenderer (`internal/ui/common/markdown.go`)
```go
func MarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer {
    r, _ := glamour.NewTermRenderer(
        glamour.WithStyles(sty.Markdown),  // estilos custom
        glamour.WithWordWrap(width),        // word wrap al ancho del viewport
    )
    return r
}
```

### Crush - Chat Message Rendering
- Los mensajes del asistente se renderizan con markdown
- Code blocks tienen syntax highlighting via chroma
- Se usa un sistema de cache para evitar re-rendering

## Plan de Implementación

### 4.1 Configuración de Glamour

```go
// internal/tui/components/chat/markdown.go
import "github.com/charmbracelet/glamour"

func NewMarkdownRenderer(theme *theme.Theme, width int) *glamour.TermRenderer {
    // Crear estilos basados en el tema actual
    mdStyle := createMarkdownStyle(theme)
    
    r, _ := glamour.NewTermRenderer(
        glamour.WithStyles(mdStyle),
        glamour.WithWordWrap(width),
        glamour.WithEmoji(),  // Soporte de emoji
    )
    return r
}

func createMarkdownStyle(t *theme.Theme) glamour.TermRendererOption {
    // Mapear colores del tema a estilos de glamour
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
        // ... más estilos
        CodeBlock: ansi.StyleCodeBlock{
            Theme:   t.ChromaTheme, // Tema de chroma para code blocks
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

### 4.2 Cache de Rendering

```go
// internal/tui/components/chat/render_cache.go
type RenderCache struct {
    mu      sync.RWMutex
    entries map[string]CacheEntry
    maxSize int
}

type CacheEntry struct {
    rendered  string
    width     int       // ancho al que se renderizó
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
    // Evict si se supera maxSize
    c.entries[hashKey(content)] = CacheEntry{
        rendered:  rendered,
        width:     width,
        timestamp: time.Now(),
    }
}
```

### 4.3 Rendering de Mensajes AI

```go
// Modificar internal/tui/components/chat/message.go
func (m *uiMessage) renderAssistantMessage(width int) string {
    // 1. Intentar cache
    if cached, ok := m.cache.Get(m.content, width); ok {
        return cached
    }
    
    // 2. Renderizar markdown
    renderer := NewMarkdownRenderer(m.theme, width)
    rendered, err := renderer.Render(m.content)
    if err != nil {
        // Fallback: texto plano
        rendered = m.content
    }
    
    // 3. Limpiar trailing whitespace
    rendered = strings.TrimRight(rendered, "\n")
    
    // 4. Cachear
    m.cache.Set(m.content, width, rendered)
    
    return rendered
}
```

### 4.4 Streaming con Markdown Parcial

Problema: El AI envía tokens incrementalmente. Necesitamos renderizar markdown parcial.

```go
// internal/tui/components/chat/streaming.go
type StreamingRenderer struct {
    buffer     strings.Builder
    lastRender string
    renderer   *glamour.TermRenderer
    width      int
    
    // Para evitar re-render en cada token
    debounceTimer *time.Timer
    minInterval   time.Duration // ej: 50ms
}

func (s *StreamingRenderer) AppendToken(token string) {
    s.buffer.WriteString(token)
    // Debounce: no re-renderizar en cada token individual
}

func (s *StreamingRenderer) Render() string {
    content := s.buffer.String()
    
    // Si el contenido termina en medio de un code block, cerrar temporalmente
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

### 4.5 Elementos Visuales Mejorados

```
┌─ Asistente ──────────────────────────────────────┐
│                                                    │
│  He modificado el archivo `main.go`:               │
│                                                    │
│  ```go                                             │
│  func main() {                                     │
│      fmt.Println("Hello, World!")                   │
│  }                                                 │
│  ```                                               │
│                                                    │
│  │ Nota: Este cambio requiere Go 1.21+             │
│                                                    │
│  Cambios realizados:                               │
│  • ✓ Actualizado main.go                           │
│  • ✓ Añadido test                                  │
│  • Pendiente: documentación                        │
│                                                    │
│  | Columna 1 | Columna 2 | Columna 3 |            │
│  |-----------|-----------|-----------|            │
│  | valor     | dato      | info      |            │
│                                                    │
└────────────────────────────────────────────────────┘
```

## Archivos a Crear
1. `internal/tui/components/chat/markdown.go` - Renderer de markdown
2. `internal/tui/components/chat/render_cache.go` - Cache de rendering
3. `internal/tui/components/chat/streaming.go` - Streaming markdown renderer

## Archivos a Modificar
1. `internal/tui/components/chat/message.go` - Usar nuevo renderer
2. `internal/tui/components/chat/list.go` - Integrar cache
3. `internal/tui/theme/theme.go` - Añadir colores de markdown

## Dependencias
```
github.com/charmbracelet/glamour  # Ya debería estar, si no añadir
```

## Consideraciones
- **Performance**: El rendering de markdown es costoso. Cache agresivo es fundamental.
- **Streaming**: Debounce el re-rendering durante streaming para evitar flicker.
- **Code blocks**: Deben tener syntax highlighting con chroma (viene incluido en glamour).
- **Width responsive**: Re-renderizar cuando cambia el ancho del terminal.
- **Temas**: Los estilos de markdown deben seguir el tema global de Pando.
- **Links clickeables**: Preparar para integración con bubblezone (Fase 5).
