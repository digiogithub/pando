package snapshots

import (
	"fmt"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// SessionsTableComponent is the public interface for the sessions table component.
type SessionsTableComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type sessionsTableCmp struct {
	table table.Model
	rows  []SessionRow
}

func (c *sessionsTableCmp) Init() tea.Cmd {
	return nil
}

func (c *sessionsTableCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SessionListMsg:
		c.rows = msg.Sessions
		c.syncRows()
		return c, c.selectedSessionCmd()
	}

	prevSelected := c.table.SelectedRow()
	t, cmd := c.table.Update(msg)
	c.table = t

	selected := c.table.SelectedRow()
	if selected != nil && (prevSelected == nil || selected[0] != prevSelected[0]) {
		return c, c.selectedSessionCmd()
	}

	return c, cmd
}

func (c *sessionsTableCmp) selectedSessionCmd() tea.Cmd {
	if len(c.rows) == 0 {
		c.table.SetCursor(0)
		return util.CmdHandler(SelectedSessionMsg{})
	}

	cursor := util.Clamp(c.table.Cursor(), 0, len(c.rows)-1)
	c.table.SetCursor(cursor)
	row := c.rows[cursor]
	return util.CmdHandler(SelectedSessionMsg{SessionID: row.SessionID})
}

func (c *sessionsTableCmp) View() string {
	t := theme.CurrentTheme()
	defaultStyles := table.DefaultStyles()
	defaultStyles.Selected = defaultStyles.Selected.Foreground(t.Primary())
	c.table.SetStyles(defaultStyles)
	return styles.ApplyThemeBackground(c.table.View(), t.Background())
}

func (c *sessionsTableCmp) GetSize() (int, int) {
	return c.table.Width(), c.table.Height()
}

func (c *sessionsTableCmp) SetSize(width int, height int) tea.Cmd {
	c.table.SetWidth(width)
	c.table.SetHeight(height)
	columns := c.table.Columns()
	for i, col := range columns {
		col.Width = (width / len(columns)) - 2
		columns[i] = col
	}
	c.table.SetColumns(columns)
	return nil
}

func (c *sessionsTableCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(c.table.KeyMap)
}

func (c *sessionsTableCmp) syncRows() {
	slices.SortFunc(c.rows, func(a, b SessionRow) int {
		if a.LatestAt > b.LatestAt {
			return -1
		}
		if a.LatestAt < b.LatestAt {
			return 1
		}
		return 0
	})

	tableRows := make([]table.Row, 0, len(c.rows))
	for _, row := range c.rows {
		tableRows = append(tableRows, table.Row{
			row.SessionID,
			fmt.Sprintf("%d", row.CommitCount),
			time.Unix(row.LatestAt, 0).Format("2006-01-02 15:04"),
			truncate(row.Description, 40),
		})
	}
	c.table.SetRows(tableRows)
}

// CommitsTableComponent is the public interface for the commits table component.
type CommitsTableComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type commitsTableCmp struct {
	table     table.Model
	sessionID string
	rows      []CommitRow
}

func (c *commitsTableCmp) Init() tea.Cmd {
	return nil
}

func (c *commitsTableCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case CommitListMsg:
		c.sessionID = msg.SessionID
		c.rows = msg.Commits
		c.syncRows()
		return c, c.selectedCommitCmd()
	}

	prevSelected := c.table.SelectedRow()
	t, cmd := c.table.Update(msg)
	c.table = t

	selected := c.table.SelectedRow()
	if selected != nil && (prevSelected == nil || selected[0] != prevSelected[0]) {
		return c, c.selectedCommitCmd()
	}

	return c, cmd
}

func (c *commitsTableCmp) selectedCommitCmd() tea.Cmd {
	if len(c.rows) == 0 {
		c.table.SetCursor(0)
		return util.CmdHandler(SelectedCommitMsg{})
	}

	cursor := util.Clamp(c.table.Cursor(), 0, len(c.rows)-1)
	c.table.SetCursor(cursor)
	row := c.rows[cursor]
	return util.CmdHandler(SelectedCommitMsg{Commit: row})
}

func (c *commitsTableCmp) View() string {
	t := theme.CurrentTheme()
	defaultStyles := table.DefaultStyles()
	defaultStyles.Selected = defaultStyles.Selected.Foreground(t.Primary())
	c.table.SetStyles(defaultStyles)
	return styles.ApplyThemeBackground(c.table.View(), t.Background())
}

func (c *commitsTableCmp) GetSize() (int, int) {
	return c.table.Width(), c.table.Height()
}

func (c *commitsTableCmp) SetSize(width int, height int) tea.Cmd {
	c.table.SetWidth(width)
	c.table.SetHeight(height)
	columns := c.table.Columns()
	for i, col := range columns {
		col.Width = (width / len(columns)) - 2
		columns[i] = col
	}
	c.table.SetColumns(columns)
	return nil
}

func (c *commitsTableCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(c.table.KeyMap)
}

func (c *commitsTableCmp) syncRows() {
	slices.SortFunc(c.rows, func(a, b CommitRow) int {
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		if a.CreatedAt < b.CreatedAt {
			return 1
		}
		return 0
	})

	tableRows := make([]table.Row, 0, len(c.rows))
	for _, row := range c.rows {
		tableRows = append(tableRows, table.Row{
			row.ShortID,
			typeIcon(row.Type),
			time.Unix(row.CreatedAt, 0).Format("2006-01-02 15:04"),
			fmt.Sprintf("%d", row.FileCount),
			formatSize(row.TotalSize),
			truncate(row.Description, 40),
		})
	}
	c.table.SetRows(tableRows)
}

// typeIcon returns a human-friendly label for the commit type.
func typeIcon(t string) string {
	switch t {
	case "start":
		return "○ start"
	case "delta":
		return "● delta"
	default:
		return t
	}
}

// formatSize converts bytes to a human-readable string.
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func NewSessionsTable() SessionsTableComponent {
	columns := []table.Column{
		{Title: "Session", Width: 20},
		{Title: "Commits", Width: 8},
		{Title: "Latest", Width: 16},
		{Title: "Description", Width: 30},
	}

	tableModel := table.New(table.WithColumns(columns))
	tableModel.Focus()

	return &sessionsTableCmp{
		table: tableModel,
		rows:  []SessionRow{},
	}
}

func NewCommitsTable() CommitsTableComponent {
	columns := []table.Column{
		{Title: "ID", Width: 10},
		{Title: "Type", Width: 10},
		{Title: "Date", Width: 16},
		{Title: "Files", Width: 6},
		{Title: "Size", Width: 10},
		{Title: "Description", Width: 30},
	}

	tableModel := table.New(table.WithColumns(columns))
	tableModel.Focus()

	return &commitsTableCmp{
		table: tableModel,
		rows:  []CommitRow{},
	}
}
