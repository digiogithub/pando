# Fase 5: Mouse Support con Bubblezone

## Objetivo
Integrar bubblezone para hacer todos los paneles y elementos clickeables con el mouse, incluyendo el sidebar, tabs del editor, elementos del chat, botones de diálogos, y scroll con wheel.

## Referencia: Bubblezone (lrstanley/bubblezone)

### Concepto
Bubblezone añade zonas con mouse tracking sobre componentes bubbletea existentes. Funciona envolviendo el output de `View()` con marcadores de zona, y luego interceptando eventos de mouse para determinar qué zona fue clickeada.

### Uso Básico
```go
import zone "github.com/lrstanley/bubblezone"

// 1. Crear manager global
var z = zone.New()

// 2. En Init(), habilitar mouse
func (m model) Init() tea.Cmd {
    return tea.EnableMouseAllMotion  // o tea.EnableMouseCellMotion
}

// 3. En View(), marcar zonas
func (m model) View() string {
    // Envolver cada elemento clickeable con zone.Mark()
    button1 := z.Mark("btn-save", "[ Save ]")
    button2 := z.Mark("btn-cancel", "[ Cancel ]")
    
    // Escanear el output final
    return z.Scan(lipgloss.JoinHorizontal(lipgloss.Top, button1, button2))
}

// 4. En Update(), detectar clicks
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        if msg.Action == tea.MouseActionPress {
            if z.Get("btn-save").InBounds(msg) {
                return m, saveCmd()
            }
            if z.Get("btn-cancel").InBounds(msg) {
                return m, cancelCmd()
            }
        }
    }
    return m, nil
}
```

### Referencia: Crush Mouse Support
Crush implementa mouse support nativo sin bubblezone, directamente en `model/chat.go`:
```go
// Campos del Chat struct
mouseDownY, mouseDragItem, mouseDragY int
lastClickTime time.Time
lastClickY, clickCount int
pendingClickID int

// Métodos
HandleMouseDown(x, y int) (bool, tea.Cmd)
HandleMouseUp(x, y int) bool
HandleMouseDrag(x, y int) bool
HandleDelayedClick(msg DelayedClickMsg) bool
selectWord(itemIdx, x, itemY int)
selectLine(itemIdx, itemY int)
```

## Plan de Implementación

### 5.1 Setup Global de Bubblezone

```go
// internal/tui/zone/zone.go
package zone

import zone "github.com/lrstanley/bubblezone"

// Manager global
var Manager = zone.New()

// Zone IDs
const (
    // Sidebar
    ZoneSidebarFile    = "sidebar-file-"    // + path hash
    ZoneSidebarDir     = "sidebar-dir-"     // + path hash
    ZoneSidebarToggle  = "sidebar-toggle"
    
    // Editor tabs
    ZoneEditorTab      = "editor-tab-"      // + tab index
    ZoneEditorTabClose = "editor-tab-close-" // + tab index
    
    // Chat
    ZoneChatMessage    = "chat-msg-"         // + message id
    ZoneChatCodeBlock  = "chat-code-"        // + block id
    ZoneChatLink       = "chat-link-"        // + link id
    ZoneChatFile       = "chat-file-"        // + file path hash
    
    // Status bar
    ZoneStatusModel    = "status-model"
    ZoneStatusSession  = "status-session"
    ZoneStatusBranch   = "status-branch"
    
    // Dialogs
    ZoneDialogButton   = "dialog-btn-"       // + button id
    ZoneDialogItem     = "dialog-item-"      // + item index
)

// Helper: generar zone ID único
func FileZoneID(prefix, path string) string {
    return prefix + hashPath(path)
}
```

### 5.2 Integración en Modelo Principal

```go
// internal/tui/tui.go
func (a *appModel) Init() tea.Cmd {
    return tea.Batch(
        // ... otros cmds ...
        tea.EnableMouseCellMotion,  // Habilitar mouse
    )
}

func (a *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        return a.handleMouse(msg)
    // ...
    }
}

func (a *appModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    switch msg.Action {
    case tea.MouseActionPress:
        switch msg.Button {
        case tea.MouseButtonLeft:
            return a.handleLeftClick(msg)
        case tea.MouseButtonWheelUp:
            return a.handleScrollUp(msg)
        case tea.MouseButtonWheelDown:
            return a.handleScrollDown(msg)
        }
    case tea.MouseActionMotion:
        return a.handleMouseDrag(msg)
    case tea.MouseActionRelease:
        return a.handleMouseRelease(msg)
    }
    return a, nil
}
```

### 5.3 Mouse en Sidebar (FileTree)

```go
// En filetree View()
func (ft *FileTree) View() string {
    var lines []string
    for _, node := range ft.visibleNodes() {
        line := ft.renderNode(node)
        
        // Hacer clickeable
        zoneID := zone.FileZoneID(zone.ZoneSidebarFile, node.Path)
        if node.Type == NodeTypeDirectory {
            zoneID = zone.FileZoneID(zone.ZoneSidebarDir, node.Path)
        }
        line = zone.Manager.Mark(zoneID, line)
        
        lines = append(lines, line)
    }
    return strings.Join(lines, "\n")
}

// En handleLeftClick
func (a *appModel) handleSidebarClick(msg tea.MouseMsg) tea.Cmd {
    for _, node := range a.fileTree.VisibleNodes() {
        fileZone := zone.FileZoneID(zone.ZoneSidebarFile, node.Path)
        dirZone := zone.FileZoneID(zone.ZoneSidebarDir, node.Path)
        
        if zone.Manager.Get(fileZone).InBounds(msg) {
            return a.openFile(node.Path)
        }
        if zone.Manager.Get(dirZone).InBounds(msg) {
            a.fileTree.ToggleExpand(node.Path)
            return nil
        }
    }
    return nil
}
```

### 5.4 Mouse en Editor Tabs

```go
// En TabBar View()
func (tb *TabBar) View() string {
    var tabs []string
    for i, tab := range tb.tabs {
        rendered := tb.renderTab(tab, i == tb.activeIdx)
        
        // Zona clickeable para el tab
        tabZone := zone.Manager.Mark(
            fmt.Sprintf("%s%d", zone.ZoneEditorTab, i),
            rendered,
        )
        
        // Botón de cerrar
        closeBtn := zone.Manager.Mark(
            fmt.Sprintf("%s%d", zone.ZoneEditorTabClose, i),
            " ✕",
        )
        
        tabs = append(tabs, tabZone + closeBtn)
    }
    return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}
```

### 5.5 Mouse en Chat

```go
// Hacer clickeables: code blocks (copiar), links (abrir), archivos mencionados
func (m *messagesCmp) renderMessageWithZones(msg uiMessage) string {
    rendered := msg.Render()
    
    // Marcar code blocks para click-to-copy
    rendered = markCodeBlocks(rendered, msg.ID)
    
    // Marcar links para click-to-open
    rendered = markLinks(rendered, msg.ID)
    
    // Marcar archivos mencionados para click-to-view
    rendered = markFileReferences(rendered, msg.ID)
    
    return rendered
}
```

### 5.6 Mouse en Status Bar

```go
func (s *statusBar) View() string {
    model := zone.Manager.Mark(zone.ZoneStatusModel, s.modelName)
    session := zone.Manager.Mark(zone.ZoneStatusSession, s.sessionName)
    branch := zone.Manager.Mark(zone.ZoneStatusBranch, s.gitBranch)
    
    return lipgloss.JoinHorizontal(lipgloss.Top,
        model, " | ", session, " | ", branch,
    )
}

// Click en modelo -> abrir selector de modelos
// Click en sesión -> abrir lista de sesiones  
// Click en branch -> mostrar git info
```

### 5.7 Scroll con Mouse Wheel

```go
func (a *appModel) handleScrollUp(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    // Determinar en qué panel estamos
    if a.isInSidebarBounds(msg.X, msg.Y) {
        a.fileTree.ScrollUp(3)
    } else if a.isInEditorBounds(msg.X, msg.Y) {
        a.activeEditor.ScrollUp(3)
    } else {
        // Chat scroll
        a.chatMessages.ScrollUp(3)
    }
    return a, nil
}
```

### 5.8 View Final con Scan

```go
func (a *appModel) View() string {
    // Construir toda la vista
    view := a.buildView()
    
    // IMPORTANTE: Scan al final para procesar todas las zonas
    return zone.Manager.Scan(view)
}
```

## Archivos a Crear
1. `internal/tui/zone/zone.go` - Manager global y constantes de zonas

## Archivos a Modificar
1. `internal/tui/tui.go` - EnableMouse, handleMouse, View con Scan
2. `internal/tui/components/filetree/filetree.go` - Marcar zonas
3. `internal/tui/components/editor/tabs.go` - Marcar zonas
4. `internal/tui/components/editor/viewer.go` - Scroll con mouse
5. `internal/tui/components/chat/list.go` - Marcar zonas, clicks
6. `internal/tui/components/dialog/*.go` - Botones clickeables

## Dependencias
```
github.com/lrstanley/bubblezone  # Mouse zones
```

## Consideraciones
- **Performance**: `zone.Scan()` debe llamarse UNA vez al final del View()
- **Zone cleanup**: Las zonas deben limpiarse cuando los componentes se destruyen
- **Terminal support**: No todos los terminales soportan mouse. Degradar gracefully.
- **Conflicto con selección de texto**: El mouse tracking puede interferir con la selección de texto del terminal. Considerar un modo "pass-through".
- **Mobile/SSH**: En terminales remotos el mouse puede no funcionar bien.
