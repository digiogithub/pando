package dialog

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/project"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

const (
	projectsDialogMinWidth = 52
)

// OpenProjectsDialogMsg triggers opening the projects dialog.
type OpenProjectsDialogMsg struct{}

// ProjectActivatedMsg is fired when the user selects a project to activate.
type ProjectActivatedMsg struct {
	ProjectID string
	Path      string
}

// ProjectAddConfirmMsg is fired when the user confirms a new project path.
type ProjectAddConfirmMsg struct {
	Path string
	Name string // optional, empty = use basename
}

// ProjectRemoveMsg is fired when the user wants to remove a project.
type ProjectRemoveMsg struct {
	ProjectID string
}

// ProjectRenameMsg is fired when the user confirms a new name for a project.
type ProjectRenameMsg struct {
	ProjectID string
	NewName   string
}

// ProjectInitConfirmMsg is fired when ErrProjectNeedsInit was returned and
// the user confirms they want to initialize the path.
type ProjectInitConfirmMsg struct {
	ProjectID string
	Path      string
}

// CloseProjectsDialogMsg is fired when the dialog is dismissed.
type CloseProjectsDialogMsg struct{}

// ProjectsDialog interface for the project selection dialog.
type ProjectsDialog interface {
	tea.Model
	layout.Bindings
	SetProjects(projects []project.Project, activeID string)
}

type projectsKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Add    key.Binding
	Remove key.Binding
	Rename key.Binding
	Escape key.Binding
}

var projectsKeys = projectsKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "previous project"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "next project"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "activate project"),
	),
	Add: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add project"),
	),
	Remove: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "remove project"),
	),
	Rename: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "rename project"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
}

type projectsDialogCmp struct {
	projects    []project.Project
	activeID    string
	selectedIdx int
	width       int
	height      int
	// addMode is true when the user is typing a new project path.
	addMode    bool
	pathInput  textinput.Model
	// renameMode is true when the user is typing a new name for a project.
	renameMode  bool
	renameInput textinput.Model
}

func (p *projectsDialogCmp) Init() tea.Cmd {
	return nil
}

func (p *projectsDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if p.addMode {
			switch {
			case key.Matches(msg, projectsKeys.Enter):
				path := strings.TrimSpace(p.pathInput.Value())
				if path == "" {
					p.addMode = false
					p.pathInput.SetValue("")
					return p, nil
				}
				p.addMode = false
				p.pathInput.SetValue("")
				return p, util.CmdHandler(ProjectAddConfirmMsg{Path: path})
			case key.Matches(msg, projectsKeys.Escape):
				p.addMode = false
				p.pathInput.SetValue("")
				return p, nil
			default:
				var cmd tea.Cmd
				p.pathInput, cmd = p.pathInput.Update(msg)
				return p, cmd
			}
		}

		if p.renameMode {
			switch {
			case key.Matches(msg, projectsKeys.Enter):
				newName := strings.TrimSpace(p.renameInput.Value())
				if newName == "" || len(p.projects) == 0 {
					p.renameMode = false
					p.renameInput.SetValue("")
					return p, nil
				}
				selected := p.projects[p.selectedIdx]
				p.renameMode = false
				p.renameInput.SetValue("")
				return p, util.CmdHandler(ProjectRenameMsg{
					ProjectID: selected.ID,
					NewName:   newName,
				})
			case key.Matches(msg, projectsKeys.Escape):
				p.renameMode = false
				p.renameInput.SetValue("")
				return p, nil
			default:
				var cmd tea.Cmd
				p.renameInput, cmd = p.renameInput.Update(msg)
				return p, cmd
			}
		}

		// Normal mode
		switch {
		case key.Matches(msg, projectsKeys.Up):
			if p.selectedIdx > 0 {
				p.selectedIdx--
			}
			return p, nil
		case key.Matches(msg, projectsKeys.Down):
			if p.selectedIdx < len(p.projects)-1 {
				p.selectedIdx++
			}
			return p, nil
		case key.Matches(msg, projectsKeys.Enter):
			if len(p.projects) == 0 {
				return p, nil
			}
			selected := p.projects[p.selectedIdx]
			return p, util.CmdHandler(ProjectActivatedMsg{
				ProjectID: selected.ID,
				Path:      selected.Path,
			})
		case key.Matches(msg, projectsKeys.Add):
			p.addMode = true
			p.pathInput.SetValue("")
			p.pathInput.Focus()
			return p, textinput.Blink
		case key.Matches(msg, projectsKeys.Rename):
			if len(p.projects) == 0 {
				return p, nil
			}
			selected := p.projects[p.selectedIdx]
			p.renameMode = true
			p.renameInput.SetValue(selected.Name)
			p.renameInput.Focus()
			return p, textinput.Blink
		case key.Matches(msg, projectsKeys.Remove):
			if len(p.projects) == 0 {
				return p, nil
			}
			selected := p.projects[p.selectedIdx]
			return p, util.CmdHandler(ProjectRemoveMsg{ProjectID: selected.ID})
		case key.Matches(msg, projectsKeys.Escape):
			return p, util.CmdHandler(CloseProjectsDialogMsg{})
		}
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
	}
	return p, nil
}

// shortenPath replaces the user's home directory with "~".
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// statusIndicator returns the display character and colour for a project status.
func statusIndicator(status string, t theme.Theme) (string, lipgloss.AdaptiveColor) {
	switch status {
	case project.StatusRunning:
		return "●", t.Success()
	case project.StatusError:
		return "✗", t.Error()
	case project.StatusInitializing:
		return "↺", t.Warning()
	case project.StatusMissing:
		return "?", t.TextMuted()
	default: // stopped
		return "○", t.TextMuted()
	}
}

func (p *projectsDialogCmp) maxContentWidth() int {
	maxWidth := projectsDialogMinWidth
	for _, proj := range p.projects {
		short := shortenPath(proj.Path)
		// indicator + space + path + space + status badge
		lineWidth := 2 + len(short) + 12
		if lineWidth > maxWidth {
			maxWidth = lineWidth
		}
	}
	if p.width > 0 {
		maxWidth = max(projectsDialogMinWidth, min(maxWidth, p.width-10))
	}
	return maxWidth
}

func (p *projectsDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if p.addMode {
		return p.viewAddMode(t, baseStyle)
	}
	if p.renameMode {
		return p.viewRenameMode(t, baseStyle)
	}
	return p.viewNormalMode(t, baseStyle)
}

func (p *projectsDialogCmp) viewNormalMode(t theme.Theme, baseStyle lipgloss.Style) string {
	dialogWidth := p.maxContentWidth()

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render("Projects")

	var lines []string
	if len(p.projects) == 0 {
		lines = append(lines, baseStyle.Width(dialogWidth).Padding(0, 1).Foreground(t.TextMuted()).Render("No projects registered"))
	} else {
		for i, proj := range p.projects {
			selected := i == p.selectedIdx
			lines = append(lines, p.renderProjectItem(proj, selected, dialogWidth, t, baseStyle))
		}
	}

	footer := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("[a] Add  [d] Remove  [r] Rename  [↑↓/jk] Nav  [enter] Switch  [esc] Close")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
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

func (p *projectsDialogCmp) renderProjectItem(proj project.Project, selected bool, width int, t theme.Theme, baseStyle lipgloss.Style) string {
	indicator, color := statusIndicator(proj.Status, t)

	indicatorStyle := baseStyle.Foreground(color)
	if selected {
		indicatorStyle = indicatorStyle.Background(t.SelectionBackground())
	}
	indicatorStr := indicatorStyle.Render(indicator)

	short := shortenPath(proj.Path)
	pathStyle := baseStyle.Foreground(t.Text())
	if selected {
		pathStyle = pathStyle.
			Background(t.SelectionBackground()).
			Foreground(t.SelectionForeground()).
			Bold(true)
	}

	var suffix string
	if proj.ID == p.activeID {
		activeStyle := baseStyle.
			Foreground(t.Primary()).
			Bold(true)
		if selected {
			activeStyle = activeStyle.
				Background(t.SelectionBackground()).
				Foreground(t.SelectionForeground())
		}
		suffix = " " + activeStyle.Render("[active]")
	}

	pathStr := pathStyle.Render(short)
	line := indicatorStr + " " + pathStr + suffix

	itemStyle := baseStyle.Width(width).Padding(0, 1)
	if selected {
		itemStyle = itemStyle.Background(t.SelectionBackground())
	}
	return itemStyle.Render(line)
}

func (p *projectsDialogCmp) viewRenameMode(t theme.Theme, baseStyle lipgloss.Style) string {
	dialogWidth := projectsDialogMinWidth

	var currentName string
	if len(p.projects) > 0 {
		currentName = p.projects[p.selectedIdx].Name
	}

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render("Rename Project: " + currentName)

	p.renameInput.Width = max(0, dialogWidth-8)
	inputStyle := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.BorderFocused())
	inputStr := inputStyle.Render(p.renameInput.View())

	footer := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("[enter] Confirm  [esc] Cancel")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(dialogWidth).Render(""),
		inputStr,
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

func (p *projectsDialogCmp) viewAddMode(t theme.Theme, baseStyle lipgloss.Style) string {
	dialogWidth := projectsDialogMinWidth

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render("Add Project")

	p.pathInput.Width = max(0, dialogWidth-8)
	inputStyle := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.BorderFocused())
	inputStr := inputStyle.Render(p.pathInput.View())

	footer := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("[enter] Confirm  [esc] Cancel")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(dialogWidth).Render(""),
		inputStr,
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

func (p *projectsDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(projectsKeys)
}

func (p *projectsDialogCmp) SetProjects(projects []project.Project, activeID string) {
	p.projects = projects
	p.activeID = activeID
	// Reset selection to the active project if present.
	p.selectedIdx = 0
	for i, proj := range projects {
		if proj.ID == activeID {
			p.selectedIdx = i
			break
		}
	}
	if len(projects) > 0 {
		p.selectedIdx = min(p.selectedIdx, len(projects)-1)
	}
	p.addMode = false
	p.pathInput.SetValue("")
	p.renameMode = false
	p.renameInput.SetValue("")
}

// NewProjectsDialogCmp creates a new projects dialog.
func NewProjectsDialogCmp() ProjectsDialog {
	input := textinput.New()
	input.Placeholder = "~/code/my-project"
	input.Prompt = "Path: "
	input.CharLimit = 512

	rename := textinput.New()
	rename.Placeholder = "my-project"
	rename.Prompt = "Name: "
	rename.CharLimit = 128

	return &projectsDialogCmp{
		pathInput:   input,
		renameInput: rename,
	}
}

// shortenPathForDisplay is an alias kept for clarity in other files.
func shortenPathForDisplay(path string) string {
	return filepath.ToSlash(shortenPath(path))
}
