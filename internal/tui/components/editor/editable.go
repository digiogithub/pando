package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
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
	}
}

type editableFileLoadedMsg struct {
	requestID int
	path      string
	lines     []string
	err       error
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

	cursorRow int
	cursorCol int // column in rune units

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
		e.dirty = false
		e.viewport.SetYOffset(0)
		e.refreshViewport()
		return e, nil

	case tea.KeyMsg:
		return e, e.handleKey(msg)
	}

	updatedVP, cmd := e.viewport.Update(msg)
	e.viewport = updatedVP
	return e, cmd
}

func (e *fileEditor) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, e.keyMap.Save):
		return e.saveFile()

	case key.Matches(msg, e.keyMap.Up):
		e.moveCursorUp()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.Down):
		e.moveCursorDown()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.Left):
		e.moveCursorLeft()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.Right):
		e.moveCursorRight()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.LineStart):
		e.cursorCol = 0
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.LineEnd):
		e.cursorCol = e.currentLineLen()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.PageUp):
		step := max(e.viewport.Height/2, 1)
		e.cursorRow = max(e.cursorRow-step, 0)
		e.clampCursorCol()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.PageDown):
		step := max(e.viewport.Height/2, 1)
		e.cursorRow = min(e.cursorRow+step, max(len(e.lines)-1, 0))
		e.clampCursorCol()
		e.ensureCursorVisible()
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.FileStart):
		e.cursorRow = 0
		e.cursorCol = 0
		e.viewport.SetYOffset(0)
		e.refreshViewport()

	case key.Matches(msg, e.keyMap.FileEnd):
		e.cursorRow = max(len(e.lines)-1, 0)
		e.cursorCol = e.currentLineLen()
		e.ensureCursorVisible()
		e.refreshViewport()

	case msg.String() == "enter":
		e.insertNewline()
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	case msg.String() == "backspace":
		e.deleteBackward()
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	case msg.String() == "delete":
		e.deleteForward()
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	case msg.String() == "tab":
		// Insert a tab as spaces
		for i := 0; i < 4; i++ {
			e.insertRune(' ')
		}
		e.markDirty()
		e.rehighlight()
		e.ensureCursorVisible()
		e.refreshViewport()

	default:
		if len(msg.Runes) == 1 {
			e.insertRune(msg.Runes[0])
			e.markDirty()
			e.rehighlight()
			e.ensureCursorVisible()
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
	content := base.Width(e.width).Height(contentHeight).Render(e.viewport.View())

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
		gutter := e.renderGutter(i, isCurrentLine)
		line := e.decorateLine(renderedLine, rawLine, isCurrentLine, t)

		builder.WriteString(gutter)
		builder.WriteString(line)
		if i < len(e.lines)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
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

func (e *fileEditor) decorateLine(highlighted, raw string, isCurrentLine bool, t tuitheme.Theme) string {
	if !isCurrentLine {
		return highlighted
	}

	// For the current line, render with cursor
	line := applyLineBackground(highlighted, t.BackgroundSecondary(), t.Text())

	// Show a block cursor at cursorCol
	col := e.cursorCol
	runes := []rune(raw)
	if col > len(runes) {
		col = len(runes)
	}

	cursorStyle := lipgloss.NewStyle().
		Background(t.Primary()).
		Foreground(t.Background())

	if col < len(runes) {
		// Cursor is on a character
		before := string(runes[:col])
		cursor := string(runes[col : col+1])
		after := string(runes[col+1:])

		beforeStyled := lipgloss.NewStyle().
			Background(t.BackgroundSecondary()).
			Foreground(t.Text()).
			Render(before)
		cursorStyled := cursorStyle.Render(cursor)
		afterStyled := lipgloss.NewStyle().
			Background(t.BackgroundSecondary()).
			Foreground(t.Text()).
			Render(after)

		_ = line // replaced by explicit cursor rendering
		return beforeStyled + cursorStyled + afterStyled
	}

	// Cursor is at end of line — show a space as cursor
	beforeStyled := lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		Foreground(t.Text()).
		Render(string(runes))
	cursorStyled := cursorStyle.Render(" ")

	_ = line
	return beforeStyled + cursorStyled
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
	right := fmt.Sprintf("Ln %d/%d  Col %d  [EDIT]", e.cursorRow+1, lineCount, e.cursorCol+1)
	if lineCount == 0 {
		right = "[EDIT]"
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
