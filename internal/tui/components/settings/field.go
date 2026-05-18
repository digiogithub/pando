package settings

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/theme"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

const selectFieldVisibleOptions = 4

type FieldType int

const (
	FieldText FieldType = iota
	FieldToggle
	FieldSelect
	FieldAction // triggers a command when enter/space is pressed
)

type Field struct {
	Label            string
	Key              string
	Value            string
	Type             FieldType
	Options          []string
	Masked           bool
	ReadOnly         bool
	Disabled         bool
	UseModelDialog   bool
	ModelDialogTitle string
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
	options      []string
	activeIdx    int
	renderWidth  int
	scrollOffset int
	zoneBaseID   string
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
		zoneBaseID:  field.Key,
	}
}

func (c *SelectFieldCmp) SetWidth(width int) {
	c.renderWidth = max(1, width)
}

func (c *SelectFieldCmp) Update(msg tea.Msg) (tea.Cmd, bool) {
	if len(c.options) == 0 {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			c.moveUp()
		case "down", "j":
			c.moveDown()
		case "enter":
			return nil, true
		}
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			return nil, false
		}

		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if tuizone.InBounds(c.listZoneID(), msg) {
				c.moveUp()
			}
		case tea.MouseButtonWheelDown:
			if tuizone.InBounds(c.listZoneID(), msg) {
				c.moveDown()
			}
		case tea.MouseButtonLeft:
			if !tuizone.InBounds(c.listZoneID(), msg) {
				return nil, false
			}
			for i := c.scrollOffset; i < c.visibleEnd(); i++ {
				if tuizone.InBounds(c.optionZoneID(i), msg) {
					c.activeIdx = i
					c.ensureVisible()
					return nil, true
				}
			}
		}
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

	start := c.scrollOffset
	end := c.visibleEnd()
	items := make([]string, 0, end-start+1)
	for i := start; i < end; i++ {
		option := c.options[i]
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

		items = append(items, tuizone.MarkModelItem(c.optionZoneID(i), itemStyle.Render(prefix+option)))
	}

	if len(c.options) > selectFieldVisibleOptions {
		indicator := ""
		if c.scrollOffset > 0 {
			indicator += "↑ "
		}
		if c.visibleEnd() < len(c.options) {
			indicator += "↓"
		}
		items = append(items, lipgloss.NewStyle().Width(c.renderWidth).Align(lipgloss.Right).Foreground(t.Primary()).Render(strings.TrimSpace(indicator)))
	}

	return tuizone.MarkModelList(c.listZoneID(), lipgloss.JoinVertical(lipgloss.Left, items...))
}

func (c SelectFieldCmp) Value() string {
	if len(c.options) == 0 {
		return ""
	}

	return c.options[c.activeIdx]
}

func (c *SelectFieldCmp) moveUp() {
	c.activeIdx = (c.activeIdx - 1 + len(c.options)) % len(c.options)
	c.ensureVisible()
}

func (c *SelectFieldCmp) moveDown() {
	c.activeIdx = (c.activeIdx + 1) % len(c.options)
	c.ensureVisible()
}

func (c *SelectFieldCmp) ensureVisible() {
	if c.activeIdx < c.scrollOffset {
		c.scrollOffset = c.activeIdx
	}
	if c.activeIdx >= c.scrollOffset+selectFieldVisibleOptions {
		c.scrollOffset = c.activeIdx - (selectFieldVisibleOptions - 1)
	}
	maxOffset := max(0, len(c.options)-selectFieldVisibleOptions)
	if c.scrollOffset > maxOffset {
		c.scrollOffset = maxOffset
	}
	if c.scrollOffset < 0 {
		c.scrollOffset = 0
	}
}

func (c SelectFieldCmp) visibleEnd() int {
	return min(c.scrollOffset+selectFieldVisibleOptions, len(c.options))
}

func (c SelectFieldCmp) listZoneID() string {
	return "settings-select-list-" + c.zoneBaseID
}

func (c SelectFieldCmp) optionZoneID(index int) string {
	return c.listZoneID() + "-option-" + strconv.Itoa(index)
}
