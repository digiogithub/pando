package editor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
	muwrap "github.com/muesli/reflow/wrap"
)

// CloseViewerMsg is emitted when the viewer should be closed.
type CloseViewerMsg struct {
	Path string
}

// FileViewerComponent is the read-only file viewer used by the editor area.
type FileViewerComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
	OpenFile(path string) tea.Cmd
	FilePath() string
}

type fileLoadedMsg struct {
	requestID        int
	path             string
	content          string
	lines            []string
	highlightedLines []string
	binary           bool
	err              error
}

type fileViewer struct {
	width  int
	height int

	viewport    viewport.Model
	searchInput textinput.Model
	keyMap      ViewerKeyMap

	filePath         string
	rawContent       string
	rawLines         []string
	highlightedLines []string
	cursorLine       int

	searchMode    bool
	searchQuery   string
	searchMatches []int
	currentMatch  int

	// Word wrap state
	wordWrap        bool
	visualToRaw     []int // visual line index → raw line index (when wordWrap is true)
	rawToFirstVisual []int // raw line index → first visual line index (when wordWrap is true)
	totalVisualLines int   // total visual line count when wordWrap is true

	loading       bool
	loadErr       error
	binary        bool
	nextRequestID int
	pendingLoadID int
}

// NewFileViewer creates a new read-only file viewer component.
func NewFileViewer() FileViewerComponent {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 2

	search := textinput.New()
	search.Prompt = "/"
	search.CharLimit = 256

	viewer := &fileViewer{
		viewport:     vp,
		searchInput:  search,
		keyMap:       DefaultViewerKeyMap(),
		currentMatch: -1,
	}
	viewer.applySearchInputTheme()
	viewer.refreshViewportContent()

	return viewer
}

func (v *fileViewer) Init() tea.Cmd {
	return nil
}

func (v *fileViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dialog.ThemeChangedMsg:
		v.applySearchInputTheme()
		v.rehighlight()
		v.refreshViewportContent()
		return v, nil

	case fileLoadedMsg:
		if msg.requestID != v.pendingLoadID {
			return v, nil
		}

		v.loading = false
		v.loadErr = msg.err

		if msg.err != nil {
			v.rawContent = ""
			v.rawLines = nil
			v.highlightedLines = nil
			v.binary = false
			v.cursorLine = 0
			v.viewport.SetYOffset(0)
			v.refreshViewportContent()
			return v, util.ReportError(msg.err)
		}

		v.filePath = msg.path
		v.rawContent = msg.content
		v.rawLines = msg.lines
		v.highlightedLines = alignHighlightedLines(msg.lines, msg.highlightedLines)
		v.binary = msg.binary
		v.cursorLine = 0
		v.viewport.SetYOffset(0)
		v.recomputeSearchMatches(false)
		v.refreshViewportContent()
		return v, nil

	case tea.KeyMsg:
		if v.searchMode {
			return v, v.updateSearch(msg)
		}

		switch {
		case key.Matches(msg, v.keyMap.Down):
			v.moveCursor(1)
			return v, nil
		case key.Matches(msg, v.keyMap.Up):
			v.moveCursor(-1)
			return v, nil
		case key.Matches(msg, v.keyMap.HalfPageDown):
			v.moveCursor(v.pageStep())
			return v, nil
		case key.Matches(msg, v.keyMap.HalfPageUp):
			v.moveCursor(-v.pageStep())
			return v, nil
		case key.Matches(msg, v.keyMap.Top):
			v.setCursor(0)
			return v, nil
		case key.Matches(msg, v.keyMap.Bottom):
			v.setCursor(max(v.lineCount()-1, 0))
			return v, nil
		case key.Matches(msg, v.keyMap.Search):
			return v, v.beginSearch()
		case key.Matches(msg, v.keyMap.SearchNext):
			v.navigateMatches(1)
			return v, nil
		case key.Matches(msg, v.keyMap.SearchPrev):
			v.navigateMatches(-1)
			return v, nil
		case key.Matches(msg, v.keyMap.Close):
			return v, util.CmdHandler(CloseViewerMsg{Path: v.filePath})
		case key.Matches(msg, v.keyMap.ToggleWordWrap):
			v.wordWrap = !v.wordWrap
			v.refreshViewportContent()
			v.ensureCursorVisible()
			return v, nil
		}
	}

	updatedViewport, cmd := v.viewport.Update(msg)
	v.viewport = updatedViewport
	v.syncCursorWithViewport()
	v.refreshViewportContent()

	return v, cmd
}

func (v *fileViewer) View() string {
	if v.width <= 0 || v.height <= 0 {
		return ""
	}

	t := tuitheme.CurrentTheme()
	base := styles.BaseStyle().Width(v.width)

	contentHeight := max(v.height-1, 0)
	content := base.
		Width(v.width).
		Height(contentHeight).
		Render(tuizone.MarkViewerViewport(v.viewport.View()))

	status := v.renderStatusLine(t)
	return lipgloss.JoinVertical(lipgloss.Left, content, status)
}

func (v *fileViewer) SetSize(width, height int) tea.Cmd {
	v.width = max(width, 0)
	v.height = max(height, 0)
	v.viewport.Width = v.width
	v.viewport.Height = max(v.height-1, 0)
	v.searchInput.Width = max(v.width-28, 12)
	v.ensureCursorVisible()
	v.refreshViewportContent()
	return nil
}

func (v *fileViewer) GetSize() (int, int) {
	return v.width, v.height
}

func (v *fileViewer) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(v.keyMap)
}

func (v *fileViewer) FilePath() string {
	return v.filePath
}

// OpenFile asynchronously loads a file and applies syntax highlighting.
func (v *fileViewer) OpenFile(path string) tea.Cmd {
	v.nextRequestID++
	v.pendingLoadID = v.nextRequestID
	v.filePath = path
	v.loading = true
	v.loadErr = nil
	v.rawContent = ""
	v.rawLines = nil
	v.highlightedLines = nil
	v.binary = false
	v.cursorLine = 0
	v.cancelSearch()
	v.viewport.SetYOffset(0)
	v.refreshViewportContent()

	requestID := v.pendingLoadID

	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return fileLoadedMsg{
				requestID: requestID,
				path:      path,
				err:       err,
			}
		}

		content := normalizeContent(data)
		lines := splitViewerLines(content)
		highlightedLines := lines

		if !looksBinary(data) {
			highlightedLines = New(tuitheme.CurrentTheme()).HighlightLines(content, path)
		} else {
			content = "Binary file, cannot display"
			lines = []string{content}
			highlightedLines = lines
		}

		return fileLoadedMsg{
			requestID:        requestID,
			path:             path,
			content:          content,
			lines:            lines,
			highlightedLines: alignHighlightedLines(lines, highlightedLines),
			binary:           looksBinary(data),
		}
	}
}

func (v *fileViewer) beginSearch() tea.Cmd {
	v.searchMode = true
	v.searchInput.SetValue(v.searchQuery)
	v.searchInput.CursorEnd()
	return v.searchInput.Focus()
}

func (v *fileViewer) updateSearch(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, v.keyMap.CancelSearch):
		v.cancelSearch()
		v.refreshViewportContent()
		return nil
	case msg.String() == "enter":
		v.searchMode = false
		v.searchInput.Blur()
		v.refreshViewportContent()
		return nil
	}

	updatedInput, cmd := v.searchInput.Update(msg)
	v.searchInput = updatedInput
	v.searchQuery = v.searchInput.Value()
	v.recomputeSearchMatches(true)
	v.refreshViewportContent()

	return cmd
}

func (v *fileViewer) cancelSearch() {
	v.searchMode = false
	v.searchInput.Blur()
	v.searchInput.SetValue("")
	v.searchQuery = ""
	v.searchMatches = nil
	v.currentMatch = -1
}

func (v *fileViewer) moveCursor(delta int) {
	v.setCursor(v.cursorLine + delta)
}

func (v *fileViewer) setCursor(line int) {
	if v.lineCount() == 0 {
		v.cursorLine = 0
		v.viewport.SetYOffset(0)
		v.refreshViewportContent()
		return
	}

	v.cursorLine = util.Clamp(line, 0, v.lineCount()-1)
	v.ensureCursorVisible()
	v.refreshViewportContent()
}

func (v *fileViewer) navigateMatches(delta int) {
	if len(v.searchMatches) == 0 {
		return
	}

	if v.currentMatch < 0 {
		v.currentMatch = 0
	} else {
		v.currentMatch = (v.currentMatch + delta + len(v.searchMatches)) % len(v.searchMatches)
	}

	v.setCursor(v.searchMatches[v.currentMatch])
}

func (v *fileViewer) pageStep() int {
	return max(v.viewport.Height/2, 1)
}

func (v *fileViewer) ensureCursorVisible() {
	if v.lineCount() == 0 {
		v.viewport.SetYOffset(0)
		return
	}

	if v.wordWrap && v.rawToFirstVisual != nil && v.cursorLine < len(v.rawToFirstVisual) {
		visualLine := v.rawToFirstVisual[v.cursorLine]
		visibleHeight := max(v.viewport.Height, 1)
		yOffset := v.viewport.YOffset
		if visualLine < yOffset {
			yOffset = visualLine
		} else if visualLine >= yOffset+visibleHeight {
			yOffset = visualLine - visibleHeight + 1
		}
		maxOffset := max(v.totalVisualLines-visibleHeight, 0)
		v.viewport.SetYOffset(util.Clamp(yOffset, 0, maxOffset))
		return
	}

	visibleHeight := max(v.viewport.Height, 1)
	yOffset := v.viewport.YOffset

	if v.cursorLine < yOffset {
		yOffset = v.cursorLine
	} else if v.cursorLine >= yOffset+visibleHeight {
		yOffset = v.cursorLine - visibleHeight + 1
	}

	maxOffset := max(v.lineCount()-visibleHeight, 0)
	v.viewport.SetYOffset(util.Clamp(yOffset, 0, maxOffset))
}

func (v *fileViewer) syncCursorWithViewport() {
	if v.lineCount() == 0 {
		v.cursorLine = 0
		return
	}

	if v.wordWrap && v.visualToRaw != nil && len(v.visualToRaw) > 0 {
		visibleHeight := max(v.viewport.Height, 1)
		minVis := util.Clamp(v.viewport.YOffset, 0, max(len(v.visualToRaw)-1, 0))
		maxVis := util.Clamp(v.viewport.YOffset+visibleHeight-1, 0, max(len(v.visualToRaw)-1, 0))
		minRaw := v.visualToRaw[minVis]
		maxRaw := v.visualToRaw[maxVis]
		v.cursorLine = util.Clamp(v.cursorLine, minRaw, maxRaw)
		return
	}

	visibleHeight := max(v.viewport.Height, 1)
	minVisible := util.Clamp(v.viewport.YOffset, 0, max(v.lineCount()-1, 0))
	maxVisible := util.Clamp(v.viewport.YOffset+visibleHeight-1, 0, max(v.lineCount()-1, 0))
	v.cursorLine = util.Clamp(v.cursorLine, minVisible, maxVisible)
}

func (v *fileViewer) refreshViewportContent() {
	if v.wordWrap && v.width > 0 {
		content, vToR, rToV := v.buildWrappedContent()
		v.visualToRaw = vToR
		v.rawToFirstVisual = rToV
		v.totalVisualLines = len(vToR)
		v.viewport.SetContent(content)
		return
	}
	v.visualToRaw = nil
	v.rawToFirstVisual = nil
	v.totalVisualLines = len(v.rawLines)
	v.viewport.SetContent(v.renderViewportContent())
}

func (v *fileViewer) renderViewportContent() string {
	t := tuitheme.CurrentTheme()
	base := styles.BaseStyle()

	switch {
	case v.loading:
		name := v.displayName()
		if name == "" {
			name = "file"
		}
		return base.Foreground(t.TextMuted()).Render("Loading " + name + "...")
	case v.loadErr != nil:
		return base.Foreground(t.Error()).Render(v.loadErr.Error())
	case v.filePath == "":
		return base.Foreground(t.TextMuted()).Render("Open a file to preview it here.")
	case len(v.rawLines) == 0:
		return ""
	}

	var builder strings.Builder
	matchLines := make(map[int]struct{}, len(v.searchMatches))
	for _, idx := range v.searchMatches {
		matchLines[idx] = struct{}{}
	}

	for i, rawLine := range v.rawLines {
		renderedLine := rawLine
		if i < len(v.highlightedLines) && v.highlightedLines[i] != "" {
			renderedLine = v.highlightedLines[i]
		}

		_, isMatch := matchLines[i]
		isCurrentLine := i == v.cursorLine
		isCurrentMatch := v.currentMatch >= 0 &&
			v.currentMatch < len(v.searchMatches) &&
			v.searchMatches[v.currentMatch] == i

		gutter := v.renderGutter(i, isCurrentLine, isMatch, isCurrentMatch)
		line := v.decorateLine(renderedLine, isCurrentLine, isMatch, isCurrentMatch, t)
		builder.WriteString(gutter)
		builder.WriteString(line)
		if i < len(v.rawLines)-1 {
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}

// buildWrappedContent renders the file with word-wrapping, building visual↔raw line mappings.
func (v *fileViewer) buildWrappedContent() (string, []int, []int) {
	t := tuitheme.CurrentTheme()
	contentWidth := max(v.width-v.gutterWidth()-2, 1)

	matchLines := make(map[int]struct{}, len(v.searchMatches))
	for _, idx := range v.searchMatches {
		matchLines[idx] = struct{}{}
	}

	var builder strings.Builder
	visualToRaw := make([]int, 0, len(v.rawLines))
	rawToFirstVisual := make([]int, len(v.rawLines))

	contStyle := func() lipgloss.Style {
		return lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Background(t.Background()).
			PaddingRight(1)
	}

	for i, rawLine := range v.rawLines {
		rawToFirstVisual[i] = len(visualToRaw)

		displayLine := rawLine
		if i < len(v.highlightedLines) && v.highlightedLines[i] != "" {
			displayLine = v.highlightedLines[i]
		}

		_, isMatch := matchLines[i]
		isCurrentLine := i == v.cursorLine
		isCurrentMatch := v.currentMatch >= 0 &&
			v.currentMatch < len(v.searchMatches) &&
			v.searchMatches[v.currentMatch] == i

		// Wrap the display line (ANSI-aware)
		var segs []string
		if rawLine == "" {
			segs = []string{displayLine}
		} else {
			wrapped := muwrap.String(displayLine, contentWidth)
			segs = strings.Split(wrapped, "\n")
			if len(segs) == 0 {
				segs = []string{""}
			}
		}

		for segIdx, seg := range segs {
			visualToRaw = append(visualToRaw, i)

			var gutter string
			if segIdx == 0 {
				gutter = v.renderGutter(i, isCurrentLine, isMatch, isCurrentMatch)
			} else {
				gutter = contStyle().Render(strings.Repeat(" ", v.gutterWidth()))
			}

			decoratedSeg := v.decorateLine(seg, isCurrentLine, isMatch, isCurrentMatch, t)
			builder.WriteString(gutter)
			builder.WriteString(decoratedSeg)
			if i < len(v.rawLines)-1 || segIdx < len(segs)-1 {
				builder.WriteByte('\n')
			}
		}
	}

	return builder.String(), visualToRaw, rawToFirstVisual
}

func (v *fileViewer) renderGutter(lineIndex int, isCurrentLine, isMatch, isCurrentMatch bool) string {
	t := tuitheme.CurrentTheme()
	lineStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(t.Background()).
		PaddingRight(1)

	switch {
	case isCurrentMatch:
		lineStyle = lineStyle.Foreground(t.Background()).Background(t.Primary()).Bold(true)
	case isCurrentLine:
		lineStyle = lineStyle.Foreground(t.Primary()).Background(t.BackgroundSecondary()).Bold(true)
	case isMatch:
		lineStyle = lineStyle.Foreground(t.TextEmphasized()).Background(t.BackgroundDarker())
	}

	return lineStyle.Render(fmt.Sprintf("%*d", v.gutterWidth(), lineIndex+1))
}

func (v *fileViewer) decorateLine(line string, isCurrentLine, isMatch, isCurrentMatch bool, t tuitheme.Theme) string {
	switch {
	case isCurrentMatch:
		return applyLineBackground(line, t.Primary(), t.Background())
	case isCurrentLine:
		return applyLineBackground(line, t.BackgroundSecondary(), t.Text())
	case isMatch:
		return applyLineBackground(line, t.BackgroundDarker(), t.Text())
	default:
		return line
	}
}

func (v *fileViewer) renderStatusLine(t tuitheme.Theme) string {
	leftStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)
	rightStyle := lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)

	left := v.displayName()
	if left == "" {
		left = "No file"
	}

	if v.searchMode {
		left = left + "  " + v.searchInput.View()
	} else if v.searchQuery != "" {
		left = left + "  " + v.searchSummary()
	}

	right := fmt.Sprintf("Ln %d/%d", v.currentLineNumber(), v.lineCount())
	if v.wordWrap {
		right = "WRAP  " + right
	}
	if v.loading {
		right = "Loading"
	} else if v.loadErr != nil {
		right = "Error"
	}

	rightRendered := rightStyle.Render(right)
	available := max(v.width-lipgloss.Width(rightRendered), 0)
	left = truncateRunes(left, available)

	leftRendered := leftStyle.Width(max(v.width-lipgloss.Width(rightRendered), 0)).Render(left)
	return lipgloss.JoinHorizontal(lipgloss.Left, leftRendered, rightRendered)
}

func (v *fileViewer) displayName() string {
	if v.filePath == "" {
		return ""
	}
	return filepath.Base(v.filePath)
}

func (v *fileViewer) currentLineNumber() int {
	if v.lineCount() == 0 {
		return 0
	}
	return v.cursorLine + 1
}

func (v *fileViewer) lineCount() int {
	return len(v.rawLines)
}

func (v *fileViewer) gutterWidth() int {
	return max(len(fmt.Sprintf("%d", max(v.lineCount(), 1))), 2)
}

func (v *fileViewer) recomputeSearchMatches(jumpToFirst bool) {
	if v.searchQuery == "" || len(v.rawLines) == 0 {
		v.searchMatches = nil
		v.currentMatch = -1
		return
	}

	needle := strings.ToLower(v.searchQuery)
	matches := make([]int, 0)
	for idx, line := range v.rawLines {
		if strings.Contains(strings.ToLower(line), needle) {
			matches = append(matches, idx)
		}
	}

	v.searchMatches = matches
	if len(matches) == 0 {
		v.currentMatch = -1
		return
	}

	if jumpToFirst || v.currentMatch < 0 || v.currentMatch >= len(matches) {
		v.currentMatch = 0
	}
	v.cursorLine = matches[v.currentMatch]
	v.ensureCursorVisible()
}

func (v *fileViewer) searchSummary() string {
	if v.searchQuery == "" {
		return ""
	}

	if len(v.searchMatches) == 0 || v.currentMatch < 0 {
		return fmt.Sprintf("/%s 0 matches", v.searchQuery)
	}

	return fmt.Sprintf("/%s %d/%d", v.searchQuery, v.currentMatch+1, len(v.searchMatches))
}

func (v *fileViewer) rehighlight() {
	if v.rawContent == "" || v.filePath == "" || len(v.rawLines) == 0 || v.binary {
		return
	}

	v.highlightedLines = alignHighlightedLines(
		v.rawLines,
		New(tuitheme.CurrentTheme()).HighlightLines(v.rawContent, v.filePath),
	)
}

func (v *fileViewer) applySearchInputTheme() {
	t := tuitheme.CurrentTheme()
	v.searchInput.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(t.BackgroundSecondary())
	v.searchInput.PromptStyle = lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(t.BackgroundSecondary())
	v.searchInput.TextStyle = lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(t.BackgroundSecondary())
	v.searchInput.Cursor.Style = lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(t.BackgroundSecondary())
}

func normalizeContent(data []byte) string {
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func splitViewerLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}

func alignHighlightedLines(rawLines, highlightedLines []string) []string {
	if len(rawLines) == len(highlightedLines) {
		lines := make([]string, len(highlightedLines))
		copy(lines, highlightedLines)
		return lines
	}

	lines := make([]string, len(rawLines))
	for i := range rawLines {
		if i < len(highlightedLines) {
			lines[i] = highlightedLines[i]
		} else {
			lines[i] = rawLines[i]
		}
	}
	return lines
}

func applyLineBackground(line string, background, foreground lipgloss.AdaptiveColor) string {
	styled := styles.ForceReplaceBackgroundWithLipgloss(line, background)
	return lipgloss.NewStyle().
		Background(background).
		Foreground(foreground).
		Render(styled)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}

	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func looksBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}
