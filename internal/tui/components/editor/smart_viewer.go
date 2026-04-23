package editor

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// smartViewer is a composite FileViewerComponent that dispatches to the
// appropriate specialized viewer based on the file extension:
//   - .md  → markdownViewer (rendered markdown via teacup)
//   - image files → imageViewer (terminal image via teacup)
//   - everything else → fileViewer (syntax-highlighted text)
type smartViewer struct {
	width    int
	height   int
	filePath string

	text     *fileViewer
	markdown *markdownViewer
	image    *imageViewer

	active FileViewerComponent
}

// NewSmartFileViewer creates a composite file viewer that automatically
// selects the best renderer for the given file type.
func NewSmartFileViewer() FileViewerComponent {
	sv := &smartViewer{
		text:     NewFileViewer().(*fileViewer),
		markdown: newMarkdownViewer(),
		image:    newImageViewer(),
	}
	sv.active = sv.text
	return sv
}

func (sv *smartViewer) Init() tea.Cmd {
	return tea.Batch(sv.text.Init(), sv.markdown.Init(), sv.image.Init())
}

func (sv *smartViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := sv.active.Update(msg)
	sv.active = updated.(FileViewerComponent)
	sv.syncActive()
	return sv, cmd
}

func (sv *smartViewer) View() string {
	return sv.active.View()
}

func (sv *smartViewer) SetSize(width, height int) tea.Cmd {
	sv.width = max(width, 0)
	sv.height = max(height, 0)
	return sv.active.SetSize(sv.width, sv.height)
}

func (sv *smartViewer) GetSize() (int, int) {
	return sv.width, sv.height
}

func (sv *smartViewer) BindingKeys() []key.Binding {
	return sv.active.BindingKeys()
}

func (sv *smartViewer) FilePath() string {
	return sv.filePath
}

func (sv *smartViewer) OpenFile(path string) tea.Cmd {
	sv.filePath = path
	next := sv.viewerFor(path)

	if next != sv.active {
		// Switch to the new viewer and resize it.
		sv.active = next
		if sv.width > 0 || sv.height > 0 {
			_ = sv.active.SetSize(sv.width, sv.height)
		}
	}

	return sv.active.OpenFile(path)
}

// viewerFor selects the appropriate FileViewerComponent for a given file path.
func (sv *smartViewer) viewerFor(path string) FileViewerComponent {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case ext == ".md":
		return sv.markdown
	case IsImageFile(path):
		return sv.image
	default:
		return sv.text
	}
}

// syncActive keeps the internal pointer consistent after Update returns a new
// model value (Update on a concrete type returns tea.Model, not the pointer).
func (sv *smartViewer) syncActive() {
	switch sv.active.(type) {
	case *fileViewer:
		sv.text = sv.active.(*fileViewer)
	case *markdownViewer:
		sv.markdown = sv.active.(*markdownViewer)
	case *imageViewer:
		sv.image = sv.active.(*imageViewer)
	}
}
