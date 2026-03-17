package page

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/components/snapshots"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
)

// SnapshotPage is the public interface for the snapshots page.
type SnapshotPage interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type snapshotsPage struct {
	width, height int
	table         layout.Container
	details       layout.Container
}

func (p *snapshotsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.SetSize(msg.Width, msg.Height)
	}

	table, cmd := p.table.Update(msg)
	cmds = append(cmds, cmd)
	p.table = table.(layout.Container)

	details, cmd := p.details.Update(msg)
	cmds = append(cmds, cmd)
	p.details = details.(layout.Container)

	return p, tea.Batch(cmds...)
}

func (p *snapshotsPage) View() string {
	style := styles.BaseStyle().Width(p.width).Height(p.height)
	return style.Render(lipgloss.JoinVertical(lipgloss.Top,
		p.table.View(),
		p.details.View(),
	))
}

func (p *snapshotsPage) BindingKeys() []key.Binding {
	return p.table.BindingKeys()
}

// GetSize implements SnapshotsPage.
func (p *snapshotsPage) GetSize() (int, int) {
	return p.width, p.height
}

// SetSize implements SnapshotsPage.
func (p *snapshotsPage) SetSize(width int, height int) tea.Cmd {
	p.width = width
	p.height = height
	return tea.Batch(
		p.table.SetSize(width, height/2),
		p.details.SetSize(width, height/2),
	)
}

func (p *snapshotsPage) Init() tea.Cmd {
	return tea.Batch(
		p.table.Init(),
		p.details.Init(),
	)
}

// NewSnapshotsPage creates and returns a new snapshots page.
func NewSnapshotsPage() SnapshotsPage {
	return &snapshotsPage{
		table:   layout.NewContainer(snapshots.NewSnapshotsTable(), layout.WithBorderAll()),
		details: layout.NewContainer(snapshots.NewSnapshotsDetails(), layout.WithBorderAll()),
	}
}
