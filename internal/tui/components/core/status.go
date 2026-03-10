package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/lsp/protocol"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/components/chat"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

// StatusAction represents the type of action triggered by clicking a status bar element.
type StatusAction int

const (
	ActionShowHelp StatusAction = iota
	ActionShowModels
	ActionShowSession
	ActionShowDiagnostics
	ActionOpenFile
)

// StatusActionMsg is emitted when a clickable status bar element is clicked.
type StatusActionMsg struct {
	Action StatusAction
	Data   string // e.g., file path for ActionOpenFile
}

// BreadcrumbsUpdatedMsg is sent when the breadcrumb trail changes.
type BreadcrumbsUpdatedMsg struct {
	Files []string
}

const maxBreadcrumbs = 5
const maxBreadcrumbNameLen = 20

type StatusCmp interface {
	tea.Model
}

type statusCmp struct {
	info        util.InfoMsg
	width       int
	messageTTL  time.Duration
	lspClients  map[string]*lsp.Client
	session     session.Session
	breadcrumbs []string // recently edited file paths
}

// clearMessageCmd is a command that clears status messages after a timeout
func (m statusCmp) clearMessageCmd(ttl time.Duration) tea.Cmd {
	return tea.Tick(ttl, func(time.Time) tea.Msg {
		return util.ClearStatusMsg{}
	})
}

func (m statusCmp) Init() tea.Cmd {
	return nil
}

func (m statusCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case chat.SessionSelectedMsg:
		m.session = msg
	case chat.SessionClearedMsg:
		m.session = session.Session{}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent {
			if m.session.ID == msg.Payload.ID {
				m.session = msg.Payload
			}
		}
	case util.InfoMsg:
		m.info = msg
		ttl := msg.TTL
		if ttl == 0 {
			ttl = m.messageTTL
		}
		return m, m.clearMessageCmd(ttl)
	case util.ClearStatusMsg:
		m.info = util.InfoMsg{}
	case BreadcrumbsUpdatedMsg:
		m.breadcrumbs = msg.Files
		// Enforce max breadcrumbs
		if len(m.breadcrumbs) > maxBreadcrumbs {
			m.breadcrumbs = m.breadcrumbs[len(m.breadcrumbs)-maxBreadcrumbs:]
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			return m.handleMouseClick(msg)
		}
	}
	return m, nil
}

// handleMouseClick checks zone bounds for clickable status bar elements and
// returns an appropriate StatusActionMsg command.
func (m statusCmp) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if tuizone.InBounds(tuizone.StatusHelp, msg) {
		return m, util.CmdHandler(StatusActionMsg{Action: ActionShowHelp})
	}
	if tuizone.InBounds(tuizone.StatusDiagnostics, msg) {
		return m, util.CmdHandler(StatusActionMsg{Action: ActionShowDiagnostics})
	}
	// Check breadcrumb clicks
	for i, filePath := range m.breadcrumbs {
		if tuizone.InBounds(tuizone.StatusBreadcrumbID(i), msg) {
			return m, util.CmdHandler(StatusActionMsg{Action: ActionOpenFile, Data: filePath})
		}
	}
	return m, nil
}

var helpWidget = ""

// getHelpWidget returns the help widget with current theme colors
func getHelpWidget() string {
	t := theme.CurrentTheme()
	helpText := "ctrl+h help"

	return styles.Padded().
		Background(t.TextMuted()).
		Foreground(t.BackgroundDarker()).
		Bold(true).
		Render(helpText)
}

func formatTokensAndCost(tokens, contextWindow int64, cost float64) string {
	// Format tokens in human-readable format (e.g., 110K, 1.2M)
	var formattedTokens string
	switch {
	case tokens >= 1_000_000:
		formattedTokens = fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		formattedTokens = fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	default:
		formattedTokens = fmt.Sprintf("%d", tokens)
	}

	// Remove .0 suffix if present
	if strings.HasSuffix(formattedTokens, ".0K") {
		formattedTokens = strings.Replace(formattedTokens, ".0K", "K", 1)
	}
	if strings.HasSuffix(formattedTokens, ".0M") {
		formattedTokens = strings.Replace(formattedTokens, ".0M", "M", 1)
	}

	// Format cost with $ symbol and 2 decimal places
	formattedCost := fmt.Sprintf("$%.2f", cost)

	percentage := (float64(tokens) / float64(contextWindow)) * 100
	if percentage > 80 {
		// add the warning icon and percentage
		formattedTokens = fmt.Sprintf("%s(%d%%)", styles.WarningIcon, int(percentage))
	}

	return fmt.Sprintf("Context: %s, Cost: %s", formattedTokens, formattedCost)
}

// renderBreadcrumbs renders the breadcrumb trail of recently edited files.
func (m statusCmp) renderBreadcrumbs() string {
	if len(m.breadcrumbs) == 0 {
		return ""
	}

	t := theme.CurrentTheme()
	var parts []string
	separator := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Render(" > ")

	for i, filePath := range m.breadcrumbs {
		name := filepath.Base(filePath)
		// Truncate long names
		if len(name) > maxBreadcrumbNameLen {
			name = name[:maxBreadcrumbNameLen-3] + "..."
		}

		var styled string
		if i == len(m.breadcrumbs)-1 {
			// Active (last) file highlighted with Primary color
			styled = lipgloss.NewStyle().
				Foreground(t.Primary()).
				Bold(true).
				Render(name)
		} else {
			styled = lipgloss.NewStyle().
				Foreground(t.Text()).
				Render(name)
		}

		parts = append(parts, tuizone.MarkStatusBreadcrumb(i, styled))
	}

	breadcrumbContent := strings.Join(parts, separator)
	return lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		PaddingLeft(1).
		PaddingRight(1).
		Render(breadcrumbContent)
}

func (m statusCmp) View() string {
	t := theme.CurrentTheme()
	modelID := config.Get().Agents[config.AgentCoder].Model
	model := models.SupportedModels[modelID]

	// Initialize the help widget, wrapped in a clickable zone
	status := tuizone.MarkStatusHelp(getHelpWidget())

	// Render breadcrumbs between help widget and main status area
	breadcrumbs := m.renderBreadcrumbs()
	if breadcrumbs != "" {
		status += breadcrumbs
	}

	tokenInfoWidth := 0
	if m.session.ID != "" {
		totalTokens := m.session.PromptTokens + m.session.CompletionTokens
		tokens := formatTokensAndCost(totalTokens, model.ContextWindow, m.session.Cost)
		tokensStyle := styles.Padded().
			Background(t.Text()).
			Foreground(t.BackgroundSecondary())
		var percentage float64
		if model.ContextWindow > 0 {
			percentage = (float64(totalTokens) / float64(model.ContextWindow)) * 100
		}
		if percentage > 80 {
			tokensStyle = tokensStyle.Background(t.Warning())
		}
		tokenInfoWidth = lipgloss.Width(tokens) + 2
		status += tuizone.MarkStatusSession(tokensStyle.Render(tokens))
	}

	diagnosticsContent := m.projectDiagnostics()
	diagnostics := tuizone.MarkStatusDiagnostics(
		styles.Padded().
			Background(t.BackgroundDarker()).
			Render(diagnosticsContent),
	)

	breadcrumbWidth := lipgloss.Width(breadcrumbs)
	availableWidht := max(0, m.width-lipgloss.Width(helpWidget)-lipgloss.Width(m.model())-lipgloss.Width(diagnostics)-tokenInfoWidth-breadcrumbWidth)

	if m.info.Msg != "" {
		infoStyle := styles.Padded().
			Foreground(t.Background()).
			Width(availableWidht)

		switch m.info.Type {
		case util.InfoTypeInfo:
			infoStyle = infoStyle.Background(t.Info())
		case util.InfoTypeWarn:
			infoStyle = infoStyle.Background(t.Warning())
		case util.InfoTypeError:
			infoStyle = infoStyle.Background(t.Error())
		}

		infoWidth := availableWidht - 10
		// Truncate message if it's longer than available width
		msg := m.info.Msg
		if len(msg) > infoWidth && infoWidth > 0 {
			msg = msg[:infoWidth] + "..."
		}
		status += infoStyle.Render(msg)
	} else {
		status += styles.Padded().
			Foreground(t.Text()).
			Background(t.BackgroundSecondary()).
			Width(availableWidht).
			Render("")
	}

	status += diagnostics
	status += tuizone.MarkStatusModel(m.model())
	return status
}

func (m *statusCmp) projectDiagnostics() string {
	t := theme.CurrentTheme()

	// Check if any LSP server is still initializing
	initializing := false
	for _, client := range m.lspClients {
		if client.GetServerState() == lsp.StateStarting {
			initializing = true
			break
		}
	}

	// If any server is initializing, show that status
	if initializing {
		return lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Warning()).
			Render(fmt.Sprintf("%s Initializing LSP...", styles.SpinnerIcon))
	}

	errorDiagnostics := []protocol.Diagnostic{}
	warnDiagnostics := []protocol.Diagnostic{}
	hintDiagnostics := []protocol.Diagnostic{}
	infoDiagnostics := []protocol.Diagnostic{}
	for _, client := range m.lspClients {
		for _, d := range client.GetDiagnostics() {
			for _, diag := range d {
				switch diag.Severity {
				case protocol.SeverityError:
					errorDiagnostics = append(errorDiagnostics, diag)
				case protocol.SeverityWarning:
					warnDiagnostics = append(warnDiagnostics, diag)
				case protocol.SeverityHint:
					hintDiagnostics = append(hintDiagnostics, diag)
				case protocol.SeverityInformation:
					infoDiagnostics = append(infoDiagnostics, diag)
				}
			}
		}
	}

	if len(errorDiagnostics) == 0 && len(warnDiagnostics) == 0 && len(hintDiagnostics) == 0 && len(infoDiagnostics) == 0 {
		return "No diagnostics"
	}

	diagnostics := []string{}

	if len(errorDiagnostics) > 0 {
		errStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Error()).
			Render(fmt.Sprintf("%s %d", styles.ErrorIcon, len(errorDiagnostics)))
		diagnostics = append(diagnostics, errStr)
	}
	if len(warnDiagnostics) > 0 {
		warnStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Warning()).
			Render(fmt.Sprintf("%s %d", styles.WarningIcon, len(warnDiagnostics)))
		diagnostics = append(diagnostics, warnStr)
	}
	if len(hintDiagnostics) > 0 {
		hintStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Text()).
			Render(fmt.Sprintf("%s %d", styles.HintIcon, len(hintDiagnostics)))
		diagnostics = append(diagnostics, hintStr)
	}
	if len(infoDiagnostics) > 0 {
		infoStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Info()).
			Render(fmt.Sprintf("%s %d", styles.InfoIcon, len(infoDiagnostics)))
		diagnostics = append(diagnostics, infoStr)
	}

	return strings.Join(diagnostics, " ")
}

func (m statusCmp) availableFooterMsgWidth(diagnostics, tokenInfo string) int {
	tokensWidth := 0
	if m.session.ID != "" {
		tokensWidth = lipgloss.Width(tokenInfo) + 2
	}
	return max(0, m.width-lipgloss.Width(helpWidget)-lipgloss.Width(m.model())-lipgloss.Width(diagnostics)-tokensWidth)
}

func (m statusCmp) model() string {
	t := theme.CurrentTheme()

	cfg := config.Get()

	coder, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return styles.Padded().
			Background(t.Secondary()).
			Foreground(t.Background()).
			Render("No model")
	}
	model, modelOK := models.SupportedModels[coder.Model]
	modelName := model.Name
	if !modelOK || modelName == "" {
		modelName = "No model"
	}

	return styles.Padded().
		Background(t.Secondary()).
		Foreground(t.Background()).
		Render(modelName)
}

func NewStatusCmp(lspClients map[string]*lsp.Client) StatusCmp {
	helpWidget = getHelpWidget()

	return &statusCmp{
		messageTTL:  10 * time.Second,
		lspClients:  lspClients,
		breadcrumbs: []string{},
	}
}
