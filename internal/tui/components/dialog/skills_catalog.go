package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/skills/catalog"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// --- Tea messages ---

// OpenSkillsCatalogMsg opens the skills catalog dialog.
type OpenSkillsCatalogMsg struct{}

// CloseSkillsCatalogMsg closes the skills catalog dialog.
type CloseSkillsCatalogMsg struct{}

// InstallSkillMsg requests installation of a skill.
type InstallSkillMsg struct {
	Skill  catalog.CatalogSkill
	Global bool // true = ~/.pando/skills/, false = .pando/skills/
}

// SkillInstalledMsg reports the result of a skill installation.
type SkillInstalledMsg struct {
	SkillName  string
	InstallDir string
	Err        error
}

// internal async messages
type skillSearchResultMsg struct {
	results []catalog.CatalogSkill
	err     error
}

type skillSearchDebounceMsg struct {
	query string
}

// --- Key map ---

type skillsCatalogKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	K      key.Binding
	J      key.Binding
	Enter  key.Binding
	Global key.Binding
	Escape key.Binding
}

var skillsCatalogKeys = skillsCatalogKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "previous"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next"),
	),
	K: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "previous"),
	),
	J: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "next"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "install"),
	),
	Global: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "install global"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
}

const (
	maxVisibleResults  = 8
	minQueryLen        = 2
	searchDebounceMs   = 200
	DialogDialogWidth  = 52
	DialogDialogHeight = 18
)

// SkillsCatalogDialog is a Bubbletea component for browsing and installing skills.
type SkillsCatalogDialog struct {
	width     int
	height    int
	input     textinput.Model
	results   []catalog.CatalogSkill
	selected  int
	loading   bool
	statusMsg string
	err       error
	client    *catalog.Client
}

// NewSkillsCatalogDialog creates a new SkillsCatalogDialog.
func NewSkillsCatalogDialog(width, height int, client *catalog.Client) SkillsCatalogDialog {
	if width == 0 {
		width = DialogDialogWidth
	}
	if height == 0 {
		height = DialogDialogHeight
	}

	ti := textinput.New()
	ti.Placeholder = "type to search..."
	ti.CharLimit = 64

	return SkillsCatalogDialog{
		width:  width,
		height: height,
		input:  ti,
		client: client,
	}
}

// Init focuses the text input.
func (m SkillsCatalogDialog) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles incoming messages.
func (m SkillsCatalogDialog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, skillsCatalogKeys.Escape):
			return m, util.CmdHandler(CloseSkillsCatalogMsg{})

		case key.Matches(msg, skillsCatalogKeys.Up) || key.Matches(msg, skillsCatalogKeys.K):
			if m.selected > 0 {
				m.selected--
			}
			return m, nil

		case key.Matches(msg, skillsCatalogKeys.Down) || key.Matches(msg, skillsCatalogKeys.J):
			if m.selected < len(m.results)-1 {
				m.selected++
			}
			return m, nil

		case key.Matches(msg, skillsCatalogKeys.Enter):
			if len(m.results) > 0 {
				skill := m.results[m.selected]
				return m, util.CmdHandler(InstallSkillMsg{Skill: skill, Global: false})
			}
			return m, nil

		case key.Matches(msg, skillsCatalogKeys.Global):
			if len(m.results) > 0 {
				skill := m.results[m.selected]
				return m, util.CmdHandler(InstallSkillMsg{Skill: skill, Global: true})
			}
			return m, nil

		default:
			// Forward key to text input
			prevValue := m.input.Value()
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)

			newValue := m.input.Value()
			if newValue != prevValue {
				m.selected = 0
				if len(newValue) >= minQueryLen {
					m.loading = true
					m.statusMsg = "Searching..."
					cmds = append(cmds, tea.Tick(searchDebounceMs*time.Millisecond, func(_ time.Time) tea.Msg {
						return skillSearchDebounceMsg{query: newValue}
					}))
				} else {
					m.loading = false
					m.statusMsg = ""
					m.results = nil
				}
			}
		}

	case skillSearchDebounceMsg:
		// Only search if the query still matches the current input
		if msg.query == m.input.Value() && len(msg.query) >= minQueryLen {
			cmds = append(cmds, m.searchCmd(msg.query))
		}

	case skillSearchResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.statusMsg = fmt.Sprintf("Error: %s", msg.err.Error())
			m.results = nil
		} else {
			m.err = nil
			m.statusMsg = ""
			m.results = msg.results
			m.selected = 0
		}

	case SkillInstalledMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Error: %s", msg.Err.Error())
		} else {
			m.statusMsg = fmt.Sprintf("Installed: %s", msg.SkillName)
		}
	}

	return m, tea.Batch(cmds...)
}

// searchCmd performs an async search against the catalog.
func (m SkillsCatalogDialog) searchCmd(query string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := client.Search(ctx, query, 10)
		if err != nil {
			return skillSearchResultMsg{err: err}
		}
		return skillSearchResultMsg{results: result.Skills}
	}
}

// View renders the dialog.
func (m SkillsCatalogDialog) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	innerWidth := m.width - 4 // subtract border (2) + padding (2)

	// Title
	title := base.
		Bold(true).
		Foreground(t.Primary()).
		Width(innerWidth).
		Render("Skills Catalog")

	// Search input row
	m.input.Focus()
	m.input.Width = innerWidth - 9 // "Search: " prefix takes ~9 chars
	searchLabel := base.Foreground(t.TextMuted()).Render("Search: ")
	searchRow := lipgloss.JoinHorizontal(lipgloss.Left, searchLabel, m.input.View())

	// Results list
	visible := maxVisibleResults
	if len(m.results) < visible {
		visible = len(m.results)
	}

	// Determine scroll window
	startIdx := 0
	if m.selected >= visible {
		startIdx = m.selected - visible + 1
	}
	endIdx := startIdx + visible
	if endIdx > len(m.results) {
		endIdx = len(m.results)
	}

	resultLines := make([]string, 0, visible)
	for i := startIdx; i < endIdx; i++ {
		skill := m.results[i]
		cursor := "  "
		itemStyle := base.Width(innerWidth)
		if i == m.selected {
			cursor = "▶ "
			itemStyle = itemStyle.
				Background(t.Primary()).
				Foreground(t.Background()).
				Bold(true)
		}

		installs := catalog.FormatInstalls(skill.Installs)
		// Truncate source to fit
		source := skill.Source
		maxSourceLen := 16
		if len(source) > maxSourceLen {
			source = source[:maxSourceLen-1] + "…"
		}
		// Truncate name
		name := skill.Name
		maxNameLen := 20
		if len(name) > maxNameLen {
			name = name[:maxNameLen-1] + "…"
		}

		line := fmt.Sprintf("%s%-20s  %-17s  %s", cursor, name, source, installs)
		resultLines = append(resultLines, itemStyle.Render(line))
	}

	// Status / loading line
	var statusLine string
	switch {
	case m.loading:
		statusLine = base.Foreground(t.TextMuted()).Width(innerWidth).Render("Searching...")
	case m.statusMsg != "":
		color := t.Text()
		if m.err != nil {
			color = t.Error()
		}
		statusLine = base.Foreground(color).Width(innerWidth).Render(m.statusMsg)
	case len(m.results) == 0 && len(m.input.Value()) >= minQueryLen:
		statusLine = base.Foreground(t.TextMuted()).Width(innerWidth).Render("No results found.")
	case len(m.input.Value()) < minQueryLen && len(m.input.Value()) > 0:
		statusLine = base.Foreground(t.TextMuted()).Width(innerWidth).Render("Type at least 2 characters to search.")
	default:
		statusLine = base.Width(innerWidth).Render("")
	}

	// Footer key hints
	footer := base.
		Foreground(t.TextMuted()).
		Width(innerWidth).
		Render("enter:install  G:global  esc:cancel")

	// Assemble content
	parts := []string{
		title,
		base.Width(innerWidth).Render(""),
		searchRow,
		base.Width(innerWidth).Render(""),
	}
	if len(resultLines) > 0 {
		parts = append(parts, strings.Join(resultLines, "\n"))
		parts = append(parts, base.Width(innerWidth).Render(""))
	}
	parts = append(parts, statusLine)
	parts = append(parts, base.Width(innerWidth).Render(""))
	parts = append(parts, footer)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return base.
		Padding(1, 1).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(m.width).
		Render(content)
}
