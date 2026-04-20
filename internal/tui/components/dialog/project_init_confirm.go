package dialog

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// ProjectInitYesMsg is fired when the user confirms project initialization.
type ProjectInitYesMsg struct {
	ProjectID string
	Path      string
}

// ProjectInitNoMsg is fired when the user declines project initialization.
type ProjectInitNoMsg struct{}

// ProjectInitConfirmDialog is the interface for the init confirmation dialog.
type ProjectInitConfirmDialog interface {
	tea.Model
	layout.Bindings
	SetProject(id, path string)
}

type projectInitKeyMap struct {
	Yes    key.Binding
	No     key.Binding
	Enter  key.Binding
	Escape key.Binding
}

var projectInitKeys = projectInitKeyMap{
	Yes: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "yes, initialize"),
	),
	No: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "no, cancel"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "yes, initialize"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
}

type projectInitConfirmDialogCmp struct {
	projectID string
	path      string
	width     int
	height    int
}

func (d *projectInitConfirmDialogCmp) Init() tea.Cmd {
	return nil
}

func (d *projectInitConfirmDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, projectInitKeys.Yes), key.Matches(msg, projectInitKeys.Enter):
			return d, util.CmdHandler(ProjectInitYesMsg{
				ProjectID: d.projectID,
				Path:      d.path,
			})
		case key.Matches(msg, projectInitKeys.No), key.Matches(msg, projectInitKeys.Escape):
			return d, util.CmdHandler(ProjectInitNoMsg{})
		}
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
	}
	return d, nil
}

func (d *projectInitConfirmDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	dialogWidth := 44

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render("Initialize Project?")

	short := shortenPathForDisplay(d.path)
	pathLine := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.Text()).
		Render("Path: " + short)

	msg1 := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("No Pando config found.")

	msg2 := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("Create .pando.toml and directory structure?")

	footer := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("[y/enter] Yes    [n/esc] No")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(dialogWidth).Render(""),
		pathLine,
		baseStyle.Width(dialogWidth).Render(""),
		msg1,
		msg2,
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

func (d *projectInitConfirmDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(projectInitKeys)
}

func (d *projectInitConfirmDialogCmp) SetProject(id, path string) {
	d.projectID = id
	d.path = path
}

// NewProjectInitConfirmDialogCmp creates a new project init confirmation dialog.
func NewProjectInitConfirmDialogCmp() ProjectInitConfirmDialog {
	return &projectInitConfirmDialogCmp{}
}
