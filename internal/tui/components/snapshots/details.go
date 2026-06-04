package snapshots

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/agentvcs"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// DetailComponent is the public interface for the commit detail component.
type DetailComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type detailCmp struct {
	width, height int
	commit        CommitRow
	diff          []agentvcs.DiffEntry
	err           error
	viewport      viewport.Model
}

func (d *detailCmp) Init() tea.Cmd {
	return nil
}

func (d *detailCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SelectedCommitMsg:
		d.commit = msg.Commit
		d.err = nil
		d.diff = nil
		d.updateContent()
	case CommitDetailsMsg:
		d.err = msg.Err
		d.diff = msg.Diff
		d.updateContent()
	}
	return d, nil
}

func (d *detailCmp) updateContent() {
	if d.commit.ID == "" {
		d.viewport.SetContent("No commit selected.")
		return
	}

	var content strings.Builder
	t := theme.CurrentTheme()

	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(t.Primary())
	valueStyle := lipgloss.NewStyle().Foreground(t.Text())
	mutedStyle := lipgloss.NewStyle().Foreground(t.TextMuted())

	field := func(label, value string) string {
		return fmt.Sprintf("%s %s", labelStyle.Render(label+":"), valueStyle.Render(value))
	}

	createdAt := time.Unix(d.commit.CreatedAt, 0).Format("2006-01-02 15:04:05")
	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Foreground(t.Secondary()).Render(typeIcon(d.commit.Type)),
		"  ",
		mutedStyle.Render(createdAt),
	)
	content.WriteString(lipgloss.NewStyle().Bold(true).Render(header))
	content.WriteString("\n\n")

	padding := lipgloss.NewStyle().Padding(0, 2)
	content.WriteString(padding.Render(field("ID", d.commit.ID)))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Session", d.commit.SessionID)))
	content.WriteString("\n")
	if d.commit.ParentID != "" {
		content.WriteString(padding.Render(field("Parent", d.commit.ParentID)))
		content.WriteString("\n")
	}
	content.WriteString(padding.Render(field("Type", d.commit.Type)))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Date", createdAt)))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Files", fmt.Sprintf("%d", d.commit.FileCount))))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Size", formatSize(d.commit.TotalSize))))
	content.WriteString("\n")
	if d.commit.Description != "" {
		content.WriteString("\n")
		content.WriteString(padding.Render(field("Description", d.commit.Description)))
		content.WriteString("\n")
	}

	content.WriteString("\n")
	content.WriteString(labelStyle.Render("Diff:"))
	content.WriteString("\n")

	if d.err != nil {
		content.WriteString(padding.Render(fmt.Sprintf("Failed to load diff: %v", d.err)))
		content.WriteString("\n")
	} else if len(d.diff) == 0 {
		content.WriteString(padding.Render("No file changes."))
		content.WriteString("\n")
	} else {
		for _, entry := range d.diff {
			prefix := "M"
			switch entry.Type {
			case agentvcs.DiffAdded:
				prefix = "A"
			case agentvcs.DiffDeleted:
				prefix = "D"
			case agentvcs.DiffModified:
				prefix = "M"
			}
			sizeInfo := ""
			if entry.NewSize > 0 {
				sizeInfo = fmt.Sprintf(" (%s)", formatSize(entry.NewSize))
			}
			content.WriteString(padding.Render(fmt.Sprintf("%s %s%s", prefix, entry.Path, sizeInfo)))
			content.WriteString("\n")
		}
	}

	content.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(t.TextMuted()).Italic(true)
	content.WriteString(hintStyle.Render("[r] Revert selected commit"))

	d.viewport.SetContent(content.String())
}

func (d *detailCmp) View() string {
	t := theme.CurrentTheme()
	return styles.ForceReplaceBackgroundWithLipgloss(d.viewport.View(), t.Background())
}

func (d *detailCmp) GetSize() (int, int) {
	return d.width, d.height
}

func (d *detailCmp) SetSize(width int, height int) tea.Cmd {
	d.width = width
	d.height = height
	d.viewport.Width = width
	d.viewport.Height = height
	d.updateContent()
	return nil
}

func (d *detailCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(d.viewport.KeyMap)
}

// NewSnapshotsDetails creates and returns a new snapshot detail component.
func NewSnapshotsDetails() DetailComponent {
	return &detailCmp{viewport: viewport.New(0, 0)}
}
