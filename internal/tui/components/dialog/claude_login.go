package dialog

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// ClaudeLoginCodeSubmitMsg is sent when the user submits a manual authorization code.
type ClaudeLoginCodeSubmitMsg struct {
	Code        string
	RedirectURI string // always auth.ClaudeManualRedirectURL
}

// ClaudeLoginDialogCancelMsg is sent when the user cancels the login dialog.
type ClaudeLoginDialogCancelMsg struct{}

// ClaudeLoginDialogCmp is the OAuth login dialog. It shows the authorization URL
// (manual flow) and a text input for pasting the code returned by platform.claude.com.
// It is closed automatically when the automatic browser callback delivers the code.
type ClaudeLoginDialogCmp struct {
	session       *auth.ClaudeLoginSession
	input         textinput.Model
	width, height int
	err           string // last validation error shown to the user
}

// NewClaudeLoginDialogCmp creates a new login dialog for the given session.
func NewClaudeLoginDialogCmp(session *auth.ClaudeLoginSession) ClaudeLoginDialogCmp {
	t := theme.CurrentTheme()

	ti := textinput.New()
	ti.Placeholder = "Paste authorization code or URL…"
	ti.Width = 58
	ti.Prompt = " "
	ti.PlaceholderStyle = ti.PlaceholderStyle.Background(t.Background())
	ti.PromptStyle = ti.PromptStyle.Background(t.Background())
	ti.TextStyle = ti.TextStyle.Background(t.Background()).Foreground(t.Text())
	ti.Focus()

	return ClaudeLoginDialogCmp{
		session: session,
		input:   ti,
	}
}

// Init implements tea.Model.
func (m ClaudeLoginDialogCmp) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m ClaudeLoginDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			return m, util.CmdHandler(ClaudeLoginDialogCancelMsg{})

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			raw := m.input.Value()
			code := m.session.ExtractCodeFromInput(raw)
			if code == "" {
				m.err = "Invalid code or URL — paste the full URL or the code shown on platform.claude.com"
				return m, nil
			}
			m.err = ""
			return m, util.CmdHandler(ClaudeLoginCodeSubmitMsg{
				Code:        code,
				RedirectURI: auth.ClaudeManualRedirectURL,
			})
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m ClaudeLoginDialogCmp) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	bg := t.Background()

	const dialogWidth = 64

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).Bold(true).
		Background(bg).Padding(0, 1).
		Render("Sign in to Claude.ai")

	hint := lipgloss.NewStyle().
		Foreground(t.TextMuted()).Background(bg).
		Width(dialogWidth).Padding(0, 1).
		Render("Your browser should open automatically. If it didn't, open the URL below:")

	// Truncate the URL to fit the dialog if needed.
	manualURL := m.session.ManualURL
	if lipgloss.Width(manualURL) > dialogWidth-2 {
		manualURL = manualURL[:dialogWidth-5] + "…"
	}
	urlBox := lipgloss.NewStyle().
		Foreground(t.Primary()).Background(bg).
		Width(dialogWidth).Padding(0, 1).
		Render(manualURL)

	separator := lipgloss.NewStyle().
		Foreground(t.TextMuted()).Background(bg).
		Width(dialogWidth).Padding(1, 1, 0, 1).
		Render("Or paste the authorization code shown on platform.claude.com:")

	inputRow := lipgloss.NewStyle().
		Background(bg).Width(dialogWidth).Padding(0, 1).
		Render(m.input.View())

	keys := lipgloss.NewStyle().
		Foreground(t.TextMuted()).Background(bg).
		Width(dialogWidth).Padding(1, 1, 0, 1).
		Render("enter  confirm   esc  cancel")

	parts := []string{title, hint, urlBox, separator, inputRow}

	if m.err != "" {
		errLine := lipgloss.NewStyle().
			Foreground(t.Error()).Background(bg).
			Width(dialogWidth).Padding(0, 1).
			Render("⚠ " + m.err)
		parts = append(parts, errLine)
	}

	parts = append(parts, keys)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return base.
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(bg).
		BorderForeground(t.Primary()).
		Background(bg).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

// Session returns the underlying auth session (used by tui.go to exchange the code).
func (m ClaudeLoginDialogCmp) Session() *auth.ClaudeLoginSession {
	return m.session
}

// SetSize propagates terminal size to the component.
func (m *ClaudeLoginDialogCmp) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Bindings implements layout.Bindings.
func (m ClaudeLoginDialogCmp) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}
