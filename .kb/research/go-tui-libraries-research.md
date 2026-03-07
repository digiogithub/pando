# Research: Librerías y Frameworks TUI en Go para IDE-like TUI

Fecha: 2026-03-07

---

## 1. charmbracelet/bubbles - Componentes TUI para Bubbletea

### Descripción General
Bubbles es la librería oficial de componentes reutilizables para Bubble Tea (framework TUI basado en arquitectura Elm). Todos los componentes implementan el patrón Model-Update-View.

### Componentes Disponibles

#### viewport - Area de contenido scrollable
- **Uso IDE**: Perfecto para paneles de contenido, preview de archivos, output de comandos
- **API clave**:
  - `New(opts ...Option)` - constructor con opciones
  - `SetContent(s string)` / `SetContentLines(lines []string)` - establecer contenido
  - `GetContent()` - recuperar contenido
  - `ScrollUp(n)`, `ScrollDown(n)`, `PageUp()`, `PageDown()`, `HalfPageUp()`, `HalfPageDown()`
  - `GotoTop()`, `GotoBottom()` - saltos extremos
  - `SetYOffset(n)`, `SetXOffset(n)` - posición absoluta
  - `ScrollLeft(n)`, `ScrollRight(n)` - scroll horizontal
  - `AtTop()`, `AtBottom()`, `PastBottom()` - estados de posición
  - `ScrollPercent()`, `HorizontalScrollPercent()` - progreso (0-1)
  - `TotalLineCount()`, `VisibleLineCount()` - conteos
  - `SetHighlights(matches [][]int)` - marca rangos para búsqueda
  - `HighlightNext()`, `HighlightPrevious()` - navegar highlights
  - `EnsureVisible(line, colstart, colend)` - garantiza visibilidad de línea
  - `LeftGutterFunc GutterFunc` - para números de línea o indicadores
  - `StyleLineFunc func(int) lipgloss.Style` - estilo por línea
- **Campos struct**: width, height, yOffset, xOffset, SoftWrap, FillHeight, MouseWheelEnabled, MouseWheelDelta, Style
- **High Performance Mode**: campo YPosition para rendering en alt-screen buffer

#### textinput - Campo de entrada de una línea
- **Uso IDE**: Barras de búsqueda, command palette, input de nombres
- **API**: `New()`, `SetValue(s)`, `Value()`, `Focus()`, `Blur()`, validación, placeholder

#### textarea - Campo de entrada multilínea
- **Uso IDE**: Editor de texto, editing areas
- **API**: `New()`, `SetValue(s)`, `Value()`, `LineCount()`, scroll vertical/horizontal, Unicode

#### table - Datos tabulares
- **Uso IDE**: Listados de archivos, resultados de búsqueda, git status
- **API**: `New(columns, rows)`, `SetHeight(h)`, `SetWidth(w)`, `SelectedRow()`, navegación, selección

#### list - Lista navegable con búsqueda
- **Uso IDE**: File explorer, command palette, búsqueda de archivos
- **Características**: Paginación integrada, fuzzy filtering, ayuda auto-generada, spinner, mensajes de estado
- **API**: `New(items, delegate, width, height)`, `SetItems(items)`, `SelectedItem()`, filtrado

#### filepicker - Selector de archivos
- **Uso IDE**: Abrir archivos, navegar filesystem
- **API**: `New()`, `SelectedFile()`, `AllowedTypes(types)`, navegación por directorios

#### progress - Barra de progreso
- **Uso IDE**: Indicadores de carga, progreso de operaciones
- **API**: `New()`, `SetPercent(float64)`, rellenos sólidos/degradados, animación Harmonica

#### spinner - Indicador de actividad
- **Uso IDE**: Operaciones async, carga de archivos
- **API**: `New()`, múltiples estilos predefinidos, fotogramas personalizables

#### help - Vista de ayuda
- **Uso IDE**: Barra de atajos de teclado
- **API**: `New()`, `View(width)`, modo una línea y multilínea, truncamiento automático

#### key - Gestión de keybindings
- **Uso IDE**: Sistema de atajos de teclado configurable
- **API**: `NewBinding(opts)`, `Matches(msg, bindings)`, remapeo, texto de ayuda contextual

#### paginator - Lógica de paginación
- Estilo dots (iOS) y numeración de páginas

#### timer / stopwatch - Temporizadores
- Cuenta regresiva y progresiva

#### cursor - Gestión de cursor
- Posición y estilo de cursor

#### runeutil - Utilidades de runes
- Procesamiento de Key messages

### Patrones de Composición
- Cada componente es un `Model` con `Update()` y `View()`
- Se embeben como campos de un modelo padre
- El modelo padre delega mensajes al componente correspondiente según el estado de foco
- Se componen visualmente con lipgloss.JoinHorizontal/JoinVertical

---

## 2. yorukot/superfile - File Manager TUI (Referencia de Arquitectura)

### Tecnología
- Go (88%), construido sobre Bubble Tea + Lipgloss
- Binary: `spf`

### Arquitectura del Modelo Principal

```
model (raíz bubbletea)
├── fileModel (gestor de múltiples paneles)
│   ├── FilePanels[] (array de paneles individuales)
│   ├── FocusedPanelIndex (índice del panel activo)
│   ├── SinglePanelWidth (ancho calculado dinámicamente)
│   ├── MaxFilePanel (cantidad máxima de paneles)
│   └── FilePreview (vista previa de contenido)
├── sidebarModel (barra lateral con directorios, renombrado)
├── fileMetaData (metadata via exiftool, con caché)
├── processBarModel (tareas en segundo plano)
├── clipboard (portapapeles)
└── Modales superpuestos:
    ├── helpMenu (overlay centrado)
    ├── promptModal (comandos SPF)
    ├── zoxideModal (navegación rápida)
    ├── sortModal (clasificación)
    ├── notifyModel (confirmaciones)
    ├── typingModal (entrada texto)
    └── warnModel (confirmación)
```

### Sistema de Paneles Múltiples
- Array dinámico `FilePanels[]` con índice de foco `FocusedPanelIndex`
- Cada panel tiene: Location, SearchBar, Rename, PanelMode, IsFocused
- Ancho calculado dinámicamente: `fileModelWidth = fullWidth - sidebarWidth - borde`
- Funciones: `getFocusedFilePanel()`, toggle entre paneles

### Sistema de Foco
- Estados: sidebarFocus, processBarFocus, metadataFocus, nonePanelFocus
- Toggle pattern: activar uno desactiva el anterior
- Propiedad IsFocused en cada panel de archivos

### Navegación
- `parentDirectory()` - subir al directorio padre
- `enterPanel()` - entrar a directorio o abrir archivo
- `sidebarSelectDirectory()` - cambiar directorio desde sidebar
- Integración con Zoxide para navegación rápida

### Rendering (model_render.go)
- Componentes renderizados independientemente: `sidebarRender()`, `processBarRender()`
- Layout compuesto con lipgloss
- Validación de tamaño mínimo de terminal: `terminalSizeWarnRender()`
- Dimensiones: `mainPanelHeight = fullHeight - 2(borde) - footerHeight`
- Modales se superponen sobre interfaz principal

### Estructura de Código
```
src/internal/
├── model.go                    - Modelo principal
├── model_msg.go                - Mensajes
├── model_render.go             - Rendering
├── handle_panel_movement.go    - Movimiento entre paneles
├── handle_panel_navigation.go  - Navegación dentro de paneles
├── handle_file_operations.go   - Operaciones de archivos
├── handle_modal.go             - Gestión de modales
├── file_operations.go          - Operaciones generales
├── file_operations_compress.go - Compresión
├── file_operations_extract.go  - Extracción
├── key_function.go             - Funciones de teclado
├── wheel_function.go           - Funciones de rueda del mouse
├── function.go                 - Funciones principales
├── config_function.go          - Configuración
├── default_config.go           - Config por defecto
├── type.go / type_utils.go     - Tipos
├── validation.go               - Validaciones
├── backend/                    - Lógica de backend
├── common/                     - Código compartido
└── ui/                         - Interfaz de usuario
```

### Lecciones para un IDE-like TUI
1. Separar handlers por responsabilidad (panel_movement, panel_navigation, file_operations)
2. Sistema de foco con estados enum
3. Cálculo dinámico de dimensiones basado en terminal size
4. Modales como overlay sobre la interfaz principal
5. Caché para metadata costosa (exiftool)

---

## 3. charmbracelet/glow - Renderizador Markdown en Terminal

### Arquitectura
- CLI + TUI en Go, usa Bubble Tea para la interfaz interactiva
- Usa **Glamour** internamente para el rendering de markdown

### Modos de Operación
- **TUI**: `glow` sin argumentos - interfaz interactiva para navegar y buscar markdown
- **CLI**: `glow archivo.md` - renderizado directo

### Fuentes de Contenido
- Archivos locales
- Entrada estándar (stdin)
- URLs HTTP/HTTPS
- Repositorios GitHub/GitLab

### Rendering
- Detección automática de estilo (oscuro/claro) según fondo terminal
- Salida ANSI formateada
- Paginación via `less -r`
- Ancho configurable (`-w`)

### Configuración (glow.yml)
- Estilo visual
- Ancho de renderizado
- Soporte de ratón
- Números de línea

### Estructura
```
ui/           - componentes TUI (bubbletea)
utils/        - utilidades
main.go       - entry point
config_cmd.go - configuración
style.go      - estilos
github.go     - integración GitHub
gitlab.go     - integración GitLab
```

### Integración con Bubbletea
- El modelo TUI usa viewport para scroll del markdown renderizado
- Glamour renderiza el markdown a string ANSI
- El string se pasa a viewport.SetContent()
- El usuario navega con teclado/mouse

---

## 4. lrstanley/bubblezone - Mouse Support para Bubbletea

### Problema que Resuelve
Bubbletea provee eventos mouse básicos (MouseButtonLeft, etc.) pero determinar QUÉ componente fue clickeado en interfaces multi-componente es complejo. BubbleZone abstrae esto.

### Concepto Core
Usa **marcadores ANSI de ancho cero** (zero-printable-width) que:
1. Son invisibles y no afectan `lipgloss.Width()`
2. Se insertan alrededor de componentes con `Mark()`
3. Se escanean con `Scan()` para registrar posiciones
4. Se remueven del output final antes de renderizar

### API Completa

#### Inicialización
```go
zone.NewGlobal()           // Manager global (accesible via funciones de paquete)
manager := zone.New()      // Manager local (inyectable)
defer zone.Close()         // Detener workers
```

#### Marcado de Zonas
```go
zone.Mark(id string, content string) string
// Envuelve content con marcadores identificados por id
// Ejemplo: zone.Mark("save-btn", saveButton)
```

#### Escaneo (solo en modelo raíz)
```go
zone.Scan(content string) string
// Escanea todo el view, registra posiciones de zonas, remueve marcadores
// DEBE llamarse SOLO en el View() del modelo raíz
```

#### Consulta de Zonas
```go
info := zone.Get(id string) *ZoneInfo
// Retorna info de la zona (nil si desconocida)

info.InBounds(msg tea.MouseMsg) bool
// Verifica si el evento mouse está dentro de la zona

info.Pos(msg tea.MouseMsg) (x, y int)
// Coordenadas relativas dentro de la zona (0,0 = top-left)
// Retorna (-1,-1) si fuera de bounds

info.IsZero() bool
// True si la zona aún no se conoce
```

#### ZoneInfo Struct
```go
type ZoneInfo struct {
    StartX int  // x top-left (0-based)
    StartY int  // y top-left (0-based)
    EndX   int  // x bottom-right (0-based)
    EndY   int  // y bottom-right (0-based)
}
```

#### Funciones Auxiliares
```go
zone.AnyInBounds(model, mouse)                           // Envía MsgZoneInBounds por cada zona en bounds
zone.AnyInBoundsAndUpdate(model, mouse) (Model, Cmd)     // Igual pero retorna model/cmd actualizados
zone.NewPrefix() string                                   // Prefijo único para evitar colisiones de IDs
zone.SetEnabled(bool) / zone.Enabled() bool               // Habilitar/deshabilitar
zone.Clear(id string)                                     // Limpiar datos de zona
```

### Patrón de Uso Completo

```go
// main.go
func main() {
    zone.NewGlobal()
    defer zone.Close()
    p := tea.NewProgram(model{}, tea.WithAltScreen(), tea.WithMouseCellMotion())
    p.Run()
}

// View() - modelo RAÍZ
func (m model) View() string {
    buttons := lipgloss.JoinHorizontal(lipgloss.Top,
        zone.Mark("confirm", okButton),
        zone.Mark("cancel", cancelButton),
    )
    return zone.Scan(m.style.Render(buttons))  // Scan solo aquí
}

// Update()
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        if msg.Button == tea.MouseButtonLeft {
            if zone.Get("confirm").InBounds(msg) {
                m.active = "confirm"
            }
        }
    }
    return m, nil
}
```

### Para Componentes Anidados (evitar colisiones de ID)
```go
type childModel struct {
    id string  // prefijo único
}

func NewChild() childModel {
    return childModel{id: zone.NewPrefix()}
}

func (c childModel) View() string {
    return zone.Mark(c.id+"item-1", item1)  // ID prefijado
}
```

### Mejores Prácticas
1. `Scan()` SOLO en el modelo raíz
2. Usar `lipgloss.Width()` (no `len()`) - marcadores son transparentes para Width
3. Evitar `MaxHeight/MaxWidth` de lipgloss (truncan y rompen zonas)
4. Los bounds son rectangulares (bounding box)
5. Para listas: marcar cada item con ID único
6. `NewPrefix()` para componentes reutilizables

### Requisitos
- Alt-screen habilitado: `tea.WithAltScreen()`
- Mouse tracking: `tea.WithMouseCellMotion()`

---

## 5. charmbracelet/glamour - Rendering de Markdown en Terminal

### Descripción
Librería Go para renderizar Markdown con estilos en terminales ANSI. "Stylesheet-based markdown rendering for CLI apps."

### API Principal

#### Rendering Simple
```go
out, err := glamour.Render(in, "dark")                    // Con estilo específico
out, err := glamour.RenderWithEnvironmentConfig(in)        // Usa GLAMOUR_STYLE env
outBytes, err := glamour.RenderBytes(inBytes, "dark")      // Versión bytes
```

#### TermRenderer (personalizable)
```go
r, err := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),          // Detecta fondo claro/oscuro
    glamour.WithWordWrap(80),         // Ancho de wrapping
    glamour.WithEmoji(),              // Renderizar emojis
)
out, err := r.Render(markdownString)

// También implementa io.ReadWriteCloser
r.Write(bytes)
r.Read(buf)
r.Close()
```

#### Opciones Disponibles (TermRendererOption)
```go
glamour.WithAutoStyle()                    // Detecta tema automáticamente
glamour.WithStandardStyle("dark")          // Estilo estándar
glamour.WithStylePath("/path/style.json")  // Estilo desde archivo
glamour.WithEnvironmentConfig()            // Desde env GLAMOUR_STYLE
glamour.WithStyles(ansi.StyleConfig{})     // Estilos directos
glamour.WithStylesFromJSONFile("f.json")   // Desde JSON
glamour.WithStylesFromJSONBytes(json)      // Desde JSON bytes
glamour.WithWordWrap(80)                   // Ancho wrapping
glamour.WithBaseURL("https://...")         // URL base para links relativos
glamour.WithColorProfile(termenv.TrueColor) // Perfil de color
glamour.WithEmoji()                        // Emojis
glamour.WithPreservedNewLines()            // Preservar saltos
glamour.WithTableWrap(true)               // Wrap en tablas
glamour.WithInlineTableLinks(true)        // Links inline en tablas
glamour.WithChromaFormatter("terminal256") // Formatter de Chroma
glamour.WithOptions(opt1, opt2...)        // Agrupar opciones
```

### Estilos Predefinidos
- `"dark"` - Tema oscuro (default)
- `"light"` - Tema claro
- `"auto"` - Detecta automáticamente
- `"notty"` - Para salida sin terminal
- `"dracula"` - Esquema Dracula

### Integración con Chroma
- Glamour usa Chroma internamente para syntax highlighting en bloques de código
- Configurable con `WithChromaFormatter()`: "terminal256", "terminal16m", etc.
- Automático para bloques ``` con lenguaje especificado

### Personalización de Estilos
- Via `ansi.StyleConfig` struct
- Via archivos JSON
- Via variable de entorno `GLAMOUR_STYLE`

### Integración con Bubbletea (patrón típico)
```go
// En Init() o Update():
renderer, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
renderedMarkdown, _ := renderer.Render(rawMarkdown)
m.viewport.SetContent(renderedMarkdown)

// En View():
return m.viewport.View()
```

---

## 6. alecthomas/chroma - Syntax Highlighting en Go

### Descripción
Librería Go pura para syntax highlighting. Convierte código fuente en HTML coloreado, texto ANSI para terminal, etc. Inspirada en Pygments.

### Arquitectura: 3 Componentes
1. **Lexers** - Convierten texto en flujos de tokens (200+ lenguajes)
2. **Formatters** - Transforman tokens en salida formateada
3. **Styles** - Mapean tipos de token a colores/estilos

### Lenguajes Soportados (200+)
Go, Python, Java, JavaScript, TypeScript, Rust, C/C++, C#, Ruby, PHP, Kotlin, SQL, HTML, XML, JSON, YAML, Markdown, Docker, Terraform, Bash, PowerShell, GraphQL, Haskell, y muchos más.

### Formatters de Terminal (relevantes para TUI)
- `"terminal16"` - 8/16 colores ANSI
- `"terminal256"` - 256 colores ANSI
- `"terminal16m"` - True color (24-bit RGB)
- También: HTML, noop, tokens (debugging)

### Uso Rápido
```go
import "github.com/alecthomas/chroma/v2/quick"

err := quick.Highlight(os.Stdout, sourceCode, "go", "terminal256", "monokai")
```

### Uso Programático Detallado
```go
import (
    "github.com/alecthomas/chroma/v2"
    "github.com/alecthomas/chroma/v2/lexers"
    "github.com/alecthomas/chroma/v2/formatters"
    "github.com/alecthomas/chroma/v2/styles"
)

// 1. Identificar lenguaje
lexer := lexers.Match("foo.go")          // Por filename
lexer := lexers.Get("go")                // Por nombre
lexer := lexers.Analyse(sourceCode)      // Por contenido
if lexer == nil { lexer = lexers.Fallback }

// 2. Optimizar (combinar tokens idénticos adyacentes)
lexer = chroma.Coalesce(lexer)

// 3. Seleccionar estilo
style := styles.Get("monokai")
if style == nil { style = styles.Fallback }

// 4. Seleccionar formatter
formatter := formatters.Get("terminal256")
if formatter == nil { formatter = formatters.Fallback }

// 5. Tokenizar
iterator, err := lexer.Tokenise(nil, sourceCode)

// 6. Formatear a un writer (puede ser bytes.Buffer)
var buf bytes.Buffer
err = formatter.Format(&buf, style, iterator)
highlightedCode := buf.String()
```

### Estilos/Temas Populares
- `"monokai"` - Oscuro, popular
- `"github"` - Claro
- `"dracula"` - Oscuro
- Todos los estilos de Pygments convertidos
- Case-insensitive

### Integración con TUI (patrón para IDE)
```go
// Para preview de archivos con syntax highlighting:
func highlightFile(content, filename string, width int) string {
    lexer := lexers.Match(filename)
    if lexer == nil { lexer = lexers.Fallback }
    lexer = chroma.Coalesce(lexer)
    
    style := styles.Get("dracula")
    formatter := formatters.Get("terminal256")
    
    iterator, _ := lexer.Tokenise(nil, content)
    var buf bytes.Buffer
    formatter.Format(&buf, style, iterator)
    return buf.String()
}

// Luego en bubbletea:
highlighted := highlightFile(fileContent, "main.go", m.width)
m.viewport.SetContent(highlighted)
```

### Características Adicionales
- Detección automática de lenguaje por filename, extensión o contenido
- Jerarquía de tokens: si CommentSpecial no está definido, hereda de Comment
- `chroma.Coalesce()` mejora rendimiento combinando tokens adyacentes
- Integración con less: `LESSOPEN` para preview colorizado

---

## Resumen de Integración para IDE-like TUI

### Stack Recomendado
```
bubbletea          → Framework base (Model-Update-View)
lipgloss           → Estilos y layout (borders, colors, join)
bubbles/viewport   → Paneles de contenido scrollable
bubbles/textinput  → Command palette, search bar
bubbles/textarea   → Editor de texto
bubbles/list       → File explorer, fuzzy search
bubbles/table      → Git status, resultados
bubbles/help       → Barra de atajos
bubbles/key        → Sistema de keybindings
bubblezone         → Mouse support con zonas clickeables
glamour            → Rendering de markdown (README, docs)
chroma             → Syntax highlighting de código
```

### Patrón de Composición Principal
```go
type IDEModel struct {
    // Layout
    sidebar     SidebarModel      // File tree (bubbles/list)
    editor      EditorModel       // Editor principal (bubbles/textarea o viewport+chroma)
    preview     viewport.Model    // Preview panel (viewport)
    terminal    viewport.Model    // Terminal output
    commandBar  textinput.Model   // Command palette
    statusBar   StatusBarModel    // Status bar
    help        help.Model        // Help bar
    
    // State
    focusedPanel FocusState       // Qué panel tiene foco
    keys         KeyMap           // Keybindings (bubbles/key)
    width, height int             // Terminal dimensions
}

func (m IDEModel) View() string {
    left := m.sidebar.View()
    center := m.editor.View()
    right := m.preview.View()
    
    main := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
    bottom := m.statusBar.View()
    
    full := lipgloss.JoinVertical(lipgloss.Left, main, bottom)
    return zone.Scan(full)  // Mouse support
}
```

### Lecciones de Superfile
1. **Handlers separados**: handle_panel_movement.go, handle_panel_navigation.go
2. **Foco como enum**: sidebarFocus, editorFocus, previewFocus, etc.
3. **Dimensiones dinámicas**: Recalcular en WindowSizeMsg
4. **Modales como overlay**: Renderizar encima del contenido principal
5. **Caché**: Para operaciones costosas (metadata, highlighting)
