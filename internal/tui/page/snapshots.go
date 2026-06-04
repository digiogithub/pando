package page

import (
	"context"
	"fmt"

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
	sessions      layout.Container
	commits       layout.Container
	details       layout.Container
	selected      snapshots.CommitRow
	confirmRevert bool
}

type snapshotsLoadedMsg struct {
	sessions []snapshots.SessionRow
	err      error
}

type commitsLoadedMsg struct {
	sessionID string
	commits   []snapshots.CommitRow
	err       error
}

type revertDoneMsg struct {
	err error
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
		cmds = append(cmds, util.CmdHandler(snapshots.SessionListMsg{Sessions: msg.sessions}))

	case snapshots.SelectedSessionMsg:
		if msg.SessionID != "" {
			cmds = append(cmds, p.loadCommitsCmd(msg.SessionID))
		}

	case commitsLoadedMsg:
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		cmds = append(cmds, util.CmdHandler(snapshots.CommitListMsg{SessionID: msg.sessionID, Commits: msg.commits}))

	case snapshots.SelectedCommitMsg:
		p.selected = msg.Commit
		if msg.Commit.ID != "" {
			cmds = append(cmds, p.loadCommitDetailsCmd(msg.Commit.ID))
		}

	case tea.KeyMsg:
		if p.selected.ID != "" && msg.String() == "r" {
			if !p.confirmRevert {
				p.confirmRevert = true
				return p, util.ReportInfo("Press r again to confirm revert")
			}
			p.confirmRevert = false
			cmds = append(cmds, p.revertCommitCmd(p.selected.ID))
		}
		if msg.String() != "r" {
			p.confirmRevert = false
		}

	case revertDoneMsg:
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		cmds = append(cmds,
			util.ReportInfo(fmt.Sprintf("Reverted to commit %s", p.selected.ShortID)),
			p.loadSnapshotsCmd(),
		)
	}

	sessionsCmp, cmd := p.sessions.Update(msg)
	cmds = append(cmds, cmd)
	p.sessions = sessionsCmp.(layout.Container)

	commitsCmp, cmd := p.commits.Update(msg)
	cmds = append(cmds, cmd)
	p.commits = commitsCmp.(layout.Container)

	detailsCmp, cmd := p.details.Update(msg)
	cmds = append(cmds, cmd)
	p.details = detailsCmp.(layout.Container)

	return p, tea.Batch(cmds...)
}

func (p *snapshotsPage) View() string {
	style := styles.BaseStyle().Width(p.width).Height(p.height)
	return style.Render(lipgloss.JoinVertical(lipgloss.Top,
		p.sessions.View(),
		p.commits.View(),
		p.details.View(),
	))
}

func (p *snapshotsPage) BindingKeys() []key.Binding {
	keys := p.sessions.BindingKeys()
	keys = append(keys, p.commits.BindingKeys()...)
	return keys
}

func (p *snapshotsPage) GetSize() (int, int) {
	return p.width, p.height
}

func (p *snapshotsPage) SetSize(width int, height int) tea.Cmd {
	p.width = width
	p.height = height
	third := height / 3
	return tea.Batch(
		p.sessions.SetSize(width, third),
		p.commits.SetSize(width, third),
		p.details.SetSize(width, height-(third*2)),
	)
}

func (p *snapshotsPage) Init() tea.Cmd {
	cmds := []tea.Cmd{p.sessions.Init(), p.commits.Init(), p.details.Init()}
	if cmd := p.loadSnapshotsCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// NewSnapshotsPage creates and returns a new snapshots page.
func NewSnapshotsPage(app *app.App) SnapshotPage {
	return &snapshotsPage{
		app:      app,
		sessions: layout.NewContainer(snapshots.NewSessionsTable(), layout.WithBorderAll()),
		commits:  layout.NewContainer(snapshots.NewCommitsTable(), layout.WithBorderAll()),
		details:  layout.NewContainer(snapshots.NewSnapshotsDetails(), layout.WithBorderAll()),
	}
}

func (p *snapshotsPage) loadSnapshotsCmd() tea.Cmd {
	if p.app == nil || p.app.AgentVCS == nil {
		return nil
	}

	return func() tea.Msg {
		ctx := context.Background()
		sessionIDs, err := p.app.AgentVCS.ListSessions(ctx)
		if err != nil {
			return snapshotsLoadedMsg{err: err}
		}

		rows := make([]snapshots.SessionRow, 0, len(sessionIDs))
		for _, sid := range sessionIDs {
			log, err := p.app.AgentVCS.Log(ctx, sid)
			if err != nil || len(log) == 0 {
				continue
			}
			latest := log[len(log)-1]
			rows = append(rows, snapshots.SessionRow{
				SessionID:    sid,
				CommitCount:  len(log),
				LatestAt:     latest.CreatedAt,
				LatestCommit: latest.ShortID,
				Description:  latest.Description,
			})
		}

		return snapshotsLoadedMsg{sessions: rows}
	}
}

func (p *snapshotsPage) loadCommitsCmd(sessionID string) tea.Cmd {
	if p.app == nil || p.app.AgentVCS == nil || sessionID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx := context.Background()
		log, err := p.app.AgentVCS.Log(ctx, sessionID)
		if err != nil {
			return commitsLoadedMsg{err: err}
		}

		rows := make([]snapshots.CommitRow, 0, len(log))
		for _, cs := range log {
			commitType := "delta"
			if cs.ParentID == "" {
				commitType = "start"
			}
			rows = append(rows, snapshots.CommitRow{
				ID:          cs.ID,
				ShortID:     cs.ShortID,
				ParentID:    cs.ParentID,
				SessionID:   cs.SessionID,
				Type:        commitType,
				Description: cs.Description,
				FileCount:   cs.FileCount,
				TotalSize:   cs.TotalSize,
				CreatedAt:   cs.CreatedAt,
			})
		}
		return commitsLoadedMsg{sessionID: sessionID, commits: rows}
	}
}

func (p *snapshotsPage) loadCommitDetailsCmd(commitID string) tea.Cmd {
	if p.app == nil || p.app.AgentVCS == nil || commitID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx := context.Background()
		commit, err := p.app.AgentVCS.GetCommit(ctx, commitID)
		if err != nil {
			return snapshots.CommitDetailsMsg{Err: err}
		}
		diff, diffErr := p.app.AgentVCS.DiffFromParent(ctx, commitID)
		return snapshots.CommitDetailsMsg{Commit: commit, Diff: diff, Err: diffErr}
	}
}

func (p *snapshotsPage) revertCommitCmd(commitID string) tea.Cmd {
	if p.app == nil || p.app.AgentVCS == nil || commitID == "" {
		return nil
	}

	return func() tea.Msg {
		err := p.app.AgentVCS.Revert(context.Background(), commitID)
		return revertDoneMsg{err: err}
	}
}
