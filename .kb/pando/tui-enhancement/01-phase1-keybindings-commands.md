# Fase 1: Sistema de Keybindings y Command Palette

## Objetivo
Implementar un sistema de keybindings jerárquico inspirado en crush y un command palette mejorado que permita acceso rápido a todas las funcionalidades de Pando.

## Estado Actual en Pando

### Keybindings Actuales (`internal/tui/tui.go`)
```go
type keyMap struct {
    Logs          key.Binding  // "l" - cambiar a página de logs
    Quit          key.Binding  // "q" - salir
    SwitchSession key.Binding  // "s" - cambiar sesión
    Filepicker    key.Binding  // ctrl+f - abrir filepicker
}
// También: helpEsc ("?"), returnKey ("esc")
```

### Diálogos de Comando Actuales
- `CommandDialog` en `internal/tui/components/dialog/` - básico
- `CompletionDialog` en `internal/tui/components/dialog/complete.go` - para completions

## Cómo lo Hace Crush

### KeyMap Jerárquico (`internal/ui/model/keys.go`)
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

### Sistema de Overlay para Diálogos (`internal/ui/dialog/dialog.go`)
```go
type Overlay struct {
    dialogs []Dialog  // Stack de diálogos
}
// Métodos: CloseFrontDialog, DialogLast, Update, StopLoading
// El diálogo más reciente (último del stack) recibe los eventos
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
// Open() carga files y MCP resources en paralelo
// Update() maneja Up/Down/Select/Cancel
// SetItems() configura files y resources como items filtrables
```

## Plan de Implementación

### 1.1 Expandir KeyMap
**Archivo**: `internal/tui/tui.go` (o crear `internal/tui/keys.go`)

```go
type KeyMap struct {
    // Editor context
    Editor struct {
        Send        key.Binding // enter (send message)
        Newline     key.Binding // shift+enter
        OpenEditor  key.Binding // ctrl+e (editor externo)
        AddFile     key.Binding // ctrl+a (adjuntar archivo)
        MentionFile key.Binding // @ (mencionar archivo)
        Commands    key.Binding // / (slash commands)
        HistoryPrev key.Binding // up (historial)
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
        Copy         key.Binding // y (copiar selección)
        Expand       key.Binding // enter (expandir item)
        Details      key.Binding // d (ver detalles)
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
    Models        key.Binding // ctrl+m (cambiar modelo)
    Sessions      key.Binding // ctrl+s (sesiones)
    Logs          key.Binding // ctrl+l (logs)
    ToggleSidebar key.Binding // ctrl+b (panel lateral)
    ToggleTheme   key.Binding // ctrl+t (tema)
    NewSession    key.Binding // ctrl+n (nueva sesión)
    Filepicker    key.Binding // ctrl+f (filepicker)
}
```

### 1.2 Command Palette Mejorado
**Crear/Modificar**: `internal/tui/components/dialog/command_palette.go`

Funcionalidad requerida:
- Lista filtrable de todos los comandos disponibles
- Categorías: General, Sessions, Models, Files, View
- Cada comando muestra: nombre, descripción, shortcut
- Fuzzy matching para búsqueda
- Ejecución directa del comando seleccionado

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

### 1.3 Shortcuts de Acceso Rápido
Implementar handlers en el `Update` del modelo principal:

| Shortcut | Acción | Contexto |
|----------|--------|----------|
| `ctrl+p` | Command palette | Global |
| `ctrl+m` | Selector de modelo | Global |
| `ctrl+s` | Lista de sesiones | Global |
| `ctrl+n` | Nueva sesión | Global |
| `ctrl+b` | Toggle sidebar | Global |
| `ctrl+l` | Logs page | Global |
| `ctrl+f` | Filepicker | Global |
| `ctrl+t` | Cambiar tema | Global |
| `?` | Help overlay | Global |
| `/` | Slash commands | Editor |
| `@` | Mencionar archivo | Editor |
| `esc` | Cerrar diálogo/volver | Global |

### 1.4 Help Overlay Mejorado
Mostrar todos los shortcuts disponibles organizados por contexto:
- Global shortcuts
- Editor shortcuts
- Chat navigation
- File explorer (cuando esté implementado)

## Archivos a Crear/Modificar
1. `internal/tui/keys.go` - Nuevo KeyMap jerárquico
2. `internal/tui/components/dialog/command_palette.go` - Command palette mejorado
3. `internal/tui/components/dialog/help.go` - Help overlay mejorado
4. `internal/tui/tui.go` - Integrar nuevo sistema de keybindings

## Dependencias
- Ninguna nueva (usa bubbles key.Binding existente)

## Estimación de Complejidad
- KeyMap: Baja (refactoring de estructura existente)
- Command Palette: Media (nuevo componente pero patrón similar al existente)
- Help Overlay: Baja (rendering de lista formateada)
- Integración: Media (routing de eventos en modelo principal)
