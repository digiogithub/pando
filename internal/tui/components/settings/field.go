package settings

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/theme"
)

type FieldType int

const (
	FieldText FieldType = iota
	FieldToggle
	FieldSelect
	FieldAction // triggers a command when enter/space is pressed
)

type Field struct {
	Label    string
	Key      string
	Value    string
	Type     FieldType
	Options  []string
	Masked   bool
	ReadOnly bool
	Disabled bool
	// Hint is optional helper text shown below the field row (e.g. recommended default).
	Hint string
}

func (f Field) DisplayValue(editing bool) string {
	if f.Masked && !editing && strings.TrimSpace(f.Value) != "" {
		return "****"
	}

	switch f.Type {
	case FieldToggle:
		return strconv.FormatBool(f.BoolValue())
	case FieldAction:
		return f.Value + "  [→ Enter]"
	default:
		return f.Value
	}
}

func (f Field) BoolValue() bool {
	value, err := strconv.ParseBool(strings.TrimSpace(f.Value))
	if err != nil {
		return false
	}

	return value
}

func (f *Field) SetBoolValue(value bool) {
	f.Value = strconv.FormatBool(value)
}

type fieldEditor interface {
	SetWidth(width int)
	Update(msg tea.Msg) (tea.Cmd, bool)
	View() string
	Value() string
}

type TextFieldCmp struct {
	input textinput.Model
}

func NewTextFieldCmp(field Field, width int) TextFieldCmp {
	t := theme.CurrentTheme()
	input := textinput.New()
	input.Prompt = ""
	input.SetValue(field.Value)
	input.Focus()
	input.Width = max(1, width)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(t.Primary())
	input.TextStyle = lipgloss.NewStyle().Foreground(t.Text())
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(t.TextMuted())
	input.PromptStyle = lipgloss.NewStyle().Foreground(t.Primary())

	return TextFieldCmp{input: input}
}

func (c *TextFieldCmp) SetWidth(width int) {
	c.input.Width = max(1, width)
}

func (c *TextFieldCmp) Update(msg tea.Msg) (tea.Cmd, bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
		return nil, true
	}

	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return cmd, false
}

func (c TextFieldCmp) View() string {
	return c.input.View()
}

func (c TextFieldCmp) Value() string {
	return c.input.Value()
}

type ToggleFieldCmp struct {
	value bool
}

func NewToggleFieldCmp(field Field) ToggleFieldCmp {
	return ToggleFieldCmp{value: field.BoolValue()}
}

func (c *ToggleFieldCmp) SetWidth(width int) {}

func (c *ToggleFieldCmp) Update(msg tea.Msg) (tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	switch keyMsg.String() {
	case "enter", " ":
		c.value = !c.value
		return nil, true
	default:
		return nil, false
	}
}

func (c ToggleFieldCmp) View() string {
	t := theme.CurrentTheme()

	selectedStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true)
	regularStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted())

	trueValue := regularStyle.Render("true")
	falseValue := regularStyle.Render("false")
	if c.value {
		trueValue = selectedStyle.Render("true")
	} else {
		falseValue = selectedStyle.Render("false")
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, trueValue, " / ", falseValue)
}

func (c ToggleFieldCmp) Value() string {
	return strconv.FormatBool(c.value)
}

type SelectFieldCmp struct {
	options     []string
	activeIdx   int
	renderWidth int
}

func NewSelectFieldCmp(field Field, width int) SelectFieldCmp {
	activeIdx := 0
	for i, option := range field.Options {
		if option == field.Value {
			activeIdx = i
			break
		}
	}

	return SelectFieldCmp{
		options:     append([]string(nil), field.Options...),
		activeIdx:   activeIdx,
		renderWidth: max(1, width),
	}
}

func (c *SelectFieldCmp) SetWidth(width int) {
	c.renderWidth = max(1, width)
}

func (c *SelectFieldCmp) Update(msg tea.Msg) (tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || len(c.options) == 0 {
		return nil, false
	}

	switch keyMsg.String() {
	case "up", "k":
		c.activeIdx = (c.activeIdx - 1 + len(c.options)) % len(c.options)
	case "down", "j":
		c.activeIdx = (c.activeIdx + 1) % len(c.options)
	case "enter":
		return nil, true
	}

	return nil, false
}

func (c SelectFieldCmp) View() string {
	t := theme.CurrentTheme()
	if len(c.options) == 0 {
		return lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Render("(no options)")
	}

	items := make([]string, len(c.options))
	for i, option := range c.options {
		prefix := "  "
		itemStyle := lipgloss.NewStyle().
			Width(c.renderWidth).
			Foreground(t.Text())

		if i == c.activeIdx {
			prefix = "> "
			itemStyle = itemStyle.
				Foreground(t.Primary()).
				Bold(true)
		}

		items[i] = itemStyle.Render(prefix + option)
	}

	return lipgloss.JoinVertical(lipgloss.Left, items...)
}

func (c SelectFieldCmp) Value() string {
	if len(c.options) == 0 {
		return ""
	}

	return c.options[c.activeIdx]
}
