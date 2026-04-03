package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	"github.com/sahilm/fuzzy"
)

const (
	personaDialogMinWidth        = 48
	personaDialogMaxVisibleItems = 8
	personaNoneOption            = "None (auto)"
)

// OpenPersonaDialogMsg triggers opening the persona picker.
type OpenPersonaDialogMsg struct{}

// PersonaSelectedMsg is fired when a persona is selected.
type PersonaSelectedMsg struct {
	Name string
}

// PersonaClearedMsg is fired when "None (auto)" is selected.
type PersonaClearedMsg struct{}

// PersonaDialog interface for the persona selection dialog.
type PersonaDialog interface {
	tea.Model
	layout.Bindings
	SetPersonas(personas []string, active string)
}

type personaMatch struct {
	name           string
	matchedIndexes []int
	score          int
}

type personaDialogCmp struct {
	personas    []string
	filtered    []personaMatch
	active      string
	selectedIdx int
	width       int
	height      int
	queryInput  textinput.Model
}

type personaKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
	Up     key.Binding
	Down   key.Binding
	J      key.Binding
	K      key.Binding
}

var personaKeys = personaKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select persona"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "previous persona"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next persona"),
	),
	J: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "next persona"),
	),
	K: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "previous persona"),
	),
}

func (p *personaDialogCmp) Init() tea.Cmd {
	p.queryInput.Focus()
	return textinput.Blink
}

func (p *personaDialogCmp) filterPersonas() {
	query := strings.TrimSpace(p.queryInput.Value())

	allItems := append([]string{personaNoneOption}, p.personas...)

	if query == "" {
		p.filtered = make([]personaMatch, 0, len(allItems))
		for _, name := range allItems {
			p.filtered = append(p.filtered, personaMatch{name: name})
		}
		p.selectedIdx = 0
		return
	}

	matches := fuzzy.Find(strings.ToLower(query), lowerStrings(allItems))
	filtered := make([]personaMatch, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, personaMatch{
			name:           allItems[match.Index],
			matchedIndexes: match.MatchedIndexes,
			score:          match.Score,
		})
	}

	p.filtered = filtered
	if len(p.filtered) == 0 {
		p.selectedIdx = 0
		return
	}
	if p.selectedIdx >= len(p.filtered) {
		p.selectedIdx = len(p.filtered) - 1
	}
}

func (p *personaDialogCmp) moveSelection(offset int) {
	if len(p.filtered) == 0 {
		p.selectedIdx = 0
		return
	}

	p.selectedIdx += offset
	if p.selectedIdx < 0 {
		p.selectedIdx = 0
	}
	if p.selectedIdx >= len(p.filtered) {
		p.selectedIdx = len(p.filtered) - 1
	}
}

func (p *personaDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, personaKeys.Enter):
			if len(p.filtered) == 0 {
				return p, nil
			}
			selected := p.filtered[p.selectedIdx].name
			if selected == personaNoneOption {
				return p, util.CmdHandler(PersonaClearedMsg{})
			}
			return p, util.CmdHandler(PersonaSelectedMsg{Name: selected})
		case key.Matches(msg, personaKeys.Escape):
			return p, util.CmdHandler(CompletionDialogCloseMsg{})
		case key.Matches(msg, personaKeys.Up) || key.Matches(msg, personaKeys.K):
			p.moveSelection(-1)
			return p, nil
		case key.Matches(msg, personaKeys.Down) || key.Matches(msg, personaKeys.J):
			p.moveSelection(1)
			return p, nil
		}

		var cmd tea.Cmd
		p.queryInput, cmd = p.queryInput.Update(msg)
		p.filterPersonas()
		return p, cmd
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	}

	return p, nil
}

func (p *personaDialogCmp) visibleMatches() []personaMatch {
	if len(p.filtered) <= personaDialogMaxVisibleItems {
		return p.filtered
	}

	startIdx := 0
	halfVisible := personaDialogMaxVisibleItems / 2
	if p.selectedIdx >= halfVisible && p.selectedIdx < len(p.filtered)-halfVisible {
		startIdx = p.selectedIdx - halfVisible
	} else if p.selectedIdx >= len(p.filtered)-halfVisible {
		startIdx = len(p.filtered) - personaDialogMaxVisibleItems
	}

	endIdx := min(startIdx+personaDialogMaxVisibleItems, len(p.filtered))
	return p.filtered[startIdx:endIdx]
}

func (p *personaDialogCmp) maxContentWidth() int {
	maxWidth := personaDialogMinWidth
	for _, match := range p.filtered {
		lineWidth := lipgloss.Width(match.name) + 4
		maxWidth = max(maxWidth, lineWidth)
	}

	if p.width > 0 {
		return max(personaDialogMinWidth, min(maxWidth, p.width-10))
	}
	return maxWidth
}

func (p *personaDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	dialogWidth := p.maxContentWidth()

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render("Select Persona")

	queryStyle := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.TextMuted())
	p.queryInput.Width = max(0, dialogWidth-4)
	query := queryStyle.Render(p.queryInput.View())

	var lines []string
	if len(p.filtered) == 0 {
		lines = append(lines, baseStyle.Width(dialogWidth).Padding(0, 1).Render("No personas found"))
	} else {
		visible := p.visibleMatches()
		for _, match := range visible {
			selected := p.selectedIdx < len(p.filtered) && p.filtered[p.selectedIdx].name == match.name
			lines = append(lines, p.renderPersonaItem(match, selected, dialogWidth))
		}
	}

	footerText := "No active persona"
	if p.active != "" {
		footerText = "Active: " + p.active
	}
	footer := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render(footerText)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(dialogWidth).Render(""),
		query,
		baseStyle.Width(dialogWidth).Render(""),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		baseStyle.Width(dialogWidth).Render(""),
		footer,
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (p *personaDialogCmp) renderPersonaItem(match personaMatch, selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	itemStyle := baseStyle.Width(width).Padding(0, 1)
	nameStyle := baseStyle.Foreground(t.Text())
	matchStyle := nameStyle.Foreground(t.Primary()).Bold(true)

	if selected {
		itemStyle = itemStyle.
			Background(t.SelectionBackground()).
			Foreground(t.SelectionForeground()).
			Bold(true)
		nameStyle = nameStyle.Foreground(t.SelectionForeground()).Bold(true)
		matchStyle = matchStyle.Foreground(t.SelectionForeground()).Bold(true).Underline(true)
	}

	var name string
	if len(match.matchedIndexes) > 0 {
		name = renderMatchedText(match.name, match.matchedIndexes, nameStyle, matchStyle)
	} else {
		name = nameStyle.Render(match.name)
	}

	return itemStyle.Render(name)
}

func (p *personaDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(personaKeys)
}

func (p *personaDialogCmp) SetPersonas(personas []string, active string) {
	p.personas = personas
	p.active = active
	p.queryInput.SetValue("")
	p.queryInput.CursorEnd()
	p.queryInput.Focus()
	p.selectedIdx = 0
	p.filterPersonas()
}

// NewPersonaDialogCmp creates a new persona selection dialog.
func NewPersonaDialogCmp() PersonaDialog {
	input := textinput.New()
	input.Placeholder = "Search personas"
	input.Prompt = "> "
	input.CharLimit = 128

	d := &personaDialogCmp{
		queryInput: input,
	}
	d.filterPersonas()
	return d
}
