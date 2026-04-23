# Fase 3: Editor TUI con Syntax Highlighting

## Objetivo
Implementar un viewer/editor de archivos integrado en la TUI con syntax highlighting usando chroma, sistema de tabs para múltiples archivos, y búsqueda integrada.

## Referencias de Crush

### SyntaxHighlight (`internal/ui/common/highlight.go`)
```go
func SyntaxHighlight(st *styles.Styles, source, fileName string, bg color.Color) (string, error) {
    // 1. Determinar lexer por nombre de archivo o análisis de contenido
    l := lexers.Match(fileName)
    if l == nil { l = lexers.Analyse(source) }
    if l == nil { l = lexers.Fallback }
    l = chroma.Coalesce(l)
    
    // 2. Obtener formatter terminal 16M colores
    f := formatters.Get("terminal16m")
    
    // 3. Crear estilo chroma con background personalizado
    style := chroma.MustNewStyle("crush", st.ChromaTheme())
    
    // 4. Tokenizar y formatear
    it, _ := l.Tokenise(nil, source)
    var buf bytes.Buffer
    f.Format(&buf, s, it)
    return buf.String(), nil
}
```

### DiffView usa chroma con cache (`internal/ui/diffview/diffview.go`)
```go
// Cache lexer para evitar matching caro en cada línea
cachedLexer chroma.Lexer
// Cache de líneas highlighted
syntaxCache map[string]string
```

## Plan de Implementación

### 3.1 Componente FileViewer

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
    yOffset     int      // scroll vertical
    xOffset     int      // scroll horizontal
    
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
    
    // Selection (para copiar)
    selectionStart Position
    selectionEnd   Position
    hasSelection   bool
    
    // State
    isReadOnly  bool
    isDirty     bool  // tiene cambios sin guardar
    
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
    scrollOff  int  // cuando hay muchos tabs
    styles     TabStyles
}

type Tab struct {
    FilePath  string
    FileName  string  // basename para display
    IsDirty   bool
    FileType  string  // extensión para icono
}

func (t *TabBar) Render() string {
    // Renderizar tabs horizontalmente
    // Tab activo resaltado
    // Indicador de dirty (●)
    // Icono de tipo de archivo
    // [󰟓 main.go] [● 󰌛 style.css] [ config.yaml]
}
```

### 3.3 Keybindings del Editor

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
    Copy        key.Binding  // y (copiar selección)
    Close       key.Binding  // ctrl+w (cerrar tab)
    NextTab     key.Binding  // ctrl+tab, gt
    PrevTab     key.Binding  // ctrl+shift+tab, gT
    Save        key.Binding  // ctrl+s (si editable)
    
    // View
    ToggleLineNumbers  key.Binding  // ctrl+l
    ToggleWordWrap     key.Binding  // alt+z
}
```

### 3.4 Rendering del Viewer

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
    // Usar cache
    if cached, ok := v.highlightedLines[lineIdx]; ok {
        return cached
    }
    // Highlight y cachear
    highlighted := highlightLine(v.lexer, v.chromaStyle, v.lines[lineIdx])
    v.highlightedLines[lineIdx] = highlighted
    return highlighted
}
```

### 3.5 Integración de Syntax Highlighting

```go
// internal/tui/components/editor/highlight.go
func NewHighlighter(filePath string) *Highlighter {
    lexer := lexers.Match(filePath)
    if lexer == nil {
        lexer = lexers.Fallback
    }
    lexer = chroma.Coalesce(lexer)
    
    style := getChromaStyle() // basado en tema actual
    
    return &Highlighter{
        lexer:   lexer,
        style:   style,
        cache:   make(map[int]string),
    }
}

// Highlight por línea individual (más eficiente para scroll)
func (h *Highlighter) HighlightLine(line string) string {
    it, _ := h.lexer.Tokenise(nil, line)
    var buf bytes.Buffer
    h.formatter.Format(&buf, h.style, it)
    return buf.String()
}

// Highlight de bloque (para carga inicial)
func (h *Highlighter) HighlightBlock(content string) []string {
    // Tokenizar todo el contenido de una vez (mejor precisión)
    // Luego dividir por líneas
}
```

### 3.6 Integración con el Layout Principal

```go
// En appModel
type appModel struct {
    // ... existente ...
    
    // Editor
    editorTabs    *editor.TabBar
    activeEditor  *editor.FileViewer
    showEditor    bool
    
    // Layout modes
    layoutMode    LayoutMode
}

type LayoutMode int
const (
    LayoutChat       LayoutMode = iota  // Solo chat
    LayoutSidebar                        // Sidebar + Chat  
    LayoutEditor                         // Sidebar + Editor
    LayoutSplit                          // Sidebar + Editor + Chat (3 paneles)
)
```

### 3.7 Apertura de Archivos

```go
// Cuando se selecciona un archivo del FileTree
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

// En Update
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

## Archivos a Crear
1. `internal/tui/components/editor/viewer.go` - Viewer principal
2. `internal/tui/components/editor/tabs.go` - Sistema de tabs
3. `internal/tui/components/editor/highlight.go` - Syntax highlighting
4. `internal/tui/components/editor/keys.go` - Keybindings
5. `internal/tui/components/editor/styles.go` - Estilos
6. `internal/tui/components/editor/search.go` - Búsqueda inline

## Archivos a Modificar
1. `internal/tui/tui.go` - Integrar editor y layout modes
2. `internal/tui/components/filetree/filetree.go` - Conectar apertura de archivos

## Dependencias Nuevas
```
github.com/alecthomas/chroma/v2          # Syntax highlighting
github.com/alecthomas/chroma/v2/lexers   # Lexers por lenguaje
github.com/alecthomas/chroma/v2/styles   # Temas de colores
```

## Consideraciones
- **Read-only por defecto**: El editor empieza como viewer. La edición se puede añadir después.
- **Cache de highlighting**: Cachear por línea, invalidar al editar
- **Archivos grandes**: Lazy highlighting solo de líneas visibles
- **Encoding**: Detectar encoding del archivo (UTF-8, Latin-1, etc.)
- **Binary files**: Detectar y mostrar mensaje "Binary file, cannot display"
- **Tab width**: Configurable (default 4)
