package page

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/tui/components/snapshots"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/util"
)

// SnapshotPage is the public interface for the snapshots page.
type SnapshotPage interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type snapshotsPage struct {
	app           *app.App
	width, height int
	table         layout.Container
	details       layout.Container
}

type snapshotsLoadedMsg struct {
	rows []snapshots.SnapshotRow
	err  error
}

func (p *snapshotsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.SetSize(msg.Width, msg.Height)
	case snapshotsLoadedMsg:
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		cmds = append(cmds, util.CmdHandler(snapshots.SnapshotListMsg{Snapshots: msg.rows}))
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
	cmds := []tea.Cmd{
		p.table.Init(),
		p.details.Init(),
	}
	if cmd := p.loadSnapshotsCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// NewSnapshotsPage creates and returns a new snapshots page.
func NewSnapshotsPage(app *app.App) SnapshotPage {
	return &snapshotsPage{
		app:     app,
		table:   layout.NewContainer(snapshots.NewSnapshotsTable(), layout.WithBorderAll()),
		details: layout.NewContainer(snapshots.NewSnapshotsDetails(), layout.WithBorderAll()),
	}
}

func (p *snapshotsPage) loadSnapshotsCmd() tea.Cmd {
	if p.app == nil || p.app.AgentVCS == nil {
		return nil
	}

	return func() tea.Msg {
		ctx := context.Background()
		sessions, err := p.app.AgentVCS.ListSessions(ctx)
		if err != nil {
			return snapshotsLoadedMsg{err: err}
		}

		var rows []snapshots.SnapshotRow
		for _, sid := range sessions {
			log, err := p.app.AgentVCS.Log(ctx, sid)
			if err != nil {
				continue
			}
			for _, cs := range log {
				commitType := "delta"
				if cs.ParentID == "" {
					commitType = "start"
				}
				rows = append(rows, snapshots.SnapshotRow{
					ID:          cs.ID,
					SessionID:   cs.SessionID,
					Type:        commitType,
					Description: cs.Description,
					FileCount:   cs.FileCount,
					TotalSize:   cs.TotalSize,
					CreatedAt:   cs.CreatedAt,
				})
			}
		}
		return snapshotsLoadedMsg{rows: rows}
	}
}
