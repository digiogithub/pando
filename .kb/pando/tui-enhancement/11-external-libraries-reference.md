# Referencia de Librerías Externas para Pando TUI

## 1. Bubbles Components Útiles

### viewport (YA USADO en Pando)
- `SetContent()`, `ScrollUp/Down()`, `PageUp/Down()`
- **`LeftGutterFunc`** → Perfecto para números de línea en el editor
- **`StyleLineFunc`** → Estilo por línea (current line highlight, search matches)
- **`SetHighlights()`** + `HighlightNext/Previous` → Para búsqueda
- Mouse wheel support nativo
- High performance mode para alt-screen

### list (CONSIDERAR para File Tree)
- Lista navegable con fuzzy filtering integrado
- Paginación, spinner, mensajes de estado
- Podría usarse como base para el file tree o command palette

### key/help (YA USADO en Pando)
- Remapeo de keybindings
- Vista de ayuda auto-generada

## 2. Bubblezone - Detalles de Implementación

### Mecanismo Interno
- Usa marcadores ANSI de **ancho cero** (invisibles, no afectan `lipgloss.Width()`)
- `Scan()` registra posiciones y remueve marcadores
- SOLO llamar Scan() en el modelo raíz

### API Esencial
```go
zone.NewGlobal()                         // Init global
zone.Mark("id", content)                 // Marcar zona
zone.Scan(view)                          // Escanear (SOLO en raíz)
zone.Get("id").InBounds(mouseMsg)        // Check click
zone.Get("id").Pos(mouseMsg)             // Coordenadas relativas
zone.NewPrefix()                         // Prefijo único para componentes reutilizables
zone.AnyInBoundsAndUpdate()              // Batch process
```

### Requisitos
```go
tea.WithAltScreen()
tea.WithMouseCellMotion()
```

### Mejores Prácticas
- Scan SOLO en modelo raíz
- Usar `lipgloss.Width()` (no `len()`)
- Evitar MaxHeight/MaxWidth de lipgloss (rompe bounds)
- `NewPrefix()` para componentes reutilizados en la misma vista

## 3. Chroma - Syntax Highlighting

### Uso Rápido
```go
quick.Highlight(writer, code, "go", "terminal256", "monokai")
```

### Uso Programático (como crush)
```go
lexer := lexers.Match("main.go")        // Por filename
if lexer == nil { lexer = lexers.Analyse(code) } // Por contenido
lexer = chroma.Coalesce(lexer)           // Optimizar tokens
style := styles.Get("monokai")
formatter := formatters.Get("terminal16m")  // True color 24-bit
iterator, _ := lexer.Tokenise(nil, code)
var buf bytes.Buffer
formatter.Format(&buf, style, iterator)
result := buf.String()
```

### Formatters de Terminal
- `"terminal16"` → 8/16 colores ANSI (compatible con todo)
- `"terminal256"` → 256 colores (la mayoría de terminales)
- `"terminal16m"` → True color 24-bit (terminales modernos)

### Integración con Temas de Pando
Los 9 temas de Pando ya definen 8 colores de syntax:
- SyntaxComment, SyntaxKeyword, SyntaxFunction, SyntaxString
- SyntaxNumber, SyntaxOperator, SyntaxType, SyntaxVariable

Se puede crear un `chroma.Style` custom que mapee estos colores:
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

## 4. Glamour - Ya en Pando

### Configuración Avanzada
```go
renderer, _ := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),            // Auto dark/light
    glamour.WithWordWrap(width),        // Word wrap
    glamour.WithEmoji(),                // Soporte emoji
    glamour.WithStyles(customStyle),    // Estilos custom
    glamour.WithChromaFormatter("terminal16m"), // True color code blocks
)
```

### Integración con Chroma
Glamour usa chroma internamente para code blocks. Se puede configurar el formatter:
- `WithChromaFormatter("terminal16m")` para mejor calidad

## 5. Superfile - Patrones de Arquitectura

### Panel System
```go
type fileModel struct {
    FilePanels      []FilePanel
    FocusedPanelIndex int
}
```
- Array dinámico de paneles
- Ancho calculado: `fileModelWidth = fullWidth - sidebarWidth - borders`
- Focus como enum con toggle pattern

### Estructura de Handlers (separación de responsabilidades)
```
handle_panel_movement.go     # Movimiento entre paneles
handle_panel_navigation.go   # Navegación dentro de panel
handle_file_operations.go    # Copy, move, delete, rename
handle_modal.go              # Gestión de modales
```

### Aplicabilidad a Pando
- El patrón de `FilePanels[]` con `FocusedPanelIndex` es elegante
- Podría usarse para el sistema de paneles: Sidebar, Editor, Chat
- El toggle pattern de focus es limpio

## 6. Integración Recomendada en Pando

### Para el File Tree
- Usar `bubbles/list` como base con items custom
- O implementar custom con viewport (más control)
- Viewport `LeftGutterFunc` para indent indicators

### Para el Editor/Viewer  
- Usar `bubbles/viewport` con:
  - `LeftGutterFunc` → números de línea
  - `StyleLineFunc` → current line, search matches
  - `SetHighlights` → búsqueda
- Chroma para syntax highlighting del contenido

### Para Mouse
- Bubblezone ya importado en go.mod
- Añadir `zone.Mark()` a cada componente interactivo
- `zone.Scan()` en el View() final de appModel
- `zone.NewPrefix()` para file tree items (muchos con mismo patrón)

### Para Markdown
- Glamour ya importado
- Configurar `WithChromaFormatter("terminal16m")` para code blocks
- Mapear colores MarkdownText/MarkdownHeading/etc del tema
