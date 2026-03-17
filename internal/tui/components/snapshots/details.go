package snapshots

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// DetailComponent is the public interface for the snapshot detail component.
type DetailComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type detailCmp struct {
	width, height   int
	currentSnapshot SelectedSnapshotMsg
	viewport        viewport.Model
}

func (d *detailCmp) Init() tea.Cmd {
	return nil
}

func (d *detailCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SelectedSnapshotMsg:
		if msg.ID != d.currentSnapshot.ID {
			d.currentSnapshot = msg
			d.updateContent()
		}
	}
	return d, nil
}

func (d *detailCmp) updateContent() {
	if d.currentSnapshot.ID == "" {
		d.viewport.SetContent("No snapshot selected.")
		return
	}

	var content strings.Builder
	t := theme.CurrentTheme()

	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(t.Primary())
	valueStyle := lipgloss.NewStyle().Foreground(t.Text())
	mutedStyle := lipgloss.NewStyle().Foreground(t.TextMuted())

	field := func(label, value string) string {
		return fmt.Sprintf("%s %s",
			labelStyle.Render(label+":"),
			valueStyle.Render(value),
		)
	}

	// Header: type icon + date
	createdAt := time.Unix(d.currentSnapshot.CreatedAt, 0).Format("2006-01-02 15:04:05")
	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Foreground(t.Secondary()).Render(typeIcon(d.currentSnapshot.Type)),
		"  ",
		mutedStyle.Render(createdAt),
	)
	content.WriteString(lipgloss.NewStyle().Bold(true).Render(header))
	content.WriteString("\n\n")

	// Fields
	padding := lipgloss.NewStyle().Padding(0, 2)

	content.WriteString(padding.Render(field("ID", d.currentSnapshot.ID)))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Session", d.currentSnapshot.SessionID)))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Type", d.currentSnapshot.Type)))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Date", createdAt)))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Files", fmt.Sprintf("%d", d.currentSnapshot.FileCount))))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Size", formatSize(d.currentSnapshot.TotalSize))))
	content.WriteString("\n")
	content.WriteString(padding.Render(field("Working Dir", d.currentSnapshot.WorkingDir)))
	content.WriteString("\n")

	if d.currentSnapshot.Description != "" {
		content.WriteString("\n")
		content.WriteString(padding.Render(field("Description", d.currentSnapshot.Description)))
		content.WriteString("\n")
	}

	// Action hints
	content.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(t.TextMuted()).Italic(true)
	content.WriteString(hintStyle.Render("[r] Revert  [d] Delete  [c] Compare"))

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
	return &detailCmp{
		viewport: viewport.New(0, 0),
	}
}
