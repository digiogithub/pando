package dialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	"github.com/digiogithub/pando/internal/userinput"
)

// QuestionResponseMsg represents the user's response to an AskUserQuestion request.
type QuestionResponseMsg struct {
	RequestID string
	Answers   []userinput.Answer
	Cancelled bool
}

// askQuestionMode is the current screen of the dialog.
type askQuestionMode int

const (
	askModeAsking askQuestionMode = iota
	askModeSummary
)

// AskQuestionDialogCmp is the interface for the question dialog component.
type AskQuestionDialogCmp interface {
	tea.Model
	layout.Bindings
	SetRequest(request userinput.QuestionRequest) tea.Cmd
}

type askQuestionMapping struct {
	Up      key.Binding
	Down    key.Binding
	Toggle  key.Binding
	Next    key.Binding
	Prev    key.Binding
	Confirm key.Binding
	Cancel  key.Binding
}

var askQuestionKeys = askQuestionMapping{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Toggle: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "select"),
	),
	Next: key.NewBinding(
		key.WithKeys("enter", "right", "tab"),
		key.WithHelp("enter/→", "next"),
	),
	Prev: key.NewBinding(
		key.WithKeys("left", "shift+tab"),
		key.WithHelp("←", "previous"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
}

type askQuestionDialogCmp struct {
	width      int
	height     int
	windowSize tea.WindowSizeMsg

	request    userinput.QuestionRequest
	currentIdx int
	cursor     int // highlighted row in the current question (last row = "Other")
	mode       askQuestionMode
	selections map[int]map[int]bool // questionIdx -> optionIdx -> selected
	otherText  map[int]string       // questionIdx -> free text for the "Other" option

	otherEditing bool
	otherInput   textinput.Model
}

// otherIndex returns the row index of the synthetic "Other" option for the
// current question (it always sits after the real options).
func (a *askQuestionDialogCmp) otherIndex() int {
	if a.currentIdx >= len(a.request.Questions) {
		return 0
	}
	return len(a.request.Questions[a.currentIdx].Options)
}

func (a *askQuestionDialogCmp) Init() tea.Cmd {
	return textinput.Blink
}

func (a *askQuestionDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.windowSize = msg
		return a, a.SetSize()
	case tea.KeyMsg:
		// While editing free text, route keys to the input first.
		if a.otherEditing {
			switch {
			case key.Matches(msg, askQuestionKeys.Confirm):
				a.commitOther()
				return a, nil
			case key.Matches(msg, askQuestionKeys.Cancel):
				a.otherEditing = false
				a.otherInput.Blur()
				return a, nil
			default:
				var cmd tea.Cmd
				a.otherInput, cmd = a.otherInput.Update(msg)
				return a, cmd
			}
		}

		switch a.mode {
		case askModeSummary:
			return a.updateSummary(msg)
		default:
			return a.updateAsking(msg)
		}
	}
	return a, nil
}

func (a *askQuestionDialogCmp) updateAsking(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, askQuestionKeys.Cancel):
		return a, util.CmdHandler(QuestionResponseMsg{RequestID: a.request.ID, Cancelled: true})
	case key.Matches(msg, askQuestionKeys.Up):
		if a.cursor > 0 {
			a.cursor--
		}
		return a, nil
	case key.Matches(msg, askQuestionKeys.Down):
		if a.cursor < a.otherIndex() {
			a.cursor++
		}
		return a, nil
	case key.Matches(msg, askQuestionKeys.Toggle):
		return a, a.toggleCurrent()
	case key.Matches(msg, askQuestionKeys.Next):
		// Advance to the next question, or the summary on the last one.
		if a.currentIdx < len(a.request.Questions)-1 {
			a.currentIdx++
			a.cursor = 0
			return a, nil
		}
		a.mode = askModeSummary
		return a, nil
	case key.Matches(msg, askQuestionKeys.Prev):
		if a.currentIdx > 0 {
			a.currentIdx--
			a.cursor = 0
		}
		return a, nil
	}
	return a, nil
}

func (a *askQuestionDialogCmp) updateSummary(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, askQuestionKeys.Cancel):
		return a, util.CmdHandler(QuestionResponseMsg{RequestID: a.request.ID, Cancelled: true})
	case key.Matches(msg, askQuestionKeys.Prev):
		// Back to editing the last question.
		a.mode = askModeAsking
		a.currentIdx = len(a.request.Questions) - 1
		a.cursor = 0
		return a, nil
	case key.Matches(msg, askQuestionKeys.Confirm):
		return a, util.CmdHandler(QuestionResponseMsg{
			RequestID: a.request.ID,
			Answers:   a.buildAnswers(),
		})
	}
	return a, nil
}

// toggleCurrent selects/deselects the option under the cursor, honouring
// single vs multi select, and starts text editing when "Other" is chosen.
func (a *askQuestionDialogCmp) toggleCurrent() tea.Cmd {
	if a.currentIdx >= len(a.request.Questions) {
		return nil
	}
	q := a.request.Questions[a.currentIdx]

	// "Other" row: begin free-text editing.
	if a.cursor == a.otherIndex() {
		a.otherEditing = true
		a.otherInput.SetValue(a.otherText[a.currentIdx])
		a.otherInput.CursorEnd()
		return a.otherInput.Focus()
	}

	if a.selections[a.currentIdx] == nil {
		a.selections[a.currentIdx] = make(map[int]bool)
	}
	if q.MultiSelect {
		a.selections[a.currentIdx][a.cursor] = !a.selections[a.currentIdx][a.cursor]
	} else {
		// Single select: clear the others first.
		on := a.selections[a.currentIdx][a.cursor]
		a.selections[a.currentIdx] = make(map[int]bool)
		a.selections[a.currentIdx][a.cursor] = !on
	}
	return nil
}

// commitOther stores the free text and exits editing mode.
func (a *askQuestionDialogCmp) commitOther() {
	a.otherText[a.currentIdx] = strings.TrimSpace(a.otherInput.Value())
	a.otherEditing = false
	a.otherInput.Blur()
}

// buildAnswers converts the current selection state into userinput.Answer values.
func (a *askQuestionDialogCmp) buildAnswers() []userinput.Answer {
	answers := make([]userinput.Answer, 0, len(a.request.Questions))
	for i, q := range a.request.Questions {
		var selected []string
		for optIdx, on := range a.selections[i] {
			if on && optIdx < len(q.Options) {
				selected = append(selected, q.Options[optIdx].Label)
			}
		}
		answers = append(answers, userinput.Answer{
			QuestionID: q.ID,
			Selected:   selected,
			OtherText:  a.otherText[i],
		})
	}
	return answers
}

func (a *askQuestionDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	var body string
	if a.mode == askModeSummary {
		body = a.renderSummary()
	} else {
		body = a.renderQuestion()
	}

	content := lipgloss.JoinVertical(
		lipgloss.Top,
		a.renderTitle(),
		baseStyle.Render(strings.Repeat(" ", a.width-4)),
		body,
		baseStyle.Render(strings.Repeat(" ", a.width-4)),
		a.renderFooter(),
	)

	return baseStyle.
		Padding(1, 0, 0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(a.width).
		Height(a.height).
		Render(content)
}

func (a *askQuestionDialogCmp) renderTitle() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	title := "Question"
	if total := len(a.request.Questions); total > 1 && a.mode == askModeAsking {
		title = fmt.Sprintf("Question %d/%d", a.currentIdx+1, total)
	} else if a.mode == askModeSummary {
		title = "Review your answers"
	}
	return baseStyle.
		Bold(true).
		Width(a.width - 4).
		Foreground(t.Primary()).
		Render(title)
}

func (a *askQuestionDialogCmp) renderQuestion() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if a.currentIdx >= len(a.request.Questions) {
		return ""
	}
	q := a.request.Questions[a.currentIdx]

	var lines []string
	if q.Header != "" {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Bold(true).Width(a.width-4).Render(q.Header))
	}
	lines = append(lines,
		baseStyle.Foreground(t.Text()).Width(a.width-4).Render(q.Question),
		baseStyle.Render(strings.Repeat(" ", a.width-4)),
	)

	for i, opt := range q.Options {
		lines = append(lines, a.renderOption(i, a.optionMark(q, i), opt.Label, opt.Description))
	}

	// Synthetic "Other" row.
	otherDesc := "Provide a custom answer"
	if txt := a.otherText[a.currentIdx]; txt != "" {
		otherDesc = txt
	}
	if a.otherEditing {
		lines = append(lines, a.renderOtherEditor())
	} else {
		lines = append(lines, a.renderOption(a.otherIndex(), a.otherMark(), "Other", otherDesc))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// optionMark returns the checkbox/radio marker for a real option.
func (a *askQuestionDialogCmp) optionMark(q userinput.Question, idx int) string {
	on := a.selections[a.currentIdx][idx]
	if q.MultiSelect {
		if on {
			return "[x]"
		}
		return "[ ]"
	}
	if on {
		return "(•)"
	}
	return "( )"
}

// otherMark returns the marker for the "Other" row.
func (a *askQuestionDialogCmp) otherMark() string {
	if a.otherText[a.currentIdx] != "" {
		return "[x]"
	}
	return "[ ]"
}

func (a *askQuestionDialogCmp) renderOption(idx int, mark, label, description string) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	cursor := "  "
	style := baseStyle.Foreground(t.Text())
	if idx == a.cursor {
		cursor = "▌ "
		style = baseStyle.Foreground(t.Primary()).Bold(true)
	}

	text := fmt.Sprintf("%s%s %s", cursor, mark, label)
	if description != "" {
		text = fmt.Sprintf("%s — %s", text, description)
	}
	return style.Width(a.width - 4).Render(text)
}

func (a *askQuestionDialogCmp) renderOtherEditor() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	label := baseStyle.Foreground(t.Primary()).Bold(true).Render("▌ Other: ")
	return baseStyle.Width(a.width - 4).Render(label + a.otherInput.View())
}

func (a *askQuestionDialogCmp) renderSummary() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	var lines []string
	for i, q := range a.request.Questions {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Bold(true).Width(a.width-4).Render(q.Question))

		var selected []string
		for optIdx, on := range a.selections[i] {
			if on && optIdx < len(q.Options) {
				selected = append(selected, q.Options[optIdx].Label)
			}
		}
		if txt := a.otherText[i]; txt != "" {
			selected = append(selected, fmt.Sprintf("Other: %s", txt))
		}
		answer := "(no selection)"
		if len(selected) > 0 {
			answer = strings.Join(selected, ", ")
		}
		lines = append(lines,
			baseStyle.Foreground(t.Text()).Width(a.width-4).Render("  → "+answer),
			baseStyle.Render(strings.Repeat(" ", a.width-4)),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (a *askQuestionDialogCmp) renderFooter() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	var help string
	switch {
	case a.otherEditing:
		help = "enter confirm · esc cancel"
	case a.mode == askModeSummary:
		help = "enter confirm · ← edit · esc cancel"
	default:
		help = "↑/↓ move · space select · enter next · ← back · esc cancel"
	}
	return baseStyle.Foreground(t.TextMuted()).Width(a.width - 4).Render(help)
}

func (a *askQuestionDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(askQuestionKeys)
}

func (a *askQuestionDialogCmp) SetSize() tea.Cmd {
	if a.windowSize.Width == 0 {
		return nil
	}
	a.width = int(float64(a.windowSize.Width) * 0.7)
	a.height = int(float64(a.windowSize.Height) * 0.6)
	a.otherInput.Width = a.width - 16
	return nil
}

func (a *askQuestionDialogCmp) SetRequest(request userinput.QuestionRequest) tea.Cmd {
	a.request = request
	a.currentIdx = 0
	a.cursor = 0
	a.mode = askModeAsking
	a.otherEditing = false
	a.selections = make(map[int]map[int]bool)
	a.otherText = make(map[int]string)
	return a.SetSize()
}

// NewAskQuestionDialogCmp creates a new question dialog component.
func NewAskQuestionDialogCmp() AskQuestionDialogCmp {
	t := theme.CurrentTheme()
	ti := textinput.New()
	ti.Placeholder = "Type your answer..."
	ti.Prompt = ""
	ti.PlaceholderStyle = ti.PlaceholderStyle.Background(t.Background())
	ti.PromptStyle = ti.PromptStyle.Background(t.Background())
	ti.TextStyle = ti.TextStyle.Background(t.Background())

	return &askQuestionDialogCmp{
		otherInput: ti,
		mode:       askModeAsking,
		selections: make(map[int]map[int]bool),
		otherText:  make(map[int]string),
	}
}
