package editor

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tcimage "github.com/mistakenelf/teacup/image"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
)

// imageExtensions holds the file extensions recognised as images.
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
	".webp": true,
	".tiff": true,
	".tif":  true,
}

// IsImageFile reports whether path is a supported image file.
func IsImageFile(path string) bool {
	ext := filepath.Ext(path)
	if ext == "" {
		return false
	}
	return imageExtensions[filepath.Ext(path)]
}

// imageViewer is a read-only viewer for image files that renders via teacup.
type imageViewer struct {
	model    tcimage.Model
	width    int
	height   int
	filePath string
}

// newImageViewer creates a new image file viewer.
func newImageViewer() *imageViewer {
	t := tuitheme.CurrentTheme()
	borderColor := lipgloss.AdaptiveColor{
		Light: t.Primary().Light,
		Dark:  t.Primary().Dark,
	}
	m := tcimage.New(true, true, borderColor)
	return &imageViewer{model: m}
}

func (v *imageViewer) Init() tea.Cmd {
	return v.model.Init()
}

func (v *imageViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := v.model.Update(msg)
	v.model = updated
	return v, cmd
}

func (v *imageViewer) View() string {
	if v.width <= 0 || v.height <= 0 {
		return ""
	}
	t := tuitheme.CurrentTheme()

	content := styles.BaseStyle().
		Width(v.width).
		Height(v.height - 1).
		Render(v.model.View())

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

	right := rightStyle.Render("Image")
	available := max(v.width-lipgloss.Width(right), 0)
	left := leftStyle.Width(available).Render(truncateRunes(name, available))
	status := lipgloss.JoinHorizontal(lipgloss.Left, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, content, status)
}

func (v *imageViewer) SetSize(width, height int) tea.Cmd {
	v.width = max(width, 0)
	v.height = max(height, 0)
	return v.model.SetSize(v.width, max(v.height-1, 0))
}

func (v *imageViewer) GetSize() (int, int) {
	return v.width, v.height
}

func (v *imageViewer) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(DefaultViewerKeyMap())
}

func (v *imageViewer) OpenFile(path string) tea.Cmd {
	v.filePath = path
	return v.model.SetFileName(path)
}

func (v *imageViewer) FilePath() string {
	return v.filePath
}
