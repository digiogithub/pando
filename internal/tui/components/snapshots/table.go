package snapshots

import (
	"fmt"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/snapshot"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// TableComponent is the public interface for the snapshot table component.
type TableComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type tableCmp struct {
	table table.Model
	rows  []SnapshotRow
}

func (c *tableCmp) Init() tea.Cmd {
	return nil
}

func (c *tableCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case pubsub.Event[snapshot.Snapshot]:
		snap := msg.Payload
		row := snapshotToRow(snap)

		switch msg.Type {
		case pubsub.DeletedEvent:
			c.rows = slices.DeleteFunc(c.rows, func(r SnapshotRow) bool {
				return r.ID == snap.ID
			})
		case pubsub.CreatedEvent, pubsub.UpdatedEvent:
			found := false
			for i, r := range c.rows {
				if r.ID == snap.ID {
					c.rows[i] = row
					found = true
					break
				}
			}
			if !found {
				c.rows = append(c.rows, row)
			}
		}

		c.syncRows()
		return c, c.selectedSnapshotCmd()

	case SnapshotListMsg:
		c.rows = msg.Snapshots
		c.syncRows()
		return c, c.selectedSnapshotCmd()
	}

	prevSelected := c.table.SelectedRow()
	t, cmd := c.table.Update(msg)
	cmds = append(cmds, cmd)
	c.table = t

	selected := c.table.SelectedRow()
	if selected != nil {
		if prevSelected == nil || selected[0] != prevSelected[0] {
			for _, row := range c.rows {
				if row.ID == selected[0] {
					cmds = append(cmds, util.CmdHandler(SelectedSnapshotMsg{
						ID:          row.ID,
						SessionID:   row.SessionID,
						Type:        row.Type,
						Description: row.Description,
						WorkingDir:  row.WorkingDir,
						FileCount:   row.FileCount,
						TotalSize:   row.TotalSize,
						CreatedAt:   row.CreatedAt,
					}))
					break
				}
			}
		}
	}

	return c, tea.Batch(cmds...)
}

func (c *tableCmp) selectedSnapshotCmd() tea.Cmd {
	if len(c.rows) == 0 {
		c.table.SetCursor(0)
		return util.CmdHandler(SelectedSnapshotMsg{})
	}

	cursor := util.Clamp(c.table.Cursor(), 0, len(c.rows)-1)
	c.table.SetCursor(cursor)
	row := c.rows[cursor]
	return util.CmdHandler(SelectedSnapshotMsg{
		ID:          row.ID,
		SessionID:   row.SessionID,
		Type:        row.Type,
		Description: row.Description,
		WorkingDir:  row.WorkingDir,
		FileCount:   row.FileCount,
		TotalSize:   row.TotalSize,
		CreatedAt:   row.CreatedAt,
	})
}

func (c *tableCmp) View() string {
	t := theme.CurrentTheme()
	defaultStyles := table.DefaultStyles()
	defaultStyles.Selected = defaultStyles.Selected.Foreground(t.Primary())
	c.table.SetStyles(defaultStyles)
	return styles.ForceReplaceBackgroundWithLipgloss(c.table.View(), t.Background())
}

func (c *tableCmp) GetSize() (int, int) {
	return c.table.Width(), c.table.Height()
}

func (c *tableCmp) SetSize(width int, height int) tea.Cmd {
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

func (c *tableCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(c.table.KeyMap)
}

// syncRows sorts c.rows newest-first and pushes them into the table model.
func (c *tableCmp) syncRows() {
	slices.SortFunc(c.rows, func(a, b SnapshotRow) int {
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
			row.ID,
			row.SessionID,
			typeIcon(row.Type),
			time.Unix(row.CreatedAt, 0).Format("2006-01-02 15:04"),
			fmt.Sprintf("%d", row.FileCount),
			formatSize(row.TotalSize),
		})
	}
	c.table.SetRows(tableRows)
}

// typeIcon returns a human-friendly label with an icon for the snapshot type.
func typeIcon(t string) string {
	switch t {
	case snapshot.SnapshotTypeStart:
		return "⬆ start"
	case snapshot.SnapshotTypeEnd:
		return "⬇ end"
	case snapshot.SnapshotTypeManual:
		return "📌 manual"
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

// snapshotToRow converts a snapshot.Snapshot into a SnapshotRow.
func snapshotToRow(snap snapshot.Snapshot) SnapshotRow {
	return SnapshotRow{
		ID:          snap.ID,
		SessionID:   snap.SessionID,
		Type:        snap.Type,
		Description: snap.Description,
		WorkingDir:  snap.WorkingDir,
		FileCount:   snap.FileCount,
		TotalSize:   snap.TotalSize,
		CreatedAt:   snap.CreatedAt,
	}
}

// NewSnapshotsTable creates and returns a new snapshot table component.
func NewSnapshotsTable() TableComponent {
	columns := []table.Column{
		{Title: "ID", Width: 10},
		{Title: "Session", Width: 10},
		{Title: "Type", Width: 10},
		{Title: "Date", Width: 16},
		{Title: "Files", Width: 6},
		{Title: "Size", Width: 10},
	}

	tableModel := table.New(
		table.WithColumns(columns),
	)
	tableModel.Focus()

	return &tableCmp{
		table: tableModel,
		rows:  []SnapshotRow{},
	}
}
