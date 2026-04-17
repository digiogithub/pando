package dialog

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// InitDialogCmp is a component that asks the user if they want to initialize
// the project (create AGENTS.md) and, when no local config file exists, whether
// to generate a .pando.toml in the current directory.
type InitDialogCmp struct {
	width, height int

	// AGENTS.md question
	initSelected int // 0=yes 1=no

	// Config generation question — only shown when showConfigSection is true.
	showConfigSection bool
	configSelected    int // 0=yes 1=no
	focusConfig       bool // true when the keyboard focus is on the config row

	keys initDialogKeyMap
}

// NewInitDialogCmp creates a new InitDialogCmp.
// It automatically detects whether the config-generation section should be shown.
func NewInitDialogCmp() InitDialogCmp {
	return InitDialogCmp{
		initSelected:      0,
		configSelected:    0,
		showConfigSection: config.ShouldGenerateLocalConfig(),
		focusConfig:       false,
		keys:              initDialogKeyMap{},
	}
}

type initDialogKeyMap struct {
	Tab    key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
	Y      key.Binding
	N      key.Binding
}

// ShortHelp implements key.Map.
func (k initDialogKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch section / toggle"),
		),
		key.NewBinding(
			key.WithKeys("left", "right"),
			key.WithHelp("←/→", "toggle yes/no"),
		),
		key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		key.NewBinding(
			key.WithKeys("esc", "q"),
			key.WithHelp("esc/q", "cancel"),
		),
	}
}

// FullHelp implements key.Map.
func (k initDialogKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// Init implements tea.Model.
func (m InitDialogCmp) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m InitDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc", "q"))):
			return m, util.CmdHandler(CloseInitDialogMsg{Initialize: false, GenerateConfig: false})

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			if m.showConfigSection {
				m.focusConfig = !m.focusConfig
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("left", "right", "h", "l"))):
			if m.focusConfig && m.showConfigSection {
				m.configSelected = (m.configSelected + 1) % 2
			} else {
				m.initSelected = (m.initSelected + 1) % 2
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return m, util.CmdHandler(CloseInitDialogMsg{
				Initialize:     m.initSelected == 0,
				GenerateConfig: m.showConfigSection && m.configSelected == 0,
			})

		case key.Matches(msg, key.NewBinding(key.WithKeys("y"))):
			if m.focusConfig && m.showConfigSection {
				m.configSelected = 0
			} else {
				m.initSelected = 0
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
			if m.focusConfig && m.showConfigSection {
				m.configSelected = 1
			} else {
				m.initSelected = 1
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View implements tea.Model.
func (m InitDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	maxWidth := 62

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Padding(0, 1).
		Render("Initialize Project")

	explanation := baseStyle.
		Foreground(t.Text()).
		Width(maxWidth).
		Padding(0, 1).
		Render("Initialization creates or updates AGENTS.md, the main project memory file that Pando prefers when it is available. If you already have PANDO.md or CLAUDE.md, useful guidance can be migrated into AGENTS.md.")

	// AGENTS.md question row
	initQuestion := baseStyle.
		Foreground(t.Text()).
		Width(maxWidth).
		Padding(1, 1, 0, 1).
		Render("Initialize AGENTS.md for this project?")

	initButtons := m.renderYesNo(t, baseStyle, maxWidth, m.initSelected, !m.focusConfig)

	sections := []string{
		title,
		baseStyle.Width(maxWidth).Render(""),
		explanation,
		initQuestion,
		initButtons,
	}

	// Config generation section
	if m.showConfigSection {
		cwd := config.WorkingDirectory()
		configQuestion := baseStyle.
			Foreground(t.Text()).
			Width(maxWidth).
			Padding(1, 1, 0, 1).
			Render(fmt.Sprintf("Generate .pando.toml in current directory?\n%s", cwd))

		configButtons := m.renderYesNo(t, baseStyle, maxWidth, m.configSelected, m.focusConfig)
		sections = append(sections, configQuestion, configButtons)
	}

	sections = append(sections, baseStyle.Width(maxWidth).Render(""))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

// renderYesNo renders a Yes/No button pair. active highlights the focused pair.
func (m InitDialogCmp) renderYesNo(
	t theme.Theme,
	baseStyle lipgloss.Style,
	maxWidth int,
	selectedIdx int,
	active bool,
) string {
	yesStyle := baseStyle
	noStyle := baseStyle

	borderColor := t.TextMuted()
	if active {
		borderColor = t.Primary()
	}

	if selectedIdx == 0 {
		yesStyle = yesStyle.Background(borderColor).Foreground(t.Background()).Bold(true)
		noStyle = noStyle.Background(t.Background()).Foreground(borderColor)
	} else {
		noStyle = noStyle.Background(borderColor).Foreground(t.Background()).Bold(true)
		yesStyle = yesStyle.Background(t.Background()).Foreground(borderColor)
	}

	yes := yesStyle.Padding(0, 3).Render("Yes")
	no := noStyle.Padding(0, 3).Render("No")
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, yes, baseStyle.Render("  "), no)

	return baseStyle.
		Width(maxWidth).
		Padding(0, 1).
		Render(buttons)
}

// SetSize sets the size of the component.
func (m *InitDialogCmp) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Bindings implements layout.Bindings.
func (m InitDialogCmp) Bindings() []key.Binding {
	return m.keys.ShortHelp()
}

// CloseInitDialogMsg is a message that is sent when the init dialog is closed.
type CloseInitDialogMsg struct {
	Initialize     bool // run AGENTS.md creation
	GenerateConfig bool // write .pando.toml to cwd
}

// ShowInitDialogMsg is a message that is sent to show the init dialog.
type ShowInitDialogMsg struct {
	Show bool
}

// ConfigGeneratedMsg is sent after .pando.toml has been successfully written to
// the current directory so the TUI can navigate to the settings page.
type ConfigGeneratedMsg struct {
	Path string
}
