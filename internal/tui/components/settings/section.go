package settings

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/theme"
)

type SaveFieldMsg struct {
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
	if len(s.Fields) == 0 {
		return lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Padding(1, 0).
			Render("No settings in this section.")
	}

	fieldViews := make([]string, len(s.Fields))
	for i, field := range s.Fields {
		isActiveField := active && i == s.activeFieldIdx
		isEditingField := isActiveField && s.editor != nil

		borderColor := t.BorderNormal()
		labelStyle := lipgloss.NewStyle().
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

		valueStyle := lipgloss.NewStyle().
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
			lipgloss.NewStyle().Width(2).Render(""),
			valueStyle.Render(value),
		)

		fieldContent := row
		if field.Hint != "" {
			hintStyle := lipgloss.NewStyle().
				Foreground(t.TextMuted()).
				Italic(true).
				PaddingLeft(1)
			fieldContent = lipgloss.JoinVertical(lipgloss.Left, row, hintStyle.Render(field.Hint))
		}

		fieldViews[i] = lipgloss.NewStyle().
			Width(max(1, width)).
			Padding(0, 1).
			Border(lipgloss.NormalBorder()).
			BorderForeground(borderColor).
			Render(fieldContent)
	}

	return lipgloss.JoinVertical(lipgloss.Left, fieldViews...)
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
