# Phase 6: Diff Viewer and Change Management

## Objective
Implement a complete diff viewer (split and unified) to visualize AI agent changes, modified files panel, integrated git status, and navigation between changes.

## Reference: Crush DiffView

### Structure (`internal/ui/diffview/diffview.go`)
```go
type DiffView struct {
    layout          layout           // split or unified
    before          file             // original content
    after           file             // modified content
    contextLines    int              // context lines
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

### View Modes
- **Unified**: Traditional diff view with +/- indicators
- **Split**: Side-by-side view with before/after

### Rendering (`renderUnified`, `renderSplit`)
- Syntax highlighting per line with cache
- Colors: green for additions, red for deletions
- Line numbers for both sides
- Hunk headers with position information

### Use in Permissions (`internal/ui/dialog/permissions.go`)
```go
func (p *Permissions) hasDiffView() bool
func (p *Permissions) renderDiff(filePath, oldContent, newContent string, contentWidth int) string
// diffMaxWidth = 180
```

### Diff Generation (`internal/diff/diff.go`)
```go
func GenerateDiff(beforeContent, afterContent, fileName string) (string, int, int)
```

## Implementation Plan

### 6.1 DiffView Component

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
    before     string  // original content
    after      string  // modified content
    
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
    OldNum  int  // line number in before
    NewNum  int  // line number in after
}

type DiffLineType int
const (
    DiffLineContext DiffLineType = iota
    DiffLineAdd
    DiffLineDelete
)
```

### 6.2 Diff Rendering

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
    // Side-by-side view
    halfWidth := (d.width - 3) / 2  // -3 for separator
    
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

### 6.3 Modified Files Panel

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
    OldContent string  // for generating diff
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
    case ChangeStatusModified: return "●"  // yellow
    case ChangeStatusAdded:    return "+"   // green
    case ChangeStatusDeleted:  return "-"   // red
    case ChangeStatusRenamed:  return "→"   // blue
    default:                   return "?"
    }
}
```

### 6.4 Integration with AI Agent

```go
// When the agent modifies a file, capture the change
type AgentFileChangeMsg struct {
    Path       string
    OldContent string
    NewContent string
    ToolName   string  // "edit", "write", etc.
}

// In the main model
func (a *appModel) handleAgentChange(msg AgentFileChangeMsg) tea.Cmd {
    change := diff.FileChange{
        Path:       msg.Path,
        Status:     diff.ChangeStatusModified,
        OldContent: msg.OldContent,
        NewContent: msg.NewContent,
    }
    
    // Calculate stats
    change.Additions, change.Deletions = countChanges(
        msg.OldContent, msg.NewContent)
    
    a.changesPanel.AddChange(change)
    
    // Update git status in sidebar
    return a.fileTree.RefreshGitStatus()
}
```

### 6.5 Inline Diff in Chat

```go
// When AI shows a change in chat, render inline diff
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

### 6.6 Changes Page/View

```go
// internal/tui/page/changes.go
// New page or view mode for managing changes
type ChangesView struct {
    // Left panel: list of changed files
    changesPanel *diff.ChangesPanel
    
    // Right panel: diff of selected file
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
    RevertFile   key.Binding // r (revert change)
    OpenFile     key.Binding // enter (open in editor)
    NextHunk     key.Binding // ]c
    PrevHunk     key.Binding // [c
    CopyDiff     key.Binding // y
}
```

### 6.7 Integrated Git Status

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

## Files to Create
1. `internal/tui/components/diff/diffview.go` - DiffView component
2. `internal/tui/components/diff/styles.go` - Diff styles
3. `internal/tui/components/diff/changes_panel.go` - Changes panel
4. `internal/tui/components/diff/keys.go` - Keybindings
5. `internal/tui/components/git/status.go` - Git status integration
6. `internal/tui/page/changes.go` - Changes page (optional)

## Files to Modify
1. `internal/tui/tui.go` - Integrate changes panel and diff viewer
2. `internal/tui/components/filetree/filetree.go` - Show git status icons
3. `internal/tui/components/chat/message.go` - Inline diffs in chat

## Dependencies
```
github.com/alecthomas/chroma/v2  # Already added in Phase 3
# For diff: use go-udiff or custom implementation
# go-udiff: github.com/nicois/udiff (or similar)
```

## Considerations
- **Diff algorithm**: Use Myers diff or similar. Crush uses `udiff` package.
- **Performance**: Large diffs need virtualization (only render visible).
- **Inline word diff**: Within modified lines, highlight the exact words that changed.
- **Binary files**: Detect and display "Binary file differs".
- **Large files**: Limit displayed diff size, with option to view complete.
- **Undo/Revert**: Allow reverting individual agent changes.
- **Auto-refresh**: Update git status periodically or after agent changes.