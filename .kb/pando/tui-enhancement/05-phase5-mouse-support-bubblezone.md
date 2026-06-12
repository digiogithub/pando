# Phase 5: Mouse Support with Bubblezone

## Objective
Integrate bubblezone to make all panels and elements clickable with the mouse, including the sidebar, editor tabs, chat elements, dialog buttons, and scroll with wheel.

## Reference: Bubblezone (lrstanley/bubblezone)

### Concept
Bubblezone adds zones with mouse tracking over existing bubbletea components. It works by wrapping the output of `View()` with zone markers, and then intercepting mouse events to determine which zone was clicked.

### Basic Usage
```go
import zone "github.com/lrstanley/bubblezone"

// 1. Create global manager
var z = zone.New()

// 2. In Init(), enable mouse
func (m model) Init() tea.Cmd {
    return tea.EnableMouseAllMotion  // or tea.EnableMouseCellMotion
}

// 3. In View(), mark zones
func (m model) View() string {
    // Wrap each clickable element with zone.Mark()
    button1 := z.Mark("btn-save", "[ Save ]")
    button2 := z.Mark("btn-cancel", "[ Cancel ]")
    
    // Scan the final output
    return z.Scan(lipgloss.JoinHorizontal(lipgloss.Top, button1, button2))
}

// 4. In Update(), detect clicks
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

### Reference: Crush Mouse Support
Crush implements native mouse support without bubblezone, directly in `model/chat.go`:
```go
// Chat struct fields
mouseDownY, mouseDragItem, mouseDragY int
lastClickTime time.Time
lastClickY, clickCount int
pendingClickID int

// Methods
HandleMouseDown(x, y int) (bool, tea.Cmd)
HandleMouseUp(x, y int) bool
HandleMouseDrag(x, y int) bool
HandleDelayedClick(msg DelayedClickMsg) bool
selectWord(itemIdx, x, itemY int)
selectLine(itemIdx, itemY int)
```

## Implementation Plan

### 5.1 Global Bubblezone Setup

```go
// internal/tui/zone/zone.go
package zone

import zone "github.com/lrstanley/bubblezone"

// Global manager
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

// Helper: generate unique zone ID
func FileZoneID(prefix, path string) string {
    return prefix + hashPath(path)
}
```

### 5.2 Integration in Main Model

```go
// internal/tui/tui.go
func (a *appModel) Init() tea.Cmd {
    return tea.Batch(
        // ... other cmds ...
        tea.EnableMouseCellMotion,  // Enable mouse
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

### 5.3 Mouse in Sidebar (FileTree)

```go
// In filetree View()
func (ft *FileTree) View() string {
    var lines []string
    for _, node := range ft.visibleNodes() {
        line := ft.renderNode(node)
        
        // Make clickable
        zoneID := zone.FileZoneID(zone.ZoneSidebarFile, node.Path)
        if node.Type == NodeTypeDirectory {
            zoneID = zone.FileZoneID(zone.ZoneSidebarDir, node.Path)
        }
        line = zone.Manager.Mark(zoneID, line)
        
        lines = append(lines, line)
    }
    return strings.Join(lines, "\n")
}

// In handleLeftClick
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

### 5.4 Mouse in Editor Tabs

```go
// In TabBar View()
func (tb *TabBar) View() string {
    var tabs []string
    for i, tab := range tb.tabs {
        rendered := tb.renderTab(tab, i == tb.activeIdx)
        
        // Clickable zone for the tab
        tabZone := zone.Manager.Mark(
            fmt.Sprintf("%s%d", zone.ZoneEditorTab, i),
            rendered,
        )
        
        // Close button
        closeBtn := zone.Manager.Mark(
            fmt.Sprintf("%s%d", zone.ZoneEditorTabClose, i),
            " ✕",
        )
        
        tabs = append(tabs, tabZone + closeBtn)
    }
    return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}
```

### 5.5 Mouse in Chat

```go
// Make clickable: code blocks (copy), links (open), mentioned files
func (m *messagesCmp) renderMessageWithZones(msg uiMessage) string {
    rendered := msg.Render()
    
    // Mark code blocks for click-to-copy
    rendered = markCodeBlocks(rendered, msg.ID)
    
    // Mark links for click-to-open
    rendered = markLinks(rendered, msg.ID)
    
    // Mark mentioned files for click-to-view
    rendered = markFileReferences(rendered, msg.ID)
    
    return rendered
}
```

### 5.6 Mouse in Status Bar

```go
func (s *statusBar) View() string {
    model := zone.Manager.Mark(zone.ZoneStatusModel, s.modelName)
    session := zone.Manager.Mark(zone.ZoneStatusSession, s.sessionName)
    branch := zone.Manager.Mark(zone.ZoneStatusBranch, s.gitBranch)
    
    return lipgloss.JoinHorizontal(lipgloss.Top,
        model, " | ", session, " | ", branch,
    )
}

// Click on model -> open model selector
// Click on session -> open session list
// Click on branch -> show git info
```

### 5.7 Scroll with Mouse Wheel

```go
func (a *appModel) handleScrollUp(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    // Determine which panel we are in
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

### 5.8 Final View with Scan

```go
func (a *appModel) View() string {
    // Build the entire view
    view := a.buildView()
    
    // IMPORTANT: Scan at the end to process all zones
    return zone.Manager.Scan(view)
}
```

## Files to Create
1. `internal/tui/zone/zone.go` - Global manager and zone constants

## Files to Modify
1. `internal/tui/tui.go` - EnableMouse, handleMouse, View with Scan
2. `internal/tui/components/filetree/filetree.go` - Mark zones
3. `internal/tui/components/editor/tabs.go` - Mark zones
4. `internal/tui/components/editor/viewer.go` - Scroll with mouse
5. `internal/tui/components/chat/list.go` - Mark zones, clicks
6. `internal/tui/components/dialog/*.go` - Clickable buttons

## Dependencies
```
github.com/lrstanley/bubblezone  # Mouse zones
```

## Considerations
- **Performance**: `zone.Scan()` must be called ONCE at the end of View()
- **Zone cleanup**: Zones must be cleaned up when components are destroyed
- **Terminal support**: Not all terminals support mouse. Degrade gracefully.
- **Text selection conflict**: Mouse tracking may interfere with terminal text selection. Consider a "pass-through" mode.
- **Mobile/SSH**: In remote terminals, mouse may not work well.