# Fase 2: Panel Lateral File Explorer

## Objetivo
Implementar un panel lateral estilo IDE con árbol de directorios que permita navegar el proyecto, ver archivos y detectar cambios, inspirado en superfile y la arquitectura de crush.

## Referencias

### Superfile (yorukot/superfile)
- File manager TUI con múltiples paneles
- Navegación con teclado y mouse
- Preview de archivos
- Theming avanzado
- Usa bubbletea internamente

### Crush - TreeNode (`internal/agent/tools/ls.go`)
```go
type TreeNode struct {
    Name     string      `json:"name"`
    Path     string      `json:"path"`
    Type     NodeType    `json:"type"` // "file" | "directory"
    Children []*TreeNode `json:"children,omitempty"`
}
// createFileTree(sortedPaths, rootPath) - construye árbol desde paths
// printTree(tree, rootPath) - renderiza árbol como string
```

### Pando - SplitPane Existente (`internal/tui/layout/split.go`)
```go
func NewSplitPane(options ...SplitPaneOption) SplitPaneLayout
// Ya existe un sistema de split pane que se puede reutilizar
```

## Plan de Implementación

### 2.1 Modelo de Datos del File Tree

```go
// internal/tui/components/filetree/node.go
type NodeType int
const (
    NodeTypeFile NodeType = iota
    NodeTypeDirectory
)

type FileNode struct {
    Name        string
    Path        string // path relativo al proyecto
    Type        NodeType
    Children    []*FileNode
    IsExpanded  bool
    Depth       int
    IsSelected  bool
    GitStatus   GitFileStatus // untracked, modified, staged, etc.
    IsVisible   bool          // para filtrado
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

### 2.2 Componente FileTree

```go
// internal/tui/components/filetree/filetree.go
type FileTree struct {
    root        *FileNode
    flatList    []*FileNode    // lista plana de nodos visibles
    cursor      int            // posición actual
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

### 2.3 Funcionalidades del File Tree

1. **Navegación**:
   - `j/k` o `↑/↓` - mover cursor
   - `l/→` o `Enter` - expandir directorio / abrir archivo
   - `h/←` - colapsar directorio
   - `gg` - ir al inicio
   - `G` - ir al final

2. **Filtrado**:
   - `/` - activar búsqueda fuzzy
   - `.` - toggle archivos ocultos
   - Filtros por extensión

3. **Git Integration**:
   - Iconos de estado: ● modified, + added, - deleted, ? untracked
   - Colores: verde (added), amarillo (modified), rojo (deleted), gris (untracked)
   - Indicador en el directorio padre si tiene hijos modificados

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

### 2.4 Integración con Layout Principal

```go
// Modificar internal/tui/tui.go
type appModel struct {
    // ... existente ...
    
    // Sidebar
    showSidebar   bool
    sidebarWidth  int  // default 30, ajustable
    fileTree      *filetree.FileTree
    
    // Editor tabs (preparación para Fase 3)
    openFiles     []OpenFile
    activeFileIdx int
}
```

Uso del SplitPane existente:
```go
func (a *appModel) View() string {
    if a.showSidebar {
        return a.splitPaneView()
    }
    return a.fullView()
}

func (a *appModel) splitPaneView() string {
    // Usar layout.NewSplitPane con sidebar + main content
    sidebar := a.fileTree.View()
    main := a.mainContentView()
    // SplitPane con ratio configurable
}
```

### 2.5 Mensajes y Eventos

```go
// Mensajes del FileTree
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

### 2.6 Carga de Datos

```go
// internal/tui/components/filetree/loader.go
func LoadFileTree(projectPath string, opts LoadOptions) tea.Cmd {
    return func() tea.Msg {
        // 1. Walk del directorio respetando .gitignore
        // 2. Construir árbol de FileNodes
        // 3. Obtener git status
        return FileTreeRefreshMsg{Root: root}
    }
}

func LoadGitStatus(projectPath string) tea.Cmd {
    return func() tea.Msg {
        // Ejecutar `git status --porcelain`
        // Parsear resultados
        return GitStatusUpdateMsg{Statuses: statuses}
    }
}

type LoadOptions struct {
    MaxDepth    int
    ShowHidden  bool
    IgnorePatterns []string // .gitignore patterns
}
```

### 2.7 Iconos y Estilos

```go
// Usar iconos Nerd Font o unicode
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

## Archivos a Crear
1. `internal/tui/components/filetree/node.go` - Modelo de datos
2. `internal/tui/components/filetree/filetree.go` - Componente principal
3. `internal/tui/components/filetree/loader.go` - Carga de datos y git status
4. `internal/tui/components/filetree/styles.go` - Estilos y iconos
5. `internal/tui/components/filetree/keys.go` - Keybindings

## Archivos a Modificar
1. `internal/tui/tui.go` - Integrar sidebar con toggle
2. `internal/tui/styles/icons.go` - Añadir iconos de archivos

## Dependencias
- Ninguna nueva (usa bubbletea + lipgloss existentes)
- Opcional: `github.com/go-git/go-git/v5` para git status nativo (o shell out a `git`)

## Consideraciones
- **Performance**: No cargar todo el árbol de golpe. Lazy loading por directorio.
- **Gitignore**: Respetar .gitignore para no mostrar node_modules, .git, etc.
- **Resize**: El sidebar debe responder al resize del terminal
- **Persistencia**: Recordar estado expandido/colapsado entre sesiones
