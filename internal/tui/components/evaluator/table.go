package evaluator

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// TableComponent is the public interface for the evaluator template table.
type TableComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type tableCmp struct {
	table     table.Model
	templates []evaluator.TemplateStats
}

func (c *tableCmp) Init() tea.Cmd {
	return nil
}

func (c *tableCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	t, cmd := c.table.Update(msg)
	c.table = t
	return c, cmd
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
	if len(columns) > 0 {
		for i, col := range columns {
			col.Width = (width / len(columns)) - 2
			columns[i] = col
		}
		c.table.SetColumns(columns)
	}
	return nil
}

func (c *tableCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(c.table.KeyMap)
}

func (c *tableCmp) syncRows() {
	rows := make([]table.Row, 0, len(c.templates))
	for _, ts := range c.templates {
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", ts.Rank),
			ts.Template.Name,
			ts.Template.Section,
			fmt.Sprintf("%d", ts.Template.Version),
			fmt.Sprintf("%d", ts.TimesUsed),
			fmt.Sprintf("%.2f", ts.AvgReward),
			fmt.Sprintf("%.2f", ts.UCBScore),
		})
	}
	c.table.SetRows(rows)
}

// NewTableCmp creates a new template ranking table component.
func NewTableCmp(templates []evaluator.TemplateStats) TableComponent {
	columns := []table.Column{
		{Title: "#", Width: 4},
		{Title: "Name", Width: 20},
		{Title: "Section", Width: 14},
		{Title: "Ver", Width: 4},
		{Title: "Used", Width: 6},
		{Title: "Avg R", Width: 7},
		{Title: "UCB", Width: 7},
	}

	tableModel := table.New(
		table.WithColumns(columns),
	)
	tableModel.Focus()

	c := &tableCmp{
		table:     tableModel,
		templates: templates,
	}
	c.syncRows()
	return c
}
