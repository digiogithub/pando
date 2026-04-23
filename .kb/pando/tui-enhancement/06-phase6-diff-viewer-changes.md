# Fase 6: Diff Viewer y Gestión de Cambios

## Objetivo
Implementar un diff viewer completo (split y unified) para visualizar cambios del agente AI, panel de archivos modificados, git status integrado, y navegación entre cambios.

## Referencia: Crush DiffView

### Estructura (`internal/ui/diffview/diffview.go`)
```go
type DiffView struct {
    layout          layout           // split o unified
    before          file             // contenido original
    after           file             // contenido modificado
    contextLines    int              // líneas de contexto
    lineNumbers     bool
    height, width   int
    xOffset, yOffset int             // scroll
    infiniteYScroll bool
    style           Style
    tabWidth        int
    chromaStyle     *chroma.Style
    
    // Computed
    isComputed  bool
    unified     udiff.UnifiedDiff
    edits       []udiff.Edit
    splitHunks  []splitHunk
    
    // Metrics
    totalLines, codeWidth, fullCodeWidth int
    beforeNumDigits, afterNumDigits int
    
    // Cache
    cachedLexer chroma.Lexer
    syntaxCache map[string]string
}
```

### Modos de Vista
- **Unified**: Vista tradicional de diff con +/- indicators
- **Split**: Vista lado a lado con before/after

### Rendering (`renderUnified`, `renderSplit`)
- Syntax highlighting por línea con cache
- Colores: verde para adiciones, rojo para eliminaciones
- Números de línea para ambos lados
- Hunk headers con información de posición

### Uso en Permisos (`internal/ui/dialog/permissions.go`)
```go
func (p *Permissions) hasDiffView() bool
func (p *Permissions) renderDiff(filePath, oldContent, newContent string, contentWidth int) string
// diffMaxWidth = 180
```

### Generación de Diff (`internal/diff/diff.go`)
```go
func GenerateDiff(beforeContent, afterContent, fileName string) (string, int, int)
```

## Plan de Implementación

### 6.1 Componente DiffView

```go
// internal/tui/components/diff/diffview.go
type DiffLayout int
const (
    DiffLayoutUnified DiffLayout = iota
    DiffLayoutSplit
)

type DiffView struct {
    // Content
    filePath   string
    before     string  // contenido original
    after      string  // contenido modificado
    
    // Computed diff
    hunks      []Hunk
    totalLines int
    computed   bool
    
    // Display
    layout       DiffLayout
    contextLines int  // default 3
    lineNumbers  bool
    
    // Viewport
    width, height int
    yOffset       int
    xOffset       int
    
    // Syntax highlighting
    highlighter *Highlighter
    syntaxCache map[string]string
    
    // Styling
    styles DiffStyles
    keyMap DiffKeyMap
}

type Hunk struct {
    OldStart, OldLines int
    NewStart, NewLines int
    Lines              []DiffLine
}

type DiffLine struct {
    Type    DiffLineType
    Content string
    OldNum  int  // número de línea en before
    NewNum  int  // número de línea en after
}

type DiffLineType int
const (
    DiffLineContext DiffLineType = iota
    DiffLineAdd
    DiffLineDelete
)
```

### 6.2 Rendering de Diff

```go
func (d *DiffView) renderUnified() string {
    var sb strings.Builder
    
    for _, hunk := range d.hunks {
        // Hunk header
        header := fmt.Sprintf("@@ -%d,%d +%d,%d @@",
            hunk.OldStart, hunk.OldLines,
            hunk.NewStart, hunk.NewLines)
        sb.WriteString(d.styles.HunkHeader.Render(header) + "\n")
        
        for _, line := range hunk.Lines {
            lineStr := ""
            
            // Line number
            if d.lineNumbers {
                lineStr += d.renderLineNumbers(line)
            }
            
            // Symbol and content
            switch line.Type {
            case DiffLineAdd:
                content := d.highlightLine(line.Content)
                lineStr += d.styles.AddLine.Render("+ " + content)
            case DiffLineDelete:
                content := d.highlightLine(line.Content)
                lineStr += d.styles.DeleteLine.Render("- " + content)
            case DiffLineContext:
                content := d.highlightLine(line.Content)
                lineStr += d.styles.ContextLine.Render("  " + content)
            }
            
            sb.WriteString(lineStr + "\n")
        }
    }
    return sb.String()
}

func (d *DiffView) renderSplit() string {
    // Vista lado a lado
    halfWidth := (d.width - 3) / 2  // -3 para separador
    
    var sb strings.Builder
    for _, hunk := range d.hunks {
        for _, line := range hunk.Lines {
            left := ""
            right := ""
            
            switch line.Type {
            case DiffLineDelete:
                left = d.styles.DeleteLine.Width(halfWidth).Render(
                    d.highlightLine(line.Content))
                right = strings.Repeat(" ", halfWidth)
            case DiffLineAdd:
                left = strings.Repeat(" ", halfWidth)
                right = d.styles.AddLine.Width(halfWidth).Render(
                    d.highlightLine(line.Content))
            case DiffLineContext:
                content := d.highlightLine(line.Content)
                left = d.styles.ContextLine.Width(halfWidth).Render(content)
                right = d.styles.ContextLine.Width(halfWidth).Render(content)
            }
            
            sb.WriteString(left + " │ " + right + "\n")
        }
    }
    return sb.String()
}
```

### 6.3 Panel de Archivos Modificados

```go
// internal/tui/components/diff/changes_panel.go
type ChangesPanel struct {
    changes    []FileChange
    cursor     int
    width      int
    height     int
    yOffset    int
    styles     ChangesPanelStyles
}

type FileChange struct {
    Path       string
    Status     ChangeStatus
    Additions  int
    Deletions  int
    OldContent string  // para generar diff
    NewContent string
}

type ChangeStatus int
const (
    ChangeStatusModified ChangeStatus = iota
    ChangeStatusAdded
    ChangeStatusDeleted
    ChangeStatusRenamed
)

func (c *ChangesPanel) View() string {
    var lines []string
    
    // Header
    lines = append(lines, c.styles.Header.Render(
        fmt.Sprintf(" Changes (%d files)", len(c.changes))))
    
    for i, change := range c.changes {
        icon := changeIcon(change.Status)
        stats := fmt.Sprintf("+%d -%d", change.Additions, change.Deletions)
        
        line := fmt.Sprintf("%s %s %s",
            icon,
            filepath.Base(change.Path),
            c.styles.Stats.Render(stats))
        
        if i == c.cursor {
            line = c.styles.Selected.Render(line)
        }
        
        lines = append(lines, line)
    }
    
    return strings.Join(lines, "\n")
}

func changeIcon(status ChangeStatus) string {
    switch status {
    case ChangeStatusModified: return "●"  // amarillo
    case ChangeStatusAdded:    return "+"   // verde
    case ChangeStatusDeleted:  return "-"   // rojo
    case ChangeStatusRenamed:  return "→"   // azul
    default:                   return "?"
    }
}
```

### 6.4 Integración con el Agente AI

```go
// Cuando el agente modifica un archivo, capturar el cambio
type AgentFileChangeMsg struct {
    Path       string
    OldContent string
    NewContent string
    ToolName   string  // "edit", "write", etc.
}

// En el modelo principal
func (a *appModel) handleAgentChange(msg AgentFileChangeMsg) tea.Cmd {
    change := diff.FileChange{
        Path:       msg.Path,
        Status:     diff.ChangeStatusModified,
        OldContent: msg.OldContent,
        NewContent: msg.NewContent,
    }
    
    // Calcular stats
    change.Additions, change.Deletions = countChanges(
        msg.OldContent, msg.NewContent)
    
    a.changesPanel.AddChange(change)
    
    // Actualizar git status en sidebar
    return a.fileTree.RefreshGitStatus()
}
```

### 6.5 Vista de Diff en el Chat (Inline)

```go
// Cuando el AI muestra un cambio en el chat, renderizar inline diff
func renderInlineDiff(filePath, oldContent, newContent string, width int) string {
    dv := diff.NewDiffView(diff.DiffViewOptions{
        FilePath:     filePath,
        Before:       oldContent,
        After:        newContent,
        Layout:       diff.DiffLayoutUnified,
        ContextLines: 3,
        Width:        min(width, 120),
        LineNumbers:  true,
    })
    
    return dv.View()
}
```

### 6.6 Página/Vista de Cambios

```go
// internal/tui/page/changes.go
// Nueva página o modo de vista para gestionar cambios
type ChangesView struct {
    // Left panel: lista de archivos cambiados
    changesPanel *diff.ChangesPanel
    
    // Right panel: diff del archivo seleccionado
    diffView     *diff.DiffView
    
    // Split layout
    split        layout.SplitPaneLayout
    
    // Focus
    focusPanel   int // 0=changes, 1=diff
}

// Keybindings
type ChangesKeyMap struct {
    NextFile     key.Binding // j/down
    PrevFile     key.Binding // k/up
    ToggleLayout key.Binding // t (unified/split)
    AcceptAll    key.Binding // a
    RevertFile   key.Binding // r (revert cambio)
    OpenFile     key.Binding // enter (abrir en editor)
    NextHunk     key.Binding // ]c
    PrevHunk     key.Binding // [c
    CopyDiff     key.Binding // y
}
```

### 6.7 Git Status Integrado

```go
// internal/tui/components/git/status.go
func GetGitStatus(projectPath string) tea.Cmd {
    return func() tea.Msg {
        cmd := exec.Command("git", "-C", projectPath, "status", "--porcelain", "-u")
        output, err := cmd.Output()
        if err != nil {
            return GitStatusErrorMsg{err}
        }
        
        statuses := parseGitStatus(string(output))
        return GitStatusMsg{Statuses: statuses}
    }
}

func parseGitStatus(output string) map[string]GitFileStatus {
    statuses := make(map[string]GitFileStatus)
    for _, line := range strings.Split(output, "\n") {
        if len(line) < 4 { continue }
        
        status := line[:2]
        path := strings.TrimSpace(line[3:])
        
        switch {
        case status == "??":
            statuses[path] = GitStatusUntracked
        case status[0] == 'M' || status[1] == 'M':
            statuses[path] = GitStatusModified
        case status[0] == 'A':
            statuses[path] = GitStatusAdded
        case status[0] == 'D' || status[1] == 'D':
            statuses[path] = GitStatusDeleted
        case status[0] == 'R':
            statuses[path] = GitStatusRenamed
        }
    }
    return statuses
}
```

## Archivos a Crear
1. `internal/tui/components/diff/diffview.go` - Componente DiffView
2. `internal/tui/components/diff/styles.go` - Estilos de diff
3. `internal/tui/components/diff/changes_panel.go` - Panel de cambios
4. `internal/tui/components/diff/keys.go` - Keybindings
5. `internal/tui/components/git/status.go` - Git status integration
6. `internal/tui/page/changes.go` - Página de cambios (opcional)

## Archivos a Modificar
1. `internal/tui/tui.go` - Integrar panel de cambios y diff viewer
2. `internal/tui/components/filetree/filetree.go` - Mostrar git status icons
3. `internal/tui/components/chat/message.go` - Inline diffs en chat

## Dependencias
```
github.com/alecthomas/chroma/v2  # Ya añadido en Fase 3
# Para diff: usar go-udiff o implementación propia
# go-udiff: github.com/nicois/udiff (o similar)
```

## Consideraciones
- **Diff algorithm**: Usar Myers diff o similar. Crush usa `udiff` package.
- **Performance**: Diffs grandes necesitan virtualización (solo renderizar visible).
- **Inline word diff**: Dentro de líneas modificadas, resaltar las palabras exactas que cambiaron.
- **Binary files**: Detectar y mostrar "Binary file differs".
- **Large files**: Limitar tamaño de diff mostrado, con opción de ver completo.
- **Undo/Revert**: Permitir revertir cambios individuales del agente.
- **Auto-refresh**: Actualizar git status periódicamente o tras cambios del agente.
