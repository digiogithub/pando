package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

// FileEditDirtyMsg is emitted when the editable file's dirty state changes.
type FileEditDirtyMsg struct {
	Path  string
	Dirty bool
}

// FileEditSavedMsg is emitted after a file is successfully saved.
type FileEditSavedMsg struct {
	Path string
}

// OpenEditableFileMsg requests that a file be opened in editable mode.
type OpenEditableFileMsg struct {
	Path string
}

// ExitEditModeMsg is emitted when the user presses Esc in edit mode to return to view mode.
type ExitEditModeMsg struct {
	Path string
}

// FileEditableComponent is the editable file editor used by the editor area.
type FileEditableComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
	OpenFile(path string) tea.Cmd
	FilePath() string
	IsDirty() bool
}

// EditableKeyMap defines keybindings for the editable file editor.
type EditableKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Left      key.Binding
	Right     key.Binding
	LineStart  key.Binding
	LineEnd    key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	FileStart key.Binding
	FileEnd   key.Binding
	Save      key.Binding
	// Selection (shift variants)
	SelectUp        key.Binding
	SelectDown      key.Binding
	SelectLeft      key.Binding
	SelectRight     key.Binding
	SelectLineStart key.Binding
	SelectLineEnd   key.Binding
	SelectAll       key.Binding
	// Clipboard
	Copy           key.Binding
	Paste          key.Binding
	PasteClipboard key.Binding
	// Multicursor (Micro-style)
	AddCursorUp   key.Binding
	AddCursorDown key.Binding
	// Exit edit mode
	ExitEditMode key.Binding
}

// DefaultEditableKeyMap returns the default keybindings for the editable component.
func DefaultEditableKeyMap() EditableKeyMap {
	return EditableKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "left"),
		),
		Right: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "right"),
		),
		LineStart: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "line start"),
		),
		LineEnd: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "line end"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn", "page down"),
		),
		FileStart: key.NewBinding(
			key.WithKeys("ctrl+home"),
			key.WithHelp("ctrl+home", "file start"),
		),
		FileEnd: key.NewBinding(
			key.WithKeys("ctrl+end"),
			key.WithHelp("ctrl+end", "file end"),
		),
		Save: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "save file"),
		),
		// Selection
		SelectUp: key.NewBinding(
			key.WithKeys("shift+up"),
			key.WithHelp("shift+↑", "select up"),
		),
		SelectDown: key.NewBinding(
			key.WithKeys("shift+down"),
			key.WithHelp("shift+↓", "select down"),
		),
		SelectLeft: key.NewBinding(
			key.WithKeys("shift+left"),
			key.WithHelp("shift+←", "select left"),
		),
		SelectRight: key.NewBinding(
			key.WithKeys("shift+right"),
			key.WithHelp("shift+→", "select right"),
		),
		SelectLineStart: key.NewBinding(
			key.WithKeys("shift+home"),
			key.WithHelp("shift+home", "select to line start"),
		),
		SelectLineEnd: key.NewBinding(
			key.WithKeys("shift+end"),
			key.WithHelp("shift+end", "select to line end"),
		),
		SelectAll: key.NewBinding(
			key.WithKeys("ctrl+a"),
			key.WithHelp("ctrl+a", "select all"),
		),
		// Clipboard
		Copy: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "copy"),
		),
		Paste: key.NewBinding(
			key.WithKeys("ctrl+v"),
			key.WithHelp("ctrl+v", "paste"),
		),
		PasteClipboard: key.NewBinding(
			key.WithKeys("ctrl+shift+v"),
			key.WithHelp("ctrl+shift+v", "paste from clipboard"),
		),
		// Multicursor (Micro-style: Alt+Up/Down adds cursor)
		AddCursorUp: key.NewBinding(
			key.WithKeys("alt+up"),
			key.WithHelp("alt+↑", "add cursor above"),
		),
		AddCursorDown: key.NewBinding(
			key.WithKeys("alt+down"),
			key.WithHelp("alt+↓", "add cursor below"),
		),
		// Exit edit mode
		ExitEditMode: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "exit edit mode"),
		),
	}
}

type editableFileLoadedMsg struct {
	requestID int
	path      string
	lines     []string
	err       error
}

// cursorPos represents a single cursor position.
type cursorPos struct {
	row int
	col int
}

type fileEditor struct {
	width  int
	height int

	viewport viewport.Model
	keyMap   EditableKeyMap

	filePath         string
	lines            []string // editable buffer
	highlightedLines []string // syntax-highlighted lines for display
	lastHighlighted  string   // content at the time of last highlight (for cache invalidation)

	// Primary cursor
	cursorRow int
	cursorCol int

	// Additional cursors (multicursor)
	extraCursors []cursorPos

	// Selection
	hasSelection    bool
	selAnchorRow    int
	selAnchorCol    int

	// Internal clipboard (separate from OS clipboard)
	internalClipboard string

	dirty         bool
	loading       bool
	loadErr       error
	nextRequestID int
	pendingLoadID int
}

// NewFileEditor creates a new editable file component.
func NewFileEditor() FileEditableComponent {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 2

	return &fileEditor{
		viewport: vp,
		keyMap:   DefaultEditableKeyMap(),
		lines:    []string{""},
	}
}

func (e *fileEditor) Init() tea.Cmd {
	return nil
}

func (e *fileEditor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dialog.ThemeChangedMsg:
		e.lastHighlighted = "" // force re-highlight
		e.rehighlight()
		e.refreshViewport()
		return e, nil

	case editableFileLoadedMsg:
		if msg.requestID != e.pendingLoadID {
			return e, nil
		}
		e.loading = false
		e.loadErr = msg.err
		if msg.err != nil {
			e.lines = []string{""}
			e.highlightedLines = nil
			e.cursorRow = 0
			e.cursorCol = 0
			e.clearSelection()
			e.extraCursors = nil
			e.viewport.SetYOffset(0)
			e.refreshViewport()
			return e, util.ReportError(msg.err)
		}
		e.filePath = msg.path
		e.lines = msg.lines
		e.lastHighlighted = ""
		e.rehighlight()
		e.cursorRow = 0
		e.cursorCol = 0
		e.clearSelection()
		e.extraCursors = nil
		e.dirty = false
		e.viewport.SetYOffset(0)
		e.refreshViewport()
		return e, nil

	case tea.KeyMsg:
		return e, e.handleKey(msg)

	case tea.MouseMsg:
		return e, e.handleMouse(msg)
	}

	updatedVP, cmd := e.viewport.Update(msg)
	e.viewport = updatedVP
	return e, cmd
}

func (e *fileEditor) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	// Exit edit mode
	case key.Matches(msg, e.keyMap.ExitEditMode):
		e.clearSelection()
		e.extraCursors = nil
		return util.CmdHandler(ExitEditModeMsg{Path: e.filePath})

	case key.Matches(msg, e.keyMap.Save):
		return e.saveFile()

	// Select all
	case key.Matches(msg, e.keyMap.SelectAll):
		e.selAnchorRow = 0
		e.selAnchorCol = 0
		e.hasSelection = true
		e.cursorRow = max(len(e.lines)-1, 0)
		e.cursorCol = e.currentLineLen()
		e.ensureCursorVisible()
		e.refreshViewport()

	// Copy
	case key.Matches(msg, e.keyMap.Copy):
		if e.hasSelection {
			text := e.extractSelection()
			e.internalClipboard = text
			// Also copy to OS clipboard (best-effort)
			_ = clipboard.WriteAll(text)
		} else if len(e.extraCursors) == 0 {
			// Copy current line
			if e.cursorRow < len(e.lines) {
				e.internalClipboard = e.lines[e.cursorRow]
				_ = clipboard.WriteAll(e.internalClipboard)
			}
		}

	// Paste (internal clipboard first, then OS clipboard)
	case key.Matches(msg, e.keyMap.Paste):
		text := e.internalClipboard
		if text == "" {
			text, _ = clipboard.ReadAll()
		}
		e.pasteText(text)
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	// Paste from OS clipboard explicitly
	case key.Matches(msg, e.keyMap.PasteClipboard):
		text, _ := clipboard.ReadAll()
		e.pasteText(text)
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	// Selection movement
	case key.Matches(msg, e.keyMap.SelectUp):
		e.startOrExtendSelection()
		e.moveCursorUp()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.SelectDown):
		e.startOrExtendSelection()
		e.moveCursorDown()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.SelectLeft):
		e.startOrExtendSelection()
		e.moveCursorLeft()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.SelectRight):
		e.startOrExtendSelection()
		e.moveCursorRight()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.SelectLineStart):
		e.startOrExtendSelection()
		e.cursorCol = 0
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.SelectLineEnd):
		e.startOrExtendSelection()
		e.cursorCol = e.currentLineLen()
		e.ensureCursorVisible()
		e.refreshViewport()

	// Regular movement (clears selection)
	case key.Matches(msg, e.keyMap.Up):
		e.clearSelection()
		e.moveCursorUp()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.Down):
		e.clearSelection()
		e.moveCursorDown()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.Left):
		if e.hasSelection {
			// Jump to start of selection
			sRow, sCol, _, _ := e.selectionBounds()
			e.cursorRow, e.cursorCol = sRow, sCol
			e.clearSelection()
		} else {
			e.moveCursorLeft()
		}
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.Right):
		if e.hasSelection {
			// Jump to end of selection
			_, _, eRow, eCol := e.selectionBounds()
			e.cursorRow, e.cursorCol = eRow, eCol
			e.clearSelection()
		} else {
			e.moveCursorRight()
		}
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.LineStart):
		e.clearSelection()
		e.cursorCol = 0
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.LineEnd):
		e.clearSelection()
		e.cursorCol = e.currentLineLen()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.PageUp):
		e.clearSelection()
		step := max(e.viewport.Height/2, 1)
		e.cursorRow = max(e.cursorRow-step, 0)
		e.clampCursorCol()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.PageDown):
		e.clearSelection()
		step := max(e.viewport.Height/2, 1)
		e.cursorRow = min(e.cursorRow+step, max(len(e.lines)-1, 0))
		e.clampCursorCol()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.FileStart):
		e.clearSelection()
		e.cursorRow = 0
		e.cursorCol = 0
		e.viewport.SetYOffset(0)
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.FileEnd):
		e.clearSelection()
		e.cursorRow = max(len(e.lines)-1, 0)
		e.cursorCol = e.currentLineLen()
		e.ensureCursorVisible()
		e.refreshViewport()

	// Multicursor: Micro-style Alt+Up/Down
	case key.Matches(msg, e.keyMap.AddCursorUp):
		if e.cursorRow > 0 {
			e.extraCursors = append(e.extraCursors, cursorPos{row: e.cursorRow, col: e.cursorCol})
			e.cursorRow--
			e.clampCursorCol()
			e.ensureCursorVisible()
			e.refreshViewport()
		}

	case key.Matches(msg, e.keyMap.AddCursorDown):
		if e.cursorRow < len(e.lines)-1 {
			e.extraCursors = append(e.extraCursors, cursorPos{row: e.cursorRow, col: e.cursorCol})
			e.cursorRow++
			e.clampCursorCol()
			e.ensureCursorVisible()
			e.refreshViewport()
		}

	case msg.String() == "enter":
		if e.hasSelection {
			e.deleteSelection()
		}
		e.applyToAllCursors(func() { e.insertNewline() })
		e.clearSelection()
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	case msg.String() == "backspace":
		if e.hasSelection {
			e.deleteSelection()
			e.markDirty()
			e.rehighlight()
			e.ensureCursorVisible()
			e.refreshViewport()
		} else {
			e.applyToAllCursors(func() { e.deleteBackward() })
			e.markDirty()
			e.rehighlight()
			e.ensureCursorVisible()
			e.refreshViewport()
		}

	case msg.String() == "delete":
		if e.hasSelection {
			e.deleteSelection()
			e.markDirty()
			e.rehighlight()
			e.ensureCursorVisible()
			e.refreshViewport()
		} else {
			e.applyToAllCursors(func() { e.deleteForward() })
			e.markDirty()
			e.rehighlight()
			e.ensureCursorVisible()
			e.refreshViewport()
		}

	case msg.String() == "tab":
		if e.hasSelection {
			e.deleteSelection()
		}
		// Insert a tab as spaces
		e.applyToAllCursors(func() {
			for i := 0; i < 4; i++ {
				e.insertRune(' ')
			}
		})
		e.clearSelection()
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	default:
		if len(msg.Runes) == 1 {
			if e.hasSelection {
				e.deleteSelection()
			}
			r := msg.Runes[0]
			e.applyToAllCursors(func() { e.insertRune(r) })
			e.clearSelection()
			e.markDirty()
			e.rehighlight()
			e.ensureCursorVisible()
			e.refreshViewport()
		}
	}

	return nil
}

func (e *fileEditor) handleMouse(msg tea.MouseMsg) tea.Cmd {
	zone := tuizone.Manager.Get(tuizone.EditorViewport)

	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonLeft:
			if zone != nil && zone.InBounds(msg) {
				// Calculate relative position within the editor viewport
				relY := msg.Y - zone.StartY
				relX := msg.X - zone.StartX

				// Map to document coordinates
				row := e.viewport.YOffset + relY
				if row < 0 {
					row = 0
				}
				if row >= len(e.lines) {
					row = max(len(e.lines)-1, 0)
				}

				// Subtract gutter width (+1 for the space after line numbers)
				col := max(0, relX-e.gutterWidth()-1)
				lineLen := len([]rune(e.lines[row]))
				if col > lineLen {
					col = lineLen
				}

				e.cursorRow = row
				e.cursorCol = col
				e.clearSelection()
				e.extraCursors = nil
				e.ensureCursorVisible()
				e.refreshViewport()
				return nil
			}
		case tea.MouseButtonWheelUp:
			updatedVP, cmd := e.viewport.Update(msg)
			e.viewport = updatedVP
			return cmd
		case tea.MouseButtonWheelDown:
			updatedVP, cmd := e.viewport.Update(msg)
			e.viewport = updatedVP
			return cmd
		}
	case tea.MouseActionMotion:
		// Drag to extend selection
		if msg.Button == tea.MouseButtonLeft && zone != nil && zone.InBounds(msg) {
			if !e.hasSelection {
				// Start selection from current cursor position
				e.selAnchorRow = e.cursorRow
				e.selAnchorCol = e.cursorCol
				e.hasSelection = true
			}
			relY := msg.Y - zone.StartY
			relX := msg.X - zone.StartX

			row := e.viewport.YOffset + relY
			if row < 0 {
				row = 0
			}
			if row >= len(e.lines) {
				row = max(len(e.lines)-1, 0)
			}
			col := max(0, relX-e.gutterWidth()-1)
			lineLen := len([]rune(e.lines[row]))
			if col > lineLen {
				col = lineLen
			}
			e.cursorRow = row
			e.cursorCol = col
			e.refreshViewport()
		}
	}

	return nil
}

func (e *fileEditor) View() string {
	if e.width <= 0 || e.height <= 0 {
		return ""
	}

	t := tuitheme.CurrentTheme()
	base := styles.BaseStyle().Width(e.width)

	contentHeight := max(e.height-1, 0)
	content := base.Width(e.width).Height(contentHeight).Render(
		tuizone.MarkEditorViewport(e.viewport.View()),
	)

	status := e.renderStatusLine(t)
	return lipgloss.JoinVertical(lipgloss.Left, content, status)
}

func (e *fileEditor) SetSize(width, height int) tea.Cmd {
	e.width = max(width, 0)
	e.height = max(height, 0)
	e.viewport.Width = e.width
	e.viewport.Height = max(e.height-1, 0)
	e.ensureCursorVisible()
	e.refreshViewport()
	return nil
}

func (e *fileEditor) GetSize() (int, int) {
	return e.width, e.height
}

func (e *fileEditor) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(e.keyMap)
}

func (e *fileEditor) FilePath() string {
	return e.filePath
}

func (e *fileEditor) IsDirty() bool {
	return e.dirty
}

// OpenFile asynchronously loads a file for editing.
func (e *fileEditor) OpenFile(path string) tea.Cmd {
	e.nextRequestID++
	e.pendingLoadID = e.nextRequestID
	e.filePath = path
	e.loading = true
	e.loadErr = nil
	e.lines = []string{""}
	e.highlightedLines = nil
	e.lastHighlighted = ""
	e.cursorRow = 0
	e.cursorCol = 0
	e.clearSelection()
	e.extraCursors = nil
	e.dirty = false
	e.viewport.SetYOffset(0)
	e.refreshViewport()

	requestID := e.pendingLoadID

	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return editableFileLoadedMsg{
				requestID: requestID,
				path:      path,
				err:       err,
			}
		}

		content := normalizeContent(data)
		lines := splitViewerLines(content)

		return editableFileLoadedMsg{
			requestID: requestID,
			path:      path,
			lines:     lines,
		}
	}
}

// saveFile writes the buffer content back to disk.
func (e *fileEditor) saveFile() tea.Cmd {
	if e.filePath == "" {
		return util.ReportWarn("No file path to save")
	}

	content := strings.Join(e.lines, "\n")
	path := e.filePath

	return func() tea.Msg {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil { //nolint:gosec
			return util.InfoMsg{Type: util.InfoTypeError, Msg: "Save failed: " + err.Error()}
		}
		return FileEditSavedMsg{Path: path}
	}
}

func (e *fileEditor) markDirty() {
	if !e.dirty {
		e.dirty = true
	}
}

// --- Selection ---

func (e *fileEditor) clearSelection() {
	e.hasSelection = false
}

func (e *fileEditor) startOrExtendSelection() {
	if !e.hasSelection {
		e.selAnchorRow = e.cursorRow
		e.selAnchorCol = e.cursorCol
		e.hasSelection = true
	}
}

// selectionBounds returns (startRow, startCol, endRow, endCol) in document order.
func (e *fileEditor) selectionBounds() (int, int, int, int) {
	aRow, aCol := e.selAnchorRow, e.selAnchorCol
	cRow, cCol := e.cursorRow, e.cursorCol

	if aRow < cRow || (aRow == cRow && aCol <= cCol) {
		return aRow, aCol, cRow, cCol
	}
	return cRow, cCol, aRow, aCol
}

// extractSelection returns the selected text as a string.
func (e *fileEditor) extractSelection() string {
	if !e.hasSelection {
		return ""
	}
	sRow, sCol, eRow, eCol := e.selectionBounds()

	if sRow >= len(e.lines) {
		return ""
	}

	if sRow == eRow {
		line := []rune(e.lines[sRow])
		sCol = min(sCol, len(line))
		eCol = min(eCol, len(line))
		return string(line[sCol:eCol])
	}

	var b strings.Builder
	// First line: from sCol to end
	firstLine := []rune(e.lines[sRow])
	sCol = min(sCol, len(firstLine))
	b.WriteString(string(firstLine[sCol:]))
	b.WriteByte('\n')
	// Middle lines
	for r := sRow + 1; r < eRow && r < len(e.lines); r++ {
		b.WriteString(e.lines[r])
		b.WriteByte('\n')
	}
	// Last line: from start to eCol
	if eRow < len(e.lines) {
		lastLine := []rune(e.lines[eRow])
		eCol = min(eCol, len(lastLine))
		b.WriteString(string(lastLine[:eCol]))
	}
	return b.String()
}

// deleteSelection deletes the selected text, moves cursor to selection start.
func (e *fileEditor) deleteSelection() {
	if !e.hasSelection {
		return
	}
	sRow, sCol, eRow, eCol := e.selectionBounds()
	if sRow >= len(e.lines) {
		e.clearSelection()
		return
	}

	if sRow == eRow {
		line := []rune(e.lines[sRow])
		sCol = min(sCol, len(line))
		eCol = min(eCol, len(line))
		newLine := make([]rune, 0, len(line)-(eCol-sCol))
		newLine = append(newLine, line[:sCol]...)
		newLine = append(newLine, line[eCol:]...)
		e.lines[sRow] = string(newLine)
	} else {
		firstLine := []rune(e.lines[sRow])
		sCol = min(sCol, len(firstLine))
		before := string(firstLine[:sCol])

		var after string
		if eRow < len(e.lines) {
			lastLine := []rune(e.lines[eRow])
			eCol = min(eCol, len(lastLine))
			after = string(lastLine[eCol:])
		}

		newLines := make([]string, 0, len(e.lines)-(eRow-sRow))
		newLines = append(newLines, e.lines[:sRow]...)
		newLines = append(newLines, before+after)
		if eRow+1 < len(e.lines) {
			newLines = append(newLines, e.lines[eRow+1:]...)
		}
		e.lines = newLines
	}

	e.cursorRow = sRow
	e.cursorCol = sCol
	e.clearSelection()
	e.extraCursors = nil
}

// isRowColInSelection returns true when the given position is inside the current selection.
func (e *fileEditor) isRowColInSelection(row, col int) bool {
	if !e.hasSelection {
		return false
	}
	sRow, sCol, eRow, eCol := e.selectionBounds()

	if row < sRow || row > eRow {
		return false
	}
	if row == sRow && row == eRow {
		return col >= sCol && col < eCol
	}
	if row == sRow {
		return col >= sCol
	}
	if row == eRow {
		return col < eCol
	}
	return true
}

// pasteText inserts text at the cursor (or replaces selection).
func (e *fileEditor) pasteText(text string) {
	if text == "" {
		return
	}
	if e.hasSelection {
		e.deleteSelection()
	}
	pasted := strings.Split(text, "\n")
	if len(pasted) == 1 {
		for _, r := range []rune(pasted[0]) {
			e.insertRune(r)
		}
		return
	}
	// Multi-line paste
	for i, part := range pasted {
		for _, r := range []rune(part) {
			e.insertRune(r)
		}
		if i < len(pasted)-1 {
			e.insertNewline()
		}
	}
}

// applyToAllCursors runs fn for the primary cursor, then for each extra cursor
// (adjusting row offsets if prior operations shifted lines). For simplicity,
// the extra cursors are applied in reverse row order to avoid offset issues.
func (e *fileEditor) applyToAllCursors(fn func()) {
	if len(e.extraCursors) == 0 {
		fn()
		return
	}

	// Apply to primary first
	fn()

	// Collect all cursor positions and apply in order
	// This is a simplified implementation — for full correctness a proper
	// multi-cursor engine is needed; this handles same-line cursors naively.
	for i := range e.extraCursors {
		savedPrimary := cursorPos{row: e.cursorRow, col: e.cursorCol}
		e.cursorRow = e.extraCursors[i].row
		e.cursorCol = e.extraCursors[i].col
		fn()
		e.extraCursors[i] = cursorPos{row: e.cursorRow, col: e.cursorCol}
		e.cursorRow = savedPrimary.row
		e.cursorCol = savedPrimary.col
	}
}

// --- Cursor movement ---

func (e *fileEditor) moveCursorUp() {
	if e.cursorRow > 0 {
		e.cursorRow--
		e.clampCursorCol()
	}
}

func (e *fileEditor) moveCursorDown() {
	if e.cursorRow < len(e.lines)-1 {
		e.cursorRow++
		e.clampCursorCol()
	}
}

func (e *fileEditor) moveCursorLeft() {
	if e.cursorCol > 0 {
		e.cursorCol--
	} else if e.cursorRow > 0 {
		e.cursorRow--
		e.cursorCol = e.currentLineLen()
	}
}

func (e *fileEditor) moveCursorRight() {
	if e.cursorCol < e.currentLineLen() {
		e.cursorCol++
	} else if e.cursorRow < len(e.lines)-1 {
		e.cursorRow++
		e.cursorCol = 0
	}
}

func (e *fileEditor) clampCursorCol() {
	lineLen := e.currentLineLen()
	if e.cursorCol > lineLen {
		e.cursorCol = lineLen
	}
}

func (e *fileEditor) currentLineLen() int {
	if e.cursorRow >= len(e.lines) {
		return 0
	}
	return len([]rune(e.lines[e.cursorRow]))
}

// --- Editing operations ---

func (e *fileEditor) insertRune(r rune) {
	if e.cursorRow >= len(e.lines) {
		e.lines = append(e.lines, "")
	}

	line := []rune(e.lines[e.cursorRow])
	col := e.cursorCol
	if col > len(line) {
		col = len(line)
	}

	newLine := make([]rune, 0, len(line)+1)
	newLine = append(newLine, line[:col]...)
	newLine = append(newLine, r)
	newLine = append(newLine, line[col:]...)

	e.lines[e.cursorRow] = string(newLine)
	e.cursorCol = col + 1
}

func (e *fileEditor) insertNewline() {
	if e.cursorRow >= len(e.lines) {
		e.lines = append(e.lines, "")
		e.cursorRow = len(e.lines) - 1
		e.cursorCol = 0
		return
	}

	line := []rune(e.lines[e.cursorRow])
	col := e.cursorCol
	if col > len(line) {
		col = len(line)
	}

	before := string(line[:col])
	after := string(line[col:])

	e.lines[e.cursorRow] = before

	newLines := make([]string, 0, len(e.lines)+1)
	newLines = append(newLines, e.lines[:e.cursorRow+1]...)
	newLines = append(newLines, after)
	newLines = append(newLines, e.lines[e.cursorRow+1:]...)
	e.lines = newLines

	e.cursorRow++
	e.cursorCol = 0
}

func (e *fileEditor) deleteBackward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	if e.cursorCol > 0 {
		line := []rune(e.lines[e.cursorRow])
		col := e.cursorCol
		if col > len(line) {
			col = len(line)
		}
		newLine := make([]rune, 0, len(line)-1)
		newLine = append(newLine, line[:col-1]...)
		newLine = append(newLine, line[col:]...)
		e.lines[e.cursorRow] = string(newLine)
		e.cursorCol = col - 1
	} else if e.cursorRow > 0 {
		// Merge with previous line
		prevLen := len([]rune(e.lines[e.cursorRow-1]))
		e.lines[e.cursorRow-1] = e.lines[e.cursorRow-1] + e.lines[e.cursorRow]
		e.lines = append(e.lines[:e.cursorRow], e.lines[e.cursorRow+1:]...)
		e.cursorRow--
		e.cursorCol = prevLen
	}
}

func (e *fileEditor) deleteForward() {
	if e.cursorRow >= len(e.lines) {
		return
	}

	line := []rune(e.lines[e.cursorRow])
	col := e.cursorCol
	if col > len(line) {
		col = len(line)
	}

	if col < len(line) {
		newLine := make([]rune, 0, len(line)-1)
		newLine = append(newLine, line[:col]...)
		newLine = append(newLine, line[col+1:]...)
		e.lines[e.cursorRow] = string(newLine)
	} else if e.cursorRow < len(e.lines)-1 {
		// Merge with next line
		e.lines[e.cursorRow] = e.lines[e.cursorRow] + e.lines[e.cursorRow+1]
		e.lines = append(e.lines[:e.cursorRow+1], e.lines[e.cursorRow+2:]...)
	}
}

// --- Highlighting ---

func (e *fileEditor) rehighlight() {
	if len(e.lines) == 0 || e.filePath == "" {
		e.highlightedLines = nil
		return
	}

	content := strings.Join(e.lines, "\n")
	if content == e.lastHighlighted {
		return // no change, cached result still valid
	}

	e.lastHighlighted = content
	highlighted := New(tuitheme.CurrentTheme()).HighlightLines(content, e.filePath)
	e.highlightedLines = alignHighlightedLines(e.lines, highlighted)
}

// --- Viewport management ---

func (e *fileEditor) ensureCursorVisible() {
	if len(e.lines) == 0 {
		e.viewport.SetYOffset(0)
		return
	}

	visibleHeight := max(e.viewport.Height, 1)
	yOffset := e.viewport.YOffset

	if e.cursorRow < yOffset {
		yOffset = e.cursorRow
	} else if e.cursorRow >= yOffset+visibleHeight {
		yOffset = e.cursorRow - visibleHeight + 1
	}

	maxOffset := max(len(e.lines)-visibleHeight, 0)
	if yOffset < 0 {
		yOffset = 0
	}
	if yOffset > maxOffset {
		yOffset = maxOffset
	}
	e.viewport.SetYOffset(yOffset)
}

func (e *fileEditor) refreshViewport() {
	e.viewport.SetContent(e.renderContent())
}

func (e *fileEditor) renderContent() string {
	t := tuitheme.CurrentTheme()

	switch {
	case e.loading:
		name := filepath.Base(e.filePath)
		if name == "" || name == "." {
			name = "file"
		}
		return styles.BaseStyle().Foreground(t.TextMuted()).Render("Loading " + name + "...")
	case e.loadErr != nil:
		return styles.BaseStyle().Foreground(t.Error()).Render(e.loadErr.Error())
	case e.filePath == "" && len(e.lines) == 1 && e.lines[0] == "":
		return styles.BaseStyle().Foreground(t.TextMuted()).Render("Open a file to edit it here.")
	case len(e.lines) == 0:
		return ""
	}

	var builder strings.Builder
	for i, rawLine := range e.lines {
		// Get highlighted version if available
		renderedLine := rawLine
		if i < len(e.highlightedLines) && e.highlightedLines[i] != "" {
			renderedLine = e.highlightedLines[i]
		}

		isCurrentLine := i == e.cursorRow
		isExtraCursor := e.isExtraCursorRow(i)
		gutter := e.renderGutter(i, isCurrentLine || isExtraCursor)
		line := e.decorateLine(renderedLine, rawLine, i, isCurrentLine, isExtraCursor, t)

		builder.WriteString(gutter)
		builder.WriteString(line)
		if i < len(e.lines)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}

func (e *fileEditor) isExtraCursorRow(row int) bool {
	for _, c := range e.extraCursors {
		if c.row == row {
			return true
		}
	}
	return false
}

func (e *fileEditor) extraCursorColForRow(row int) int {
	for _, c := range e.extraCursors {
		if c.row == row {
			return c.col
		}
	}
	return 0
}

func (e *fileEditor) renderGutter(lineIndex int, isCurrentLine bool) string {
	t := tuitheme.CurrentTheme()
	lineStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(t.Background()).
		PaddingRight(1)

	if isCurrentLine {
		lineStyle = lineStyle.
			Foreground(t.Primary()).
			Background(t.BackgroundSecondary()).
			Bold(true)
	}

	return lineStyle.Render(fmt.Sprintf("%*d", e.gutterWidth(), lineIndex+1))
}

func (e *fileEditor) decorateLine(highlighted, raw string, lineIdx int, isCurrentLine, isExtraCursor bool, t tuitheme.Theme) string {
	runes := []rune(raw)
	selStyle := lipgloss.NewStyle().
		Background(t.SelectionBackground()).
		Foreground(t.SelectionForeground())

	// Build per-character styling for selection
	hasAnySelection := e.hasSelection

	if !isCurrentLine && !isExtraCursor && !hasAnySelection {
		return highlighted
	}

	if hasAnySelection && !isCurrentLine && !isExtraCursor {
		// Non-cursor line that may be partly/fully selected
		return e.renderLineWithSelection(runes, highlighted, lineIdx, selStyle, t)
	}

	if isCurrentLine {
		col := e.cursorCol
		if col > len(runes) {
			col = len(runes)
		}

		cursorStyle := lipgloss.NewStyle().
			Background(t.Primary()).
			Foreground(t.Background())

		bgStyle := lipgloss.NewStyle().
			Background(t.BackgroundSecondary()).
			Foreground(t.Text())

		// If selection is active, render selection highlighting on this line too
		if hasAnySelection {
			return e.renderLineWithCursorAndSelection(runes, highlighted, lineIdx, col, cursorStyle, selStyle, bgStyle, t)
		}

		// Normal current line rendering with block cursor
		return e.renderCursorLine(runes, col, cursorStyle, bgStyle)
	}

	if isExtraCursor {
		extraCol := e.extraCursorColForRow(lineIdx)
		if extraCol > len(runes) {
			extraCol = len(runes)
		}
		cursorStyle := lipgloss.NewStyle().
			Background(t.Primary()).
			Foreground(t.Background()).
			Faint(true)
		bgStyle := lipgloss.NewStyle().
			Background(t.BackgroundSecondary()).
			Foreground(t.Text()).
			Faint(true)
		return e.renderCursorLine(runes, extraCol, cursorStyle, bgStyle)
	}

	return highlighted
}

func (e *fileEditor) renderCursorLine(runes []rune, col int, cursorStyle, bgStyle lipgloss.Style) string {
	if col < len(runes) {
		before := bgStyle.Render(string(runes[:col]))
		cursor := cursorStyle.Render(string(runes[col : col+1]))
		after := bgStyle.Render(string(runes[col+1:]))
		return before + cursor + after
	}
	// Cursor at end of line
	before := bgStyle.Render(string(runes))
	cursor := cursorStyle.Render(" ")
	return before + cursor
}

func (e *fileEditor) renderLineWithSelection(runes []rune, _ string, lineIdx int, selStyle lipgloss.Style, t tuitheme.Theme) string {
	normal := lipgloss.NewStyle().Foreground(t.Text())
	var b strings.Builder
	for i, r := range runes {
		if e.isRowColInSelection(lineIdx, i) {
			b.WriteString(selStyle.Render(string(r)))
		} else {
			b.WriteString(normal.Render(string(r)))
		}
	}
	return b.String()
}

func (e *fileEditor) renderLineWithCursorAndSelection(runes []rune, _ string, lineIdx, col int, cursorStyle, selStyle, bgStyle lipgloss.Style, _ tuitheme.Theme) string {
	var b strings.Builder
	for i, r := range runes {
		if i == col {
			b.WriteString(cursorStyle.Render(string(r)))
		} else if e.isRowColInSelection(lineIdx, i) {
			b.WriteString(selStyle.Render(string(r)))
		} else {
			b.WriteString(bgStyle.Render(string(r)))
		}
	}
	if col >= len(runes) {
		b.WriteString(cursorStyle.Render(" "))
	}
	return b.String()
}

func (e *fileEditor) renderStatusLine(t tuitheme.Theme) string {
	leftStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)
	rightStyle := lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)

	name := e.displayName()
	if name == "" {
		name = "No file"
	}
	if e.dirty {
		name = "● " + name
	}

	left := name
	if e.loading {
		left = "Loading " + name
	} else if e.loadErr != nil {
		left = "Error loading " + name
	}

	lineCount := len(e.lines)
	cursorInfo := fmt.Sprintf("Ln %d/%d  Col %d", e.cursorRow+1, lineCount, e.cursorCol+1)
	if lineCount == 0 {
		cursorInfo = ""
	}
	extraInfo := ""
	if len(e.extraCursors) > 0 {
		extraInfo = fmt.Sprintf("  +%d cursors", len(e.extraCursors))
	}
	selInfo := ""
	if e.hasSelection {
		selInfo = "  [SEL]"
	}
	right := cursorInfo + extraInfo + selInfo + "  [EDIT] esc=view"
	if lineCount == 0 {
		right = "[EDIT] esc=view"
	}

	rightRendered := rightStyle.Render(right)
	available := max(e.width-lipgloss.Width(rightRendered), 0)
	left = truncateRunes(left, available)

	leftRendered := leftStyle.Width(max(e.width-lipgloss.Width(rightRendered), 0)).Render(left)
	return lipgloss.JoinHorizontal(lipgloss.Left, leftRendered, rightRendered)
}

func (e *fileEditor) displayName() string {
	if e.filePath == "" {
		return ""
	}
	return filepath.Base(e.filePath)
}

func (e *fileEditor) gutterWidth() int {
	return max(len(fmt.Sprintf("%d", max(len(e.lines), 1))), 2)
}
