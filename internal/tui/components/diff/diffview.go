package diff

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	chansi "github.com/charmbracelet/x/ansi"
	"github.com/digiogithub/pando/internal/tui/components/editor"
	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

type DiffLayout int

const (
	DiffLayoutUnified DiffLayout = iota
	DiffLayoutSplit
)

const (
	defaultContextLines = 3
	headerHeight        = 1
)

// DiffViewOptions configures a new DiffView.
type DiffViewOptions struct {
	Diff         string
	Width        int
	Height       int
	Layout       DiffLayout
	ContextLines int
}

// DiffView renders parsed unified diffs in unified or split mode.
type DiffView struct {
	diffText      string
	fileDiff      *FileDiff
	layout        DiffLayout
	contextLines  int
	width         int
	height        int
	viewport      viewport.Model
	styles        DiffStyles
	highlighter   *editor.Highlighter
	themeIdentity string
	hunkOffsets   []int
	pendingNavKey string
}

type renderItem struct {
	line    *DiffLine
	skipped int
}

type splitRow struct {
	left    *DiffLine
	right   *DiffLine
	skipped int
}

func NewDiffView(opts DiffViewOptions) *DiffView {
	view := &DiffView{
		layout:       DiffLayoutUnified,
		contextLines: defaultContextLines,
		viewport:     viewport.New(0, 0),
	}

	if opts.Layout == DiffLayoutSplit {
		view.layout = DiffLayoutSplit
	}
	if opts.ContextLines > 0 {
		view.contextLines = opts.ContextLines
	}

	view.viewport.MouseWheelEnabled = true
	view.viewport.MouseWheelDelta = 2
	view.viewport.SetHorizontalStep(4)
	view.SetSize(opts.Width, opts.Height)
	view.SetDiff(opts.Diff)
	return view
}

func (d *DiffView) Init() tea.Cmd {
	return d.viewport.Init()
}

func (d *DiffView) SetDiff(diff string) {
	d.diffText = diff
	d.fileDiff = ParseUnifiedDiff(diff)
	d.hunkOffsets = nil
	if d.viewport.YOffset > 0 {
		d.viewport.GotoTop()
	}
}

func (d *DiffView) SetSize(width, height int) {
	d.width = maxInt(width, 0)
	d.height = maxInt(height, 0)
	d.viewport.Width = d.width
	d.viewport.Height = maxInt(d.height-headerHeight, 0)
}

func (d *DiffView) SetContextLines(lines int) {
	if lines < 0 {
		lines = 0
	}
	d.contextLines = lines
}

func (d *DiffView) SetLayout(layout DiffLayout) {
	d.layout = layout
}

func (d *DiffView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()

		if d.handlePendingNav(key) {
			return d, nil
		}

		switch key {
		case "t":
			d.toggleLayout()
			return d, nil
		case "j", "down":
			d.viewport.LineDown(1)
			return d, nil
		case "k", "up":
			d.viewport.LineUp(1)
			return d, nil
		case "ctrl+d":
			d.viewport.HalfPageDown()
			return d, nil
		case "ctrl+u":
			d.viewport.HalfPageUp()
			return d, nil
		case "g", "home":
			d.viewport.GotoTop()
			return d, nil
		case "G", "end":
			d.viewport.GotoBottom()
			return d, nil
		case "]", "[":
			d.pendingNavKey = key
			return d, nil
		}
	}

	d.pendingNavKey = ""
	updated, cmd := d.viewport.Update(msg)
	d.viewport = updated
	return d, cmd
}

func (d *DiffView) View() string {
	d.ensureThemeAssets()
	header := d.renderHeader()
	if d.width <= 0 || d.height <= 0 {
		return header
	}

	content, offsets := d.renderContent(maxInt(d.width, 1))
	d.hunkOffsets = offsets
	d.viewport.Width = d.width
	d.viewport.Height = maxInt(d.height-headerHeight, 0)
	d.viewport.SetContent(content)

	if d.viewport.Height <= 0 {
		return header
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, tuizone.MarkDiffViewport(d.viewport.View()))
}

func (d *DiffView) handlePendingNav(key string) bool {
	if d.pendingNavKey == "" {
		return false
	}

	defer func() {
		d.pendingNavKey = ""
	}()

	if (key != "c" && key != "h") || len(d.hunkOffsets) == 0 {
		return false
	}

	switch d.pendingNavKey {
	case "]":
		d.goToHunk(d.currentHunkIndex() + 1)
		return true
	case "[":
		d.goToHunk(d.currentHunkIndex() - 1)
		return true
	default:
		return false
	}
}

func (d *DiffView) goToHunk(index int) {
	if len(d.hunkOffsets) == 0 {
		return
	}

	if index < 0 {
		index = 0
	}
	if index >= len(d.hunkOffsets) {
		index = len(d.hunkOffsets) - 1
	}
	d.viewport.SetYOffset(d.hunkOffsets[index])
}

func (d *DiffView) currentHunkIndex() int {
	if len(d.hunkOffsets) == 0 {
		return 0
	}

	current := d.viewport.YOffset
	index := 0
	for i, offset := range d.hunkOffsets {
		if offset > current {
			break
		}
		index = i
	}
	return index
}

func (d *DiffView) toggleLayout() {
	if d.layout == DiffLayoutUnified {
		d.layout = DiffLayoutSplit
		return
	}
	d.layout = DiffLayoutUnified
}

func (d *DiffView) renderHeader() string {
	d.ensureThemeAssets()
	if d.width <= 0 {
		return ""
	}

	filePath := "No diff loaded"
	additions, deletions := 0, 0
	if d.fileDiff != nil {
		if d.fileDiff.FilePath != "" {
			filePath = d.fileDiff.FilePath
		}
		additions = d.fileDiff.Additions
		deletions = d.fileDiff.Deletions
	}

	layoutLabel := "UNIFIED"
	if d.layout == DiffLayoutSplit {
		layoutLabel = "SPLIT"
	}

	title := d.styles.HeaderPath.Render(filePath)
	stats := d.styles.HeaderStats.Render(
		fmt.Sprintf(" %s %d  %s %d ", d.styles.AddedMarker.Render("+"), additions, d.styles.RemovedMarker.Render("-"), deletions),
	)
	mode := d.styles.HeaderMode.Render(layoutLabel)
	content := lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", stats, " ", mode)

	if lipgloss.Width(content) > d.width {
		content = chansi.Truncate(content, d.width, d.styles.HeaderStats.Render("..."))
	}

	padding := d.width - lipgloss.Width(content)
	if padding < 0 {
		padding = 0
	}

	return d.styles.Header.Width(d.width).Render(content + strings.Repeat(" ", padding))
}

func (d *DiffView) renderContent(width int) (string, []int) {
	if d.fileDiff == nil || len(d.fileDiff.Hunks) == 0 {
		return d.placeholderContent(), nil
	}

	if d.layout == DiffLayoutSplit {
		return d.renderSplit(width)
	}
	return d.renderUnified(width)
}

func (d *DiffView) renderUnified(width int) (string, []int) {
	var (
		lines   []string
		offsets []int
	)

	oldDigits, newDigits := d.lineNumberDigits()
	for hunkIndex, hunk := range d.fileDiff.Hunks {
		if hunkIndex > 0 {
			lines = append(lines, "")
		}
		offsets = append(offsets, len(lines))
		lines = append(lines, d.renderFullWidth(hunk.Header, d.styles.HunkHeader, width))

		for _, item := range d.visibleItems(hunk) {
			if item.skipped > 0 {
				lines = append(lines, d.renderGap(item.skipped, width))
				continue
			}
			lines = append(lines, d.renderUnifiedLine(*item.line, oldDigits, newDigits, width))
		}
	}

	return strings.Join(lines, "\n"), offsets
}

func (d *DiffView) renderSplit(width int) (string, []int) {
	var (
		lines   []string
		offsets []int
	)

	oldDigits, newDigits := d.lineNumberDigits()
	separator := d.styles.Separator.Render(" │ ")
	separatorWidth := lipgloss.Width(separator)
	leftWidth := maxInt((width-separatorWidth)/2, 1)
	rightWidth := maxInt(width-separatorWidth-leftWidth, 1)

	for hunkIndex, hunk := range d.fileDiff.Hunks {
		if hunkIndex > 0 {
			lines = append(lines, "")
		}
		offsets = append(offsets, len(lines))
		lines = append(lines, d.renderFullWidth(hunk.Header, d.styles.HunkHeader, width))

		for _, row := range d.splitRows(hunk) {
			if row.skipped > 0 {
				lines = append(lines, d.renderGap(row.skipped, width))
				continue
			}

			left := d.renderSplitColumn(row.left, true, oldDigits, leftWidth)
			right := d.renderSplitColumn(row.right, false, newDigits, rightWidth)
			lines = append(lines, left+separator+right)
		}
	}

	return strings.Join(lines, "\n"), offsets
}

func (d *DiffView) renderUnifiedLine(line DiffLine, oldDigits, newDigits, width int) string {
	lineStyle, oldStyle, newStyle, markerStyle, marker := d.lineStyles(line.Type)

	oldText := d.renderLineNumber(line.OldNum, oldDigits, oldStyle)
	newText := d.renderLineNumber(line.NewNum, newDigits, newStyle)
	prefix := lipgloss.JoinHorizontal(
		lipgloss.Left,
		oldText,
		d.styles.ContextLine.Render(" "),
		newText,
		d.styles.ContextLine.Render(" "),
		markerStyle.Render(marker),
		d.styles.ContextLine.Render(" "),
	)

	contentWidth := maxInt(width-lipgloss.Width(prefix), 0)
	content := d.renderHighlightedContent(line, lineStyle, contentWidth)
	return d.renderFullWidth(prefix+content, lineStyle, width)
}

func (d *DiffView) renderSplitColumn(line *DiffLine, isLeft bool, digits, width int) string {
	if line == nil {
		return d.renderFullWidth("", d.styles.ContextLine, width)
	}

	lineStyle, numberStyle, markerStyle, marker := d.columnStyles(line.Type, isLeft)
	lineNumber := line.NewNum
	if isLeft {
		lineNumber = line.OldNum
	}

	prefix := lipgloss.JoinHorizontal(
		lipgloss.Left,
		d.renderLineNumber(lineNumber, digits, numberStyle),
		d.styles.ContextLine.Render(" "),
		markerStyle.Render(marker),
		d.styles.ContextLine.Render(" "),
	)

	contentWidth := maxInt(width-lipgloss.Width(prefix), 0)
	content := d.renderHighlightedContent(*line, lineStyle, contentWidth)
	return d.renderFullWidth(prefix+content, lineStyle, width)
}

func (d *DiffView) renderHighlightedContent(line DiffLine, lineStyle lipgloss.Style, width int) string {
	if width <= 0 {
		return ""
	}

	content := line.Content
	if d.fileDiff != nil {
		content = d.ensureHighlighter().HighlightLine(line.Content, d.fileDiff.FilePath)
		content = tuistyles.ForceReplaceBackgroundWithLipgloss(content, lineStyle.GetBackground())
	}

	content = chansi.Cut(content, 0, width)
	if lipgloss.Width(content) > width {
		content = chansi.Truncate(content, width, "...")
	}

	padding := width - lipgloss.Width(content)
	if padding > 0 {
		content += strings.Repeat(" ", padding)
	}
	return content
}

func (d *DiffView) renderLineNumber(value, digits int, style lipgloss.Style) string {
	if value <= 0 {
		return style.Render(strings.Repeat(" ", digits))
	}
	return style.Render(fmt.Sprintf("%*d", digits, value))
}

func (d *DiffView) renderGap(skipped, width int) string {
	text := fmt.Sprintf("⋯ %d unchanged lines ⋯", skipped)
	return d.renderFullWidth(text, d.styles.Gap, width)
}

func (d *DiffView) renderFullWidth(text string, style lipgloss.Style, width int) string {
	if width <= 0 {
		return ""
	}

	if lipgloss.Width(text) > width {
		text = chansi.Truncate(text, width, "...")
	}

	padding := width - lipgloss.Width(text)
	if padding < 0 {
		padding = 0
	}

	return style.Width(width).Render(text + strings.Repeat(" ", padding))
}

func (d *DiffView) placeholderContent() string {
	if d.viewport.Height <= 0 {
		return ""
	}

	lines := make([]string, d.viewport.Height)
	message := "No diff content available"
	lines[0] = d.renderFullWidth(message, d.styles.Placeholder, maxInt(d.width, 1))
	for i := 1; i < len(lines); i++ {
		lines[i] = d.renderFullWidth("", d.styles.ContextLine, maxInt(d.width, 1))
	}
	return strings.Join(lines, "\n")
}

func (d *DiffView) visibleItems(hunk Hunk) []renderItem {
	if len(hunk.Lines) == 0 {
		items := make([]renderItem, 0, len(hunk.Lines))
		for i := range hunk.Lines {
			items = append(items, renderItem{line: &hunk.Lines[i]})
		}
		return items
	}

	ranges := d.visibleRanges(hunk.Lines)
	if len(ranges) == 0 {
		items := make([]renderItem, 0, len(hunk.Lines))
		for i := range hunk.Lines {
			items = append(items, renderItem{line: &hunk.Lines[i]})
		}
		return items
	}

	items := make([]renderItem, 0, len(hunk.Lines))
	nextStart := 0
	for _, visible := range ranges {
		if visible.start > nextStart {
			items = append(items, renderItem{skipped: visible.start - nextStart})
		}
		for i := visible.start; i <= visible.end; i++ {
			items = append(items, renderItem{line: &hunk.Lines[i]})
		}
		nextStart = visible.end + 1
	}

	if nextStart < len(hunk.Lines) {
		items = append(items, renderItem{skipped: len(hunk.Lines) - nextStart})
	}
	return items
}

func (d *DiffView) splitRows(hunk Hunk) []splitRow {
	items := d.visibleItems(hunk)
	rows := make([]splitRow, 0, len(items))

	for i := 0; i < len(items); i++ {
		item := items[i]
		if item.skipped > 0 {
			rows = append(rows, splitRow{skipped: item.skipped})
			continue
		}

		line := item.line
		switch line.Type {
		case DiffLineDelete:
			if i+1 < len(items) && items[i+1].skipped == 0 && items[i+1].line.Type == DiffLineAdd {
				rows = append(rows, splitRow{left: line, right: items[i+1].line})
				i++
			} else {
				rows = append(rows, splitRow{left: line})
			}
		case DiffLineAdd:
			rows = append(rows, splitRow{right: line})
		default:
			rows = append(rows, splitRow{left: line, right: line})
		}
	}

	return rows
}

func (d *DiffView) visibleRanges(lines []DiffLine) []lineRange {
	var (
		ranges []lineRange
		start  = -1
	)

	for i, line := range lines {
		if line.Type == DiffLineContext {
			if start >= 0 {
				ranges = append(ranges, lineRange{
					start: maxInt(start-d.contextLines, 0),
					end:   minInt(i-1+d.contextLines, len(lines)-1),
				})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}

	if start >= 0 {
		ranges = append(ranges, lineRange{
			start: maxInt(start-d.contextLines, 0),
			end:   len(lines) - 1,
		})
	}

	if len(ranges) == 0 {
		return nil
	}

	merged := []lineRange{ranges[0]}
	for _, current := range ranges[1:] {
		last := &merged[len(merged)-1]
		if current.start <= last.end+1 {
			if current.end > last.end {
				last.end = current.end
			}
			continue
		}
		merged = append(merged, current)
	}

	return merged
}

func (d *DiffView) lineNumberDigits() (int, int) {
	maxOld, maxNew := 1, 1
	if d.fileDiff == nil {
		return maxOld, maxNew
	}

	for _, hunk := range d.fileDiff.Hunks {
		for _, line := range hunk.Lines {
			if line.OldNum > maxOld {
				maxOld = line.OldNum
			}
			if line.NewNum > maxNew {
				maxNew = line.NewNum
			}
		}
	}

	return digitCount(maxOld), digitCount(maxNew)
}

func (d *DiffView) lineStyles(kind DiffLineType) (lipgloss.Style, lipgloss.Style, lipgloss.Style, lipgloss.Style, string) {
	switch kind {
	case DiffLineAdd:
		return d.styles.AddedLine, d.styles.ContextNumber, d.styles.AddedNumber, d.styles.AddedMarker, "+"
	case DiffLineDelete:
		return d.styles.RemovedLine, d.styles.RemovedNumber, d.styles.ContextNumber, d.styles.RemovedMarker, "-"
	default:
		return d.styles.ContextLine, d.styles.ContextNumber, d.styles.ContextNumber, d.styles.ContextMarker, " "
	}
}

func (d *DiffView) columnStyles(kind DiffLineType, isLeft bool) (lipgloss.Style, lipgloss.Style, lipgloss.Style, string) {
	switch kind {
	case DiffLineAdd:
		if isLeft {
			return d.styles.ContextLine, d.styles.ContextNumber, d.styles.ContextMarker, " "
		}
		return d.styles.AddedLine, d.styles.AddedNumber, d.styles.AddedMarker, "+"
	case DiffLineDelete:
		if !isLeft {
			return d.styles.ContextLine, d.styles.ContextNumber, d.styles.ContextMarker, " "
		}
		return d.styles.RemovedLine, d.styles.RemovedNumber, d.styles.RemovedMarker, "-"
	default:
		return d.styles.ContextLine, d.styles.ContextNumber, d.styles.ContextMarker, " "
	}
}

func (d *DiffView) ensureThemeAssets() {
	currentTheme := tuitheme.CurrentTheme()
	identity := fmt.Sprintf("%T:%p", currentTheme, currentTheme)
	if d.themeIdentity == identity && d.highlighter != nil {
		return
	}

	d.themeIdentity = identity
	d.styles = NewDiffStyles(currentTheme)
	d.highlighter = editor.New(currentTheme)
}

func (d *DiffView) ensureHighlighter() *editor.Highlighter {
	d.ensureThemeAssets()
	return d.highlighter
}

type lineRange struct {
	start int
	end   int
}

func digitCount(value int) int {
	if value <= 0 {
		return 1
	}
	count := 0
	for value > 0 {
		count++
		value /= 10
	}
	return count
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
