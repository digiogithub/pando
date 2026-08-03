package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/llm/agent"
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

// MCPGatewayMsg carries the current number of MCP gateway favorite tools.
// Send this message whenever the favorites count changes (e.g., after
// gateway initialization or after a tool invocation updates the stats).
type MCPGatewayMsg struct {
	FavoritesCount int
}

// ProjectActiveMsg is sent when the active project changes.
// Name is the display name (filepath.Base(path) or custom name).
// An empty Name means no project is active.
type ProjectActiveMsg struct {
	Name string
}

const maxBreadcrumbs = 5
const maxBreadcrumbNameLen = 20

type StatusCmp interface {
	tea.Model
}

// TokenUsageMsg carries a live context-window token update for the status bar.
// When Estimated is true the value is provisional (e.g. estimated while tools run)
// and is rendered with a "~" prefix and a dimmed style until confirmed.
type TokenUsageMsg struct {
	SessionID           string
	PromptTokens        int64
	CompletionTokens    int64
	ContextWindow       int64
	Estimated           bool
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	Cost                float64
}

// AutoApproveMsg toggles the "auto mode" indicator in the status bar for a session.
// When Enabled is true the agent auto-approves tool permission requests for SessionID.
type AutoApproveMsg struct {
	SessionID string
	Enabled   bool
}

type statusCmp struct {
	info              util.InfoMsg
	width             int
	messageTTL        time.Duration
	lspClients        map[string]*lsp.Client
	session           session.Session
	breadcrumbs       []string // recently edited file paths
	mcpFavoritesCount int      // number of MCP gateway favorite tools (0 = gateway off)
	activeProject     string   // display name of the active project ("" = none)

	// Live token estimate shown while the agent loop runs. estimatedActive is true
	// while a provisional value should be displayed; it is cleared when a confirmed
	// session update arrives. estimatedContextWindow overrides the model window when > 0.
	estimatedTokens        int64
	estimatedContextWindow int64
	estimatedActive        bool

	// autoApprove reflects whether the current session auto-approves tool
	// permission requests ("auto mode"). Rendered as a chip when true.
	autoApprove bool
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
		m.estimatedActive = false
	case TokenUsageMsg:
		// Apply only to the current session; ignore stale/other-session updates.
		if m.session.ID == "" || msg.SessionID == m.session.ID {
			if msg.Estimated {
				m.estimatedTokens = msg.PromptTokens + msg.CompletionTokens
				m.estimatedContextWindow = msg.ContextWindow
				m.estimatedActive = true
			} else {
				// Confirmed usage reconciles any provisional estimate.
				m.estimatedActive = false
			}
		}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent {
			if m.session.ID == msg.Payload.ID {
				m.session = msg.Payload
				// A persisted session update carries confirmed token totals.
				m.estimatedActive = false
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
	case AutoApproveMsg:
		if m.session.ID == "" || msg.SessionID == "" || msg.SessionID == m.session.ID {
			m.autoApprove = msg.Enabled
		}
	case MCPGatewayMsg:
		m.mcpFavoritesCount = msg.FavoritesCount
	case ProjectActiveMsg:
		m.activeProject = msg.Name
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
		Foreground(t.BadgeText()).
		Bold(true).
		Render(helpText)
}

// formatTokens renders the context-window label. When estimated is true the value
// is provisional (e.g. estimated while tools run) and is prefixed with "~".
func formatTokens(tokens, contextWindow int64, estimated bool) string {
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

	if contextWindow > 0 {
		percentage := (float64(tokens) / float64(contextWindow)) * 100
		if percentage > 80 {
			formattedTokens = fmt.Sprintf("%s %s (%d%%)", styles.WarningIcon, formattedTokens, int(percentage))
		}
	}

	if estimated {
		return fmt.Sprintf("Context: ~%s", formattedTokens)
	}
	return fmt.Sprintf("Context: %s", formattedTokens)
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

// sessionModel returns the model the selected session actually runs on, which is
// not necessarily the configured one: pando_setup can switch the model for a
// single session at runtime.
func (m statusCmp) sessionModel() models.Model {
	return models.SupportedModels()[agent.SessionModelID(m.session.ID)]
}

func (m statusCmp) View() string {
	t := theme.CurrentTheme()
	model := m.sessionModel()

	// Initialize the help widget, wrapped in a clickable zone
	status := tuizone.MarkStatusHelp(getHelpWidget())

	// Render breadcrumbs between help widget and main status area
	breadcrumbs := m.renderBreadcrumbs()
	if breadcrumbs != "" {
		status += breadcrumbs
	}

	// Render active project badge (if any).
	if m.activeProject != "" {
		projectBadge := styles.Padded().
			Background(t.Primary()).
			Foreground(t.BadgeText()).
			Render("⬡ " + m.activeProject)
		status += projectBadge
	}

	tokenInfoWidth := 0
	if m.session.ID != "" {
		totalTokens := m.session.PromptTokens + m.session.CompletionTokens
		contextWindow := model.ContextWindow
		estimated := false
		// Prefer the live estimate while the agent loop is running, so the counter
		// grows as tools execute / files are produced before the next confirmed usage.
		if m.estimatedActive && m.estimatedTokens > totalTokens {
			totalTokens = m.estimatedTokens
			if m.estimatedContextWindow > 0 {
				contextWindow = m.estimatedContextWindow
			}
			estimated = true
		}
		tokens := formatTokens(totalTokens, contextWindow, estimated)
		tokensStyle := styles.Padded().
			Background(t.Text()).
			Foreground(t.BadgeText())
		var percentage float64
		if contextWindow > 0 {
			percentage = (float64(totalTokens) / float64(contextWindow)) * 100
		}
		if percentage > 80 {
			tokensStyle = tokensStyle.Background(t.Warning())
		} else if estimated {
			// Dimmed treatment while the value is provisional.
			tokensStyle = tokensStyle.Background(t.TextMuted())
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

	// Auto-approve ("auto mode") indicator chip.
	autoApproveBadge := ""
	if m.autoApprove {
		autoApproveBadge = styles.Padded().
			Background(t.Warning()).
			Foreground(t.BadgeText()).
			Bold(true).
			Render("⏵⏵ auto-accept")
	}

	breadcrumbWidth := lipgloss.Width(breadcrumbs)
	availableWidht := max(0, m.width-lipgloss.Width(helpWidget)-lipgloss.Width(m.model())-lipgloss.Width(diagnostics)-tokenInfoWidth-breadcrumbWidth-lipgloss.Width(autoApproveBadge))

	if m.info.Msg != "" {
		infoStyle := styles.Padded().
			Foreground(t.BadgeText()).
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
	if autoApproveBadge != "" {
		status += autoApproveBadge
	}
	if m.mcpFavoritesCount > 0 {
		t := theme.CurrentTheme()
		mcpBadge := styles.Padded().
			Background(t.BackgroundDarker()).
			Foreground(t.Primary()).
			Render(fmt.Sprintf("⚡%d", m.mcpFavoritesCount))
		status += mcpBadge
	}
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

	modelName := m.sessionModel().Name
	if modelName == "" {
		modelName = "No model"
	}

	return styles.Padded().
		Background(t.Secondary()).
		Foreground(t.BadgeText()).
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
