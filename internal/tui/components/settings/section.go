package settings

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

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
	if len(s.Fields) == 0 {
		return nil
	}

	s.activeFieldIdx = min(max(s.activeFieldIdx, 0), len(s.Fields)-1)
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

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch keyMsg.String() {
	case "up", "k":
		s.activeFieldIdx = (s.activeFieldIdx - 1 + len(s.Fields)) % len(s.Fields)
	case "down", "j":
		s.activeFieldIdx = (s.activeFieldIdx + 1) % len(s.Fields)
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

// renderFields renders each field into its own bordered box. It is the single
// source of truth for both View and the geometry helpers (FieldHeights /
// FieldAtLine / FieldLineOffset), so click hit-testing and auto-scroll agree
// with what is actually drawn even when field heights vary (fields with a Hint
// are taller than fields without one).
func (s *Section) renderFields(width int, active bool) []string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	fieldViews := make([]string, len(s.Fields))
	for i, field := range s.Fields {
		isActiveField := active && i == s.activeFieldIdx
		isEditingField := isActiveField && s.editor != nil

		borderColor := t.BorderNormal()
		labelStyle := base.
			Width(s.labelWidth()).
			Foreground(t.TextMuted())
		if isActiveField {
			borderColor = t.BorderFocused()
			labelStyle = labelStyle.Foreground(t.Primary()).Bold(true)
		}
		if field.Disabled {
			labelStyle = labelStyle.Foreground(t.TextMuted()).Bold(false)
		}

		value := field.DisplayValue(false)
		if isEditingField {
			value = s.editor.View()
		}

		valueStyle := base.
			Width(s.valueWidth()).
			Foreground(t.Text()).
			Align(lipgloss.Right)
		if !isEditingField {
			valueStyle = valueStyle.Foreground(t.TextEmphasized())
		}
		if field.Disabled {
			valueStyle = valueStyle.Foreground(t.TextMuted())
		}

		row := lipgloss.JoinHorizontal(
			lipgloss.Top,
			labelStyle.Render(field.Label),
			base.Width(2).Render(""),
			valueStyle.Render(value),
		)

		fieldContent := row
		if field.Hint != "" {
			hintStyle := base.
				Foreground(t.TextMuted()).
				Italic(true).
				PaddingLeft(1)
			fieldContent = lipgloss.JoinVertical(lipgloss.Left, row, hintStyle.Render(field.Hint))
		}

		fieldViews[i] = base.
			Width(max(1, width)).
			Padding(0, 1).
			Border(lipgloss.NormalBorder()).
			BorderForeground(borderColor).
			Render(fieldContent)
	}

	return fieldViews
}

// FieldHeights returns the rendered line height of every field at the given
// width, matching exactly what View draws. Callers use the cumulative sums to
// map content lines to fields and back.
func (s *Section) FieldHeights(width int) []int {
	fieldViews := s.renderFields(width, true)
	heights := make([]int, len(fieldViews))
	for i, fv := range fieldViews {
		heights[i] = lipgloss.Height(fv)
	}
	return heights
}

// FieldLineOffset returns the first content line (0-indexed) of field idx.
func (s *Section) FieldLineOffset(width, idx int) int {
	heights := s.FieldHeights(width)
	offset := 0
	for i := 0; i < idx && i < len(heights); i++ {
		offset += heights[i]
	}
	return offset
}

// FieldAtLine maps a content line to the field index that occupies it, or -1.
func (s *Section) FieldAtLine(width, line int) int {
	if line < 0 {
		return -1
	}
	offset := 0
	for i, h := range s.FieldHeights(width) {
		if line < offset+h {
			return i
		}
		offset += h
	}
	return -1
}

// ActiveFieldIdx returns the index of the currently active field.
func (s *Section) ActiveFieldIdx() int {
	return s.activeFieldIdx
}

// SetActiveFieldIdx sets the active field, clamped to the valid range.
func (s *Section) SetActiveFieldIdx(idx int) {
	if len(s.Fields) == 0 {
		return
	}
	s.activeFieldIdx = min(max(idx, 0), len(s.Fields)-1)
}

func (s *Section) startEditing() tea.Cmd {
	field := s.ActiveField()
	if field == nil || field.ReadOnly || field.Disabled {
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
	if field == nil || field.ReadOnly || field.Disabled {
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
