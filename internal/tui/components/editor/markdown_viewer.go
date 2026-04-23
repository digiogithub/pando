package editor

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tcmarkdown "github.com/mistakenelf/teacup/markdown"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
)

// markdownViewer is a read-only viewer for .md files that renders markdown via teacup.
type markdownViewer struct {
	model    tcmarkdown.Model
	width    int
	height   int
	filePath string
}

// newMarkdownViewer creates a new markdown file viewer.
func newMarkdownViewer() *markdownViewer {
	t := tuitheme.CurrentTheme()
	borderColor := lipgloss.AdaptiveColor{
		Light: t.Primary().Light,
		Dark:  t.Primary().Dark,
	}
	m := tcmarkdown.New(true, true, borderColor)
	return &markdownViewer{model: m}
}

func (v *markdownViewer) Init() tea.Cmd {
	return v.model.Init()
}

func (v *markdownViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := v.model.Update(msg)
	v.model = updated
	return v, cmd
}

func (v *markdownViewer) View() string {
	if v.width <= 0 || v.height <= 0 {
		return ""
	}
	t := tuitheme.CurrentTheme()

	content := styles.BaseStyle().
		Width(v.width).
		Height(v.height - 1).
		Render(v.model.View())

	// Status line shows the file name
	name := filepath.Base(v.filePath)
	if name == "" || name == "." {
		name = "No file"
	}
	leftStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)
	rightStyle := lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)

	right := rightStyle.Render("Markdown")
	available := max(v.width-lipgloss.Width(right), 0)
	left := leftStyle.Width(available).Render(truncateRunes(name, available))
	status := lipgloss.JoinHorizontal(lipgloss.Left, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, content, status)
}

func (v *markdownViewer) SetSize(width, height int) tea.Cmd {
	v.width = max(width, 0)
	v.height = max(height, 0)
	return v.model.SetSize(v.width, max(v.height-1, 0))
}

func (v *markdownViewer) GetSize() (int, int) {
	return v.width, v.height
}

func (v *markdownViewer) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(DefaultViewerKeyMap())
}

func (v *markdownViewer) OpenFile(path string) tea.Cmd {
	v.filePath = path
	return v.model.SetFileName(path)
}

func (v *markdownViewer) FilePath() string {
	return v.filePath
}
