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

type infoDialogKeyMap struct {
	Close key.Binding
	Up    key.Binding
	Down  key.Binding
}

var infoDialogKeys = infoDialogKeyMap{
	Close: key.NewBinding(
		key.WithKeys("esc", "q", "enter"),
		key.WithHelp("esc/q/enter", "close"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "scroll up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "scroll down"),
	),
}

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

// SetContent sets the title and text content of the dialog.
func (m *InfoDialogCmp) SetContent(title, content string) {
	m.title = title
	m.content = content
	m.ready = false
}

func (m *InfoDialogCmp) Init() tea.Cmd {
	return nil
}

func (m *InfoDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, infoDialogKeys.Close):
			return m, util.CmdHandler(CloseInfoDialogMsg{})
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = false
	}

	if !m.ready {
		m.initViewport()
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *InfoDialogCmp) initViewport() {
	contentWidth := min(infoDialogMaxWidth, m.width-8)
	maxContentHeight := min(infoDialogMaxHeight, m.height-8)

	// Account for inner padding (2 chars each side)
	innerWidth := contentWidth - 4

	// Wrap content to fit the viewport width
	wrapped := wordWrap(m.content, innerWidth)
	lines := strings.Split(wrapped, "\n")
	viewportHeight := min(len(lines), maxContentHeight)

	m.viewport = viewport.New(innerWidth, viewportHeight)
	m.viewport.SetContent(wrapped)
	m.ready = true
}

func (m *InfoDialogCmp) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	if !m.ready {
		m.initViewport()
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

	// Scrollbar hint when content is scrollable
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

func (m *InfoDialogCmp) BindingKeys() []key.Binding {
	return []key.Binding{infoDialogKeys.Close, infoDialogKeys.Up, infoDialogKeys.Down}
}

// wordWrap breaks text at word boundaries to fit within maxWidth.
func wordWrap(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}
	var result strings.Builder
	for _, paragraph := range strings.Split(text, "\n") {
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
		result.WriteString("\n")
	}
	return strings.TrimRight(result.String(), "\n")
}
