package settings

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// lockedFieldMarker precedes the value of a field whose configuration key an
// extension has locked. It is plain ASCII rather than a glyph so that it reads
// the same on a terminal without a patched font.
const lockedFieldMarker = "[managed]"

type SaveFieldMsg struct {
	SectionTitle string
	Field        Field
}

type OpenModelFieldDialogMsg struct {
	SectionTitle string
	Field        Field
}

type Section struct {
	Title          string
	Group          string // optional group label shown as header in sidebar
	Fields         []Field
	activeFieldIdx int
	width          int
	editor         fieldEditor
}

type renderPlan struct {
	fieldViews   []string
	fieldHeights []int
	fieldOffsets []int
	lineToField  []int
}

func (s *Section) SetWidth(width int) {
	s.width = max(1, width)
	if s.editor != nil {
		s.editor.SetWidth(s.editorWidth())
	}
}

func (s *Section) IsEditing() bool {
	return s.editor != nil
}

func (s *Section) ActiveField() *Field {
	if s.ensureActiveFieldIdx() < 0 {
		return nil
	}

	return &s.Fields[s.activeFieldIdx]
}

func (s *Section) Update(msg tea.Msg) tea.Cmd {
	if len(s.Fields) == 0 {
		return nil
	}

	if s.editor != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "esc":
				s.editor = nil
				return nil
			case "ctrl+s":
				s.Fields[s.activeFieldIdx].Value = s.editor.Value()
				s.editor = nil
				return s.saveActiveField()
			}
		}

		cmd, done := s.editor.Update(msg)
		if done {
			s.Fields[s.activeFieldIdx].Value = s.editor.Value()
			s.editor = nil
			return tea.Batch(cmd, s.saveActiveField())
		}

		return cmd
	}

	if s.ensureActiveFieldIdx() < 0 {
		return nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch keyMsg.String() {
	case "up", "k":
		s.moveActiveField(-1)
	case "down", "j":
		s.moveActiveField(1)
	case "enter":
		field := s.ActiveField()
		if field != nil && field.Type == FieldAction {
			savedField := *field
			return func() tea.Msg {
				return SaveFieldMsg{SectionTitle: s.Title, Field: savedField}
			}
		}
		return s.startEditing()
	case "ctrl+s":
		return s.saveActiveField()
	}

	return nil
}

func (s *Section) View(width int, active bool) string {
	s.SetWidth(width)

	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	if len(s.Fields) == 0 {
		return base.
			Foreground(t.TextMuted()).
			Padding(1, 0).
			Render("No settings in this section.")
	}

	return lipgloss.JoinVertical(lipgloss.Left, s.renderFields(width, active)...)
}

// renderFields is the single source of truth for the rendered rows and their
// geometry. View, hit-testing, scroll restoration, and auto-scroll all derive
// their offsets from this exact layout so callers stay in sync with hints,
// wrapped notes, and shared card borders.
func (s *Section) renderFields(width int, active bool) []string {
	return s.renderPlan(width, active).fieldViews
}

// FieldHeights returns the rendered line height of every field at the given
// width, matching exactly what View draws.
func (s *Section) FieldHeights(width int) []int {
	plan := s.renderPlan(width, true)
	return append([]int(nil), plan.fieldHeights...)
}

// FieldLineOffset returns the first content line (0-indexed) of field idx.
func (s *Section) FieldLineOffset(width, idx int) int {
	plan := s.renderPlan(width, true)
	if idx < 0 || idx >= len(plan.fieldOffsets) {
		return 0
	}
	return plan.fieldOffsets[idx]
}

// FieldAtLine maps a content line to the focusable field index that occupies
// it, or to the nearest focusable row of the enclosing card for card border
// decoration lines. Non-focusable decoration elsewhere resolves to -1.
func (s *Section) FieldAtLine(width, line int) int {
	if line < 0 {
		return -1
	}
	plan := s.renderPlan(width, true)
	if line >= len(plan.lineToField) {
		return -1
	}
	return plan.lineToField[line]
}

// ActiveFieldIdx returns the index of the currently active field, or -1 when
// the section has no focusable rows.
func (s *Section) ActiveFieldIdx() int {
	return s.ensureActiveFieldIdx()
}

// SetActiveFieldIdx sets the active field, clamped to the nearest focusable
// row. When the section has no focusable rows, it leaves the section with no
// active field.
func (s *Section) SetActiveFieldIdx(idx int) {
	if len(s.Fields) == 0 {
		s.activeFieldIdx = -1
		return
	}

	if first := s.firstFocusableIdx(); first < 0 {
		s.activeFieldIdx = -1
		return
	}

	idx = min(max(idx, 0), len(s.Fields)-1)
	if s.Fields[idx].Focusable() {
		s.activeFieldIdx = idx
		return
	}

	if next := s.nextFocusableIdx(idx); next >= 0 {
		s.activeFieldIdx = next
		return
	}

	s.activeFieldIdx = s.prevFocusableIdx(idx)
}

func (s *Section) startEditing() tea.Cmd {
	field := s.ActiveField()
	if field == nil || !field.Editable() {
		return nil
	}

	switch field.Type {
	case FieldToggle:
		editor := NewToggleFieldCmp(*field)
		s.editor = &editor
	case FieldSelect:
		if field.UseModelDialog {
			s.editor = nil
			openedField := *field
			return func() tea.Msg {
				return OpenModelFieldDialogMsg{SectionTitle: s.Title, Field: openedField}
			}
		}
		editor := NewSelectFieldCmp(*field, s.editorWidth())
		s.editor = &editor
	default:
		editor := NewTextFieldCmp(*field, s.editorWidth())
		s.editor = &editor
		return textinput.Blink
	}

	if s.editor != nil {
		s.editor.SetWidth(s.editorWidth())
	}

	return nil
}

func (s Section) labelWidth() int {
	return min(24, max(14, s.width/3))
}

func (s Section) valueWidth() int {
	return max(12, s.width-s.labelWidth()-6)
}

func (s Section) editorWidth() int {
	return max(10, s.valueWidth())
}

func (s *Section) saveActiveField() tea.Cmd {
	field := s.ActiveField()
	if field == nil || !field.Editable() {
		return nil
	}

	savedField := *field
	return func() tea.Msg {
		return SaveFieldMsg{
			SectionTitle: s.Title,
			Field:        savedField,
		}
	}
}

func (s *Section) renderPlan(width int, active bool) renderPlan {
	s.SetWidth(width)
	activeIdx := s.ensureActiveFieldIdx()
	plan := renderPlan{
		fieldViews:   make([]string, len(s.Fields)),
		fieldHeights: make([]int, len(s.Fields)),
		fieldOffsets: make([]int, len(s.Fields)),
	}

	width = max(1, width)
	for i := 0; i < len(s.Fields); {
		card := strings.TrimSpace(s.Fields[i].Card)
		if card == "" {
			lines, lineMap := s.renderStandaloneField(i, width, active && i == activeIdx)
			plan.addField(i, lines, lineMap)
			i++
			continue
		}

		end := i + 1
		for end < len(s.Fields) && sameCardRun(s.Fields[i], s.Fields[end]) {
			end++
		}
		s.renderCardRun(&plan, width, i, end, active, activeIdx)
		i = end
	}

	return plan
}

func (p *renderPlan) addField(fieldIdx int, lines []string, lineMap []int) {
	p.fieldOffsets[fieldIdx] = len(p.lineToField)
	p.fieldHeights[fieldIdx] = len(lines)
	p.fieldViews[fieldIdx] = strings.Join(lines, "\n")
	p.lineToField = append(p.lineToField, lineMap...)
}

func (s *Section) renderStandaloneField(idx, width int, activeRow bool) ([]string, []int) {
	field := s.Fields[idx]
	lines := s.renderFieldContent(field, width, activeRow, s.isEditingField(idx))
	lineMap := make([]int, len(lines))
	mapTo := idx
	if !field.Focusable() {
		mapTo = -1
	}
	for i := range lineMap {
		lineMap[i] = mapTo
	}
	return lines, lineMap
}

func (s *Section) renderCardRun(plan *renderPlan, width, start, end int, active bool, activeIdx int) {
	border := lipgloss.RoundedBorder()
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	innerWidth := max(1, width-2)
	cardActive := active && activeIdx >= start && activeIdx < end
	borderStyle := base.Foreground(t.BorderNormal())
	if cardActive {
		borderStyle = borderStyle.Foreground(t.BorderFocused())
	}

	firstFocusable := -1
	lastFocusable := -1
	for i := start; i < end; i++ {
		if !s.Fields[i].Focusable() {
			continue
		}
		if firstFocusable < 0 {
			firstFocusable = i
		}
		lastFocusable = i
	}

	for i := start; i < end; i++ {
		field := s.Fields[i]
		rowLines := s.renderFieldContent(field, innerWidth, active && i == activeIdx, s.isEditingField(i))
		decorated := make([]string, 0, len(rowLines)+2)
		lineMap := make([]int, 0, len(rowLines)+2)

		if i == start {
			decorated = append(decorated, s.renderCardTopLine(innerWidth, field.Card, firstCardStatus(s.Fields[start:end]), borderStyle, border))
			lineMap = append(lineMap, firstFocusable)
		}

		mapTo := -1
		if field.Focusable() {
			mapTo = i
		}
		for _, line := range rowLines {
			left := borderStyle.Render(border.Left)
			right := borderStyle.Render(border.Right)
			decorated = append(decorated, left+base.Width(innerWidth).Render(line)+right)
			lineMap = append(lineMap, mapTo)
		}

		if i == end-1 {
			decorated = append(decorated, borderStyle.Render(border.BottomLeft+strings.Repeat(border.Bottom, innerWidth)+border.BottomRight))
			lineMap = append(lineMap, lastFocusable)
		}

		plan.addField(i, decorated, lineMap)
	}
}

func (s *Section) renderCardTopLine(innerWidth int, title, status string, borderStyle lipgloss.Style, border lipgloss.Border) string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	titleStyle := base.Foreground(t.Primary()).Bold(true)
	statusStyle := base.Foreground(t.TextMuted())

	titleText := ""
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		titleText = " " + trimmed + " "
	}
	statusText := ""
	if trimmed := strings.TrimSpace(status); trimmed != "" {
		statusText = " " + trimmed + " "
	}

	titleText = truncatePlain(titleText, innerWidth)
	remaining := innerWidth - lipgloss.Width(titleText)
	statusText = truncatePlain(statusText, max(0, remaining))
	remaining = max(0, innerWidth-lipgloss.Width(titleText)-lipgloss.Width(statusText))

	return borderStyle.Render(border.TopLeft) +
		titleStyle.Render(titleText) +
		borderStyle.Render(strings.Repeat(border.Top, remaining)) +
		statusStyle.Render(statusText) +
		borderStyle.Render(border.TopRight)
}

func (s *Section) renderFieldContent(field Field, width int, activeRow bool, editing bool) []string {
	switch field.Type {
	case FieldHeader:
		return s.renderHeaderField(field, width)
	case FieldNote:
		return s.renderNoteField(field, width)
	case FieldAction:
		return s.renderActionField(field, width, activeRow)
	default:
		return s.renderValueField(field, width, activeRow, editing)
	}
}

func (s *Section) renderHeaderField(field Field, width int) []string {
	glyphs := currentFieldGlyphs()
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	contentWidth := max(1, width-2)

	label := strings.TrimSpace(field.Label)
	line := glyphs.HeaderRule + glyphs.HeaderRule
	if label != "" {
		line += " " + label + " "
	}
	line = truncatePlain(line, contentWidth)
	if fill := contentWidth - lipgloss.Width(line); fill > 0 {
		line += strings.Repeat(glyphs.HeaderRule, fill)
	}

	return []string{
		clampRenderedLine(base.Render(""), width),
		clampRenderedLine("  "+base.Foreground(t.Primary()).Bold(true).Render(line), width),
	}
}

func (s *Section) renderNoteField(field Field, width int) []string {
	glyphs := currentFieldGlyphs()
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	color := t.Info()
	icon := styles.InfoIcon
	switch field.NoteLevel {
	case NoteLevelWarning:
		color = t.Warning()
		icon = styles.WarningIcon
	case NoteLevelError:
		color = t.Error()
		icon = styles.ErrorIcon
	}

	contentWidth := max(1, width-2)
	prefix := glyphs.NoteBar + " " + icon + " "
	contPrefix := glyphs.NoteBar + "   "
	firstWidth := max(1, contentWidth-lipgloss.Width(prefix))
	nextWidth := max(1, contentWidth-lipgloss.Width(contPrefix))

	lead := strings.TrimSpace(field.Label)
	text := strings.TrimSpace(field.Value)
	leadText := ""
	if lead != "" {
		leadText = lead
		if text != "" {
			leadText += ": "
		}
	}
	leadText = truncatePlain(leadText, firstWidth)
	wrapped := wrapPlainWithWidths(text, max(1, firstWidth-lipgloss.Width(leadText)), nextWidth)
	if text == "" {
		wrapped = []string{""}
	}
	lines := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		bar := base.Foreground(color).Render(glyphs.NoteBar)
		if i == 0 {
			prefixStyled := bar + " " + base.Foreground(color).Bold(true).Render(icon) + " "
			firstLine := "  " + prefixStyled
			if leadText != "" {
				firstLine += base.Foreground(color).Bold(true).Render(leadText)
			}
			if line != "" {
				firstLine += base.Foreground(t.Text()).Render(line)
			}
			lines = append(lines, clampRenderedLine(firstLine, width))
			continue
		}
		lines = append(lines, clampRenderedLine("  "+bar+"   "+base.Foreground(t.Text()).Render(line), width))
	}
	if len(lines) == 0 {
		lines = append(lines, clampRenderedLine("  "+base.Foreground(color).Render(prefix), width))
	}
	return lines
}

func (s *Section) renderActionField(field Field, width int, activeRow bool) []string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	contentWidth := max(1, width-2)
	valueStyle := base.Foreground(t.TextMuted())

	buttonMaxWidth := contentWidth
	if strings.TrimSpace(field.Value) != "" {
		buttonMaxWidth = max(1, min(contentWidth, contentWidth/2+2))
	}
	button := truncatePlain("[ "+field.Label+" ]", buttonMaxWidth)
	buttonStyle := base.Foreground(t.Accent()).Bold(true)
	if field.Disabled || field.Locked {
		buttonStyle = base.Foreground(t.TextMuted())
	}

	marker := field.LockMarker()
	if field.Value == "" {
		text := button
		if marker != "" {
			text = marker + " " + text
		}
		return s.renderBlockLines(width, activeRow, []string{
			buttonStyle.Render(truncatePlain(text, contentWidth)),
		})
	}

	valueText := field.Value
	if marker != "" {
		valueText = marker + " " + valueText
	}
	buttonWidth := lipgloss.Width(button)
	gapWidth := 2
	valueWidth := max(1, contentWidth-buttonWidth-gapWidth)
	line := buttonStyle.Render(button) + strings.Repeat(" ", gapWidth) +
		valueStyle.Width(valueWidth).Align(lipgloss.Right).Render(truncatePlain(valueText, valueWidth))

	return s.renderBlockLines(width, activeRow, []string{line})
}

func (s *Section) renderValueField(field Field, width int, activeRow bool, editing bool) []string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	contentWidth := max(1, width-2)
	if contentWidth < 12 {
		return s.renderCompactValueField(field, width, activeRow, editing)
	}
	labelWidth := s.renderLabelWidth(contentWidth)
	valueWidth := max(1, contentWidth-labelWidth-2)

	labelStyle := base.Foreground(t.TextMuted()).Width(labelWidth)
	if activeRow {
		labelStyle = labelStyle.Foreground(t.Primary()).Bold(true)
	}
	if field.Disabled || field.Locked {
		labelStyle = labelStyle.Foreground(t.TextMuted()).Bold(false)
	}

	valueStyle := base.Foreground(t.TextEmphasized()).Width(valueWidth).Align(lipgloss.Right)
	if field.ReadOnly || field.Disabled || field.Locked {
		valueStyle = valueStyle.Foreground(t.TextMuted())
	}
	if editing {
		valueStyle = base.Width(valueWidth)
	}

	label := labelStyle.Render(padPlain(field.Label, labelWidth))
	if editing {
		valueLines := strings.Split(s.editor.View(), "\n")
		lines := make([]string, 0, len(valueLines)+1)
		for i, line := range valueLines {
			currentLabel := label
			if i > 0 {
				currentLabel = base.Width(labelWidth).Render(strings.Repeat(" ", labelWidth))
			}
			lines = append(lines, currentLabel+"  "+valueStyle.Render(ansi.Truncate(line, valueWidth, "…")))
		}
		return append(s.renderBlockLines(width, activeRow, lines), s.renderHintLines(field, width, activeRow)...)
	}

	value := s.displayFieldValue(field)
	lines := []string{
		label + "  " + valueStyle.Render(truncatePlain(value, valueWidth)),
	}
	return append(s.renderBlockLines(width, activeRow, lines), s.renderHintLines(field, width, activeRow)...)
}

func (s *Section) renderCompactValueField(field Field, width int, activeRow bool, editing bool) []string {
	contentWidth := max(1, width-2)
	lines := []string{""}
	if editing {
		valueLines := strings.Split(s.editor.View(), "\n")
		lines = make([]string, 0, len(valueLines))
		for _, line := range valueLines {
			lines = append(lines, ansi.Truncate(line, contentWidth, "…"))
		}
	} else {
		text := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(s.displayFieldValue(field))
		switch {
		case text == "":
			text = value
		case value != "":
			text = text + ": " + value
		}
		lines = []string{truncatePlain(text, contentWidth)}
	}
	return append(s.renderBlockLines(width, activeRow, lines), s.renderHintLines(field, width, activeRow)...)
}

func (s *Section) renderHintLines(field Field, width int, activeRow bool) []string {
	if !activeRow || strings.TrimSpace(field.Hint) == "" {
		return nil
	}

	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	contentWidth := max(1, width-2)
	hintStyle := base.Foreground(t.TextMuted()).Italic(true)
	wrapped := wrapPlain(strings.TrimSpace(field.Hint), contentWidth)
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, hintStyle.Render(truncatePlain(line, contentWidth)))
	}
	return s.renderBlockLines(width, activeRow, lines)
}

func (s *Section) renderBlockLines(width int, activeRow bool, contentLines []string) []string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	glyphs := currentFieldGlyphs()
	markerWidth := 0
	if width >= 3 {
		markerWidth = 2
	} else if width == 2 {
		markerWidth = 1
	}
	contentWidth := max(1, width-markerWidth)
	rendered := make([]string, 0, len(contentLines))
	for i, line := range contentLines {
		marker := ""
		switch markerWidth {
		case 2:
			marker = "  "
			if activeRow && i == 0 {
				marker = glyphs.ActiveMarker + " "
			}
		case 1:
			marker = " "
			if activeRow && i == 0 {
				marker = glyphs.ActiveMarker
			}
		}

		markerStyle := base.Width(markerWidth)
		contentStyle := base.Width(contentWidth)
		if activeRow {
			markerStyle = markerStyle.Foreground(t.Primary()).Bold(true)
			contentStyle = contentStyle.Foreground(t.SelectionForeground())
			if t.HasBackground() {
				markerStyle = markerStyle.Background(t.SelectionBackground())
				contentStyle = contentStyle.Background(t.SelectionBackground())
			}
		}

		content := ansi.Truncate(line, contentWidth, "…")
		rendered = append(rendered, markerStyle.Render(marker)+contentStyle.Render(content))
	}
	return rendered
}

func (s *Section) displayFieldValue(field Field) string {
	glyphs := currentFieldGlyphs()
	value := field.Value
	if field.Masked && strings.TrimSpace(value) != "" {
		value = "****"
	}

	switch field.Type {
	case FieldToggle:
		if field.BoolValue() {
			return glyphs.ToggleOn + " on"
		}
		return glyphs.ToggleOff + " off"
	case FieldSelect:
		if strings.TrimSpace(value) == "" {
			return glyphs.Dropdown
		}
		return value + " " + glyphs.Dropdown
	default:
		if marker := field.LockMarker(); marker != "" {
			value = marker + " " + value
		}
		if field.ReadOnly || field.Disabled || field.Locked {
			return value
		}
		if strings.TrimSpace(value) == "" {
			return glyphs.EditMarker
		}
		return value + "  " + glyphs.EditMarker
	}
}

func (s *Section) renderLabelWidth(contentWidth int) int {
	contentWidth = max(1, contentWidth)
	labelWidth := min(24, max(1, contentWidth/3))
	if contentWidth >= 26 {
		labelWidth = max(14, labelWidth)
	}
	return min(labelWidth, max(1, contentWidth-3))
}

func (s *Section) ensureActiveFieldIdx() int {
	if len(s.Fields) == 0 {
		s.activeFieldIdx = -1
		return -1
	}
	if s.activeFieldIdx >= 0 && s.activeFieldIdx < len(s.Fields) && s.Fields[s.activeFieldIdx].Focusable() {
		return s.activeFieldIdx
	}
	s.activeFieldIdx = s.firstFocusableIdx()
	return s.activeFieldIdx
}

func (s *Section) firstFocusableIdx() int {
	for i := range s.Fields {
		if s.Fields[i].Focusable() {
			return i
		}
	}
	return -1
}

func (s *Section) nextFocusableIdx(idx int) int {
	for i := idx + 1; i < len(s.Fields); i++ {
		if s.Fields[i].Focusable() {
			return i
		}
	}
	return -1
}

func (s *Section) prevFocusableIdx(idx int) int {
	for i := min(idx-1, len(s.Fields)-1); i >= 0; i-- {
		if s.Fields[i].Focusable() {
			return i
		}
	}
	return -1
}

func (s *Section) moveActiveField(delta int) {
	current := s.ensureActiveFieldIdx()
	if current < 0 {
		return
	}

	idx := current
	for i := 0; i < len(s.Fields); i++ {
		idx = (idx + delta + len(s.Fields)) % len(s.Fields)
		if s.Fields[idx].Focusable() {
			s.activeFieldIdx = idx
			return
		}
	}
}

func (s *Section) isEditingField(idx int) bool {
	return s.editor != nil && idx == s.activeFieldIdx
}

func sameCardRun(left, right Field) bool {
	if strings.TrimSpace(left.Card) == "" || strings.TrimSpace(right.Card) == "" {
		return false
	}
	leftID := strings.TrimSpace(left.CardID)
	rightID := strings.TrimSpace(right.CardID)
	if leftID != "" || rightID != "" {
		return leftID != "" && leftID == rightID
	}
	return left.Card == right.Card
}

func firstCardStatus(fields []Field) string {
	for _, field := range fields {
		if status := strings.TrimSpace(field.CardStatus); status != "" {
			return status
		}
	}
	return ""
}

func clampRenderedLine(line string, width int) string {
	width = max(1, width)
	line = ansi.Truncate(line, width, "")
	if pad := width - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}
