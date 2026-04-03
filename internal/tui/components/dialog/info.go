package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

const (
	infoDialogMaxWidth  = 72
	infoDialogMaxHeight = 30
)

// CloseInfoDialogMsg is sent when the info dialog is closed.
type CloseInfoDialogMsg struct{}

// ShowInfoDialogMsg triggers the info dialog with the given title and content.
type ShowInfoDialogMsg struct {
	Title   string
	Content string
}

var infoDialogCloseKey = key.NewBinding(
	key.WithKeys("esc", "q", "enter"),
	key.WithHelp("esc/q/enter", "close"),
)

// InfoDialogCmp is an overlay dialog that displays scrollable text content.
type InfoDialogCmp struct {
	title    string
	content  string
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

// NewInfoDialogCmp creates a new InfoDialogCmp.
func NewInfoDialogCmp() InfoDialogCmp {
	return InfoDialogCmp{}
}

// SetContent sets the title and text content of the dialog and resets the viewport.
func (m *InfoDialogCmp) SetContent(title, content string) {
	m.title = title
	m.content = content
	m.ready = false
}

// Init implements tea.Model.
func (m InfoDialogCmp) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m InfoDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, infoDialogCloseKey) {
			return m, util.CmdHandler(CloseInfoDialogMsg{})
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = false
	}

	if !m.ready {
		m = m.initViewport()
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m InfoDialogCmp) initViewport() InfoDialogCmp {
	contentWidth := infoDialogMaxWidth
	if m.width > 0 {
		contentWidth = min(infoDialogMaxWidth, m.width-8)
	}
	maxContentHeight := infoDialogMaxHeight
	if m.height > 0 {
		maxContentHeight = min(infoDialogMaxHeight, m.height-8)
	}

	// Inner width accounts for padding (1 char each side inside the border)
	innerWidth := contentWidth - 4
	if innerWidth < 20 {
		innerWidth = 20
	}

	wrapped := wordWrap(m.content, innerWidth)
	lines := strings.Split(wrapped, "\n")
	viewportHeight := min(len(lines), maxContentHeight)
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	vp := viewport.New(innerWidth, viewportHeight)
	vp.SetContent(wrapped)
	m.viewport = vp
	m.ready = true
	return m
}

// View implements tea.Model.
func (m InfoDialogCmp) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	if !m.ready {
		m = m.initViewport()
	}

	contentWidth := m.viewport.Width
	dialogWidth := contentWidth + 4 // inner padding

	title := base.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render(m.title)

	body := base.
		Width(dialogWidth).
		Padding(0, 1).
		Render(m.viewport.View())

	footer := base.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("↑/↓ scroll · esc/q/enter close")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		base.Width(dialogWidth).Render(""),
		body,
		base.Width(dialogWidth).Render(""),
		footer,
	)

	return base.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

// BindingKeys returns the key bindings for this dialog.
func (m InfoDialogCmp) BindingKeys() []key.Binding {
	return []key.Binding{infoDialogCloseKey}
}

// wordWrap breaks text at word boundaries to fit within maxWidth columns.
func wordWrap(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}
	var result strings.Builder
	paragraphs := strings.Split(text, "\n")
	for pi, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			result.WriteString("\n")
			continue
		}
		lineLen := 0
		for i, word := range words {
			wLen := len([]rune(word))
			if i == 0 {
				result.WriteString(word)
				lineLen = wLen
			} else if lineLen+1+wLen > maxWidth {
				result.WriteString("\n")
				result.WriteString(word)
				lineLen = wLen
			} else {
				result.WriteString(" ")
				result.WriteString(word)
				lineLen += 1 + wLen
			}
		}
		if pi < len(paragraphs)-1 {
			result.WriteString("\n")
		}
	}
	return result.String()
}
