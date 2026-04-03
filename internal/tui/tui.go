package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/auth"
	"go.dalton.dog/bubbleup"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp/protocol"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/components/chat"
	"github.com/digiogithub/pando/internal/tui/components/core"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
	"github.com/digiogithub/pando/internal/tui/components/editor"
	"github.com/digiogithub/pando/internal/tui/components/filetree"
	"github.com/digiogithub/pando/internal/tui/components/terminal"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/page"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

type startCompactSessionMsg struct{}

// terminalFocusChangedMsg updates terminal focus on the live app model copy
// stored by Bubble Tea. The terminal panel itself is mutated through a shared
// pointer, but appModel.terminalFocused must be updated via a message because
// appModel.Update uses a value receiver.
type terminalFocusChangedMsg struct {
	focused bool
}

type appModel struct {
	width, height   int
	currentPage     page.PageID
	previousPage    page.PageID
	pages           map[page.PageID]tea.Model
	loadedPages     map[page.PageID]bool
	keys            KeyMap
	status          core.StatusCmp
	app             *app.App
	selectedSession session.Session
	chatPage        *page.ChatPageModel
	fileTree        filetree.Component
	viewer          editor.FileViewerComponent
	tabBar          *editor.TabBar
	layoutMode      page.ChatLayoutMode

	showPermissions bool
	permissions     dialog.PermissionDialogCmp

	showHelp bool
	help     dialog.HelpCmp

	showQuit bool
	quit     dialog.QuitDialog

	showSessionDialog bool
	sessionDialog     dialog.SessionDialog

	showCommandDialog bool
	commandDialog     dialog.CommandDialog
	commands          []dialog.Command

	showModelDialog bool
	modelDialog     dialog.ModelDialog

	showInitDialog bool
	initDialog     dialog.InitDialogCmp

	showFilepicker bool
	filepicker     dialog.FilepickerCmp
	filepickerMode string // "attach" or "edit"

	showThemeDialog bool
	themeDialog     dialog.ThemeDialog

	showMultiArgumentsDialog bool
	multiArgumentsDialog     dialog.MultiArgumentsDialogCmp

	showClaudeLoginDialog bool
	claudeLoginDialog     dialog.ClaudeLoginDialogCmp
	claudeLoginSession    *auth.ClaudeLoginSession

	showInfoDialog bool
	infoDialog     dialog.InfoDialogCmp

	isCompacting      bool
	compactingMessage string

	terminalPanel   *terminal.TerminalPanel
	terminalFocused bool

	alert bubbleup.AlertModel
}

func (a appModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmd := a.pages[a.currentPage].Init()
	a.loadedPages[a.currentPage] = true
	cmds = append(cmds, cmd)
	cmd = a.status.Init()
	cmds = append(cmds, cmd)
	cmd = a.alert.Init()
	cmds = append(cmds, cmd)
	cmd = a.quit.Init()
	cmds = append(cmds, cmd)
	cmd = a.help.Init()
	cmds = append(cmds, cmd)
	cmd = a.sessionDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.commandDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.modelDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.initDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.filepicker.Init()
	cmds = append(cmds, cmd)
	cmd = a.themeDialog.Init()
	cmds = append(cmds, cmd)

	// Check if we should show the init dialog
	cmds = append(cmds, func() tea.Msg {
		shouldShow, err := config.ShouldShowInitDialog()
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "Failed to check init status: " + err.Error(),
			}
		}
		return dialog.ShowInitDialogMsg{Show: shouldShow}
	})

	// Show model selector if the configured model is not yet available
	cmds = append(cmds, func() tea.Msg {
		cfg := config.Get()
		if cfg == nil {
			return nil
		}
		agentCfg, ok := cfg.Agents[config.AgentCoder]
		if !ok {
			return dialog.OpenModelDialogMsg{}
		}
		if _, modelOK := models.SupportedModels[agentCfg.Model]; !modelOK {
			return dialog.OpenModelDialogMsg{}
		}
		return nil
	})
	cmds = append(cmds, tea.EnableMouseCellMotion)

	// Fetch MCP gateway favorites count if the gateway is active.
	if a.app != nil && a.app.MCPGateway != nil {
		gw := a.app.MCPGateway
		cmds = append(cmds, func() tea.Msg {
			favorites, err := gw.GetFavorites(context.Background())
			if err != nil {
				return nil
			}
			return core.MCPGatewayMsg{FavoritesCount: len(favorites)}
		})
	}

	return tea.Batch(cmds...)
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if cmd, handled := a.handleMouse(msg); handled {
			return a, cmd
		}
	case tea.WindowSizeMsg:
		msg.Height -= 1 // Make space for the status bar
		a.width, a.height = msg.Width, msg.Height

		// Resize terminal panel and adjust available height for main content.
		terminalPanelCmd := a.terminalPanel.SetSize(a.width, a.height)
		cmds = append(cmds, terminalPanelCmd)

		// Reduce available height for main content when terminal panel is visible.
		mainMsg := msg
		mainMsg.Height -= a.terminalPanel.PanelHeight()

		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(mainMsg)
		cmds = append(cmds, cmd)

		prm, permCmd := a.permissions.Update(msg)
		a.permissions = prm.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permCmd)

		help, helpCmd := a.help.Update(msg)
		a.help = help.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)

		session, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = session.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)

		command, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = command.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)

		filepicker, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = filepicker.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		a.initDialog.SetSize(msg.Width, msg.Height)

		if a.showMultiArgumentsDialog {
			a.multiArgumentsDialog.SetSize(msg.Width, msg.Height)
			args, argsCmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			cmds = append(cmds, argsCmd, a.multiArgumentsDialog.Init())
		}

		if a.showClaudeLoginDialog {
			a.claudeLoginDialog.SetSize(msg.Width, msg.Height)
		}

		return a, tea.Batch(cmds...)
	// Status
	case util.InfoMsg:
		// Error messages go to bubbleup alert overlay instead of status bar
		if msg.Type == util.InfoTypeError {
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.ErrorKey, msg.Msg))
			outAlert, outCmd := a.alert.Update(msg)
			a.alert = outAlert.(bubbleup.AlertModel)
			cmds = append(cmds, outCmd)
			return a, tea.Batch(cmds...)
		}
		s, cmd := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case util.AlertMsg:
		if msg.Persist {
			// Persistent alerts last 10 minutes and are dismissed with Esc
			longAlert := bubbleup.NewAlertModel(60, false, 10*time.Minute)
			cmds = append(cmds, longAlert.NewAlertCmd(msg.Type, msg.Msg))
		} else {
			cmds = append(cmds, a.alert.NewAlertCmd(msg.Type, msg.Msg))
		}
		outAlert, outCmd := a.alert.Update(msg)
		a.alert = outAlert.(bubbleup.AlertModel)
		cmds = append(cmds, outCmd)
		return a, tea.Batch(cmds...)

	// Copilot device code received - show alert and start polling
	case copilotDeviceCodeMsg:
		// Use a long-duration alert (10 min) so the user has time to copy
		// the code and complete the flow. The alert is dismissed on Esc or
		// when the login completes.
		longAlert := bubbleup.NewAlertModel(60, false, 10*time.Minute)
		cmds = append(cmds, longAlert.NewAlertCmd(bubbleup.WarnKey, msg.instructions))
		outAlert, outCmd := a.alert.Update(msg)
		a.alert = outAlert.(bubbleup.AlertModel)
		cmds = append(cmds, outCmd)
		cmds = append(cmds, copilotPollCommand(msg.deviceCode))
		return a, tea.Batch(cmds...)

	// Copilot login flow completed - show result (replaces device code alert)
	case copilotLoginDoneMsg:
		if msg.err != nil {
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.ErrorKey, fmt.Sprintf("Copilot login failed: %v", msg.err)))
		} else {
			status := auth.GetCopilotAuthStatus()
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.InfoKey, status.Message))
		}
		outAlert, outCmd := a.alert.Update(msg)
		a.alert = outAlert.(bubbleup.AlertModel)
		cmds = append(cmds, outCmd)
		return a, tea.Batch(cmds...)

	// Claude OAuth login — phase 1: session ready, show dialog and open browser.
	case dialog.ClaudeLoginStartMsg:
		a.claudeLoginSession = msg.Session
		a.claudeLoginDialog = dialog.NewClaudeLoginDialogCmp(msg.Session)
		a.showClaudeLoginDialog = true
		// Open the browser with the automatic URL.
		_ = auth.OpenBrowser(msg.Session.AutoURL)
		// Start waiting for the automatic browser callback in background.
		return a, tea.Batch(a.claudeLoginDialog.Init(), dialog.ClaudeWaitAutoCodeCmd(msg.Session))

	// Claude OAuth login — automatic browser callback delivered the code.
	case auth.ClaudeAutoCode:
		if !a.showClaudeLoginDialog {
			return a, nil
		}
		a.showClaudeLoginDialog = false
		a.claudeLoginSession = nil
		if msg.Err != nil {
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.ErrorKey, "Claude login failed: "+msg.Err.Error()))
			outAlert, outCmd := a.alert.Update(msg)
			a.alert = outAlert.(bubbleup.AlertModel)
			cmds = append(cmds, outCmd)
			return a, tea.Batch(cmds...)
		}
		// Exchange the code for tokens.
		session := a.claudeLoginDialog.Session()
		return a, dialog.ClaudeExchangeCodeCmd(session, msg.Code, msg.RedirectURI)

	// Claude OAuth login — user submitted a manual code via the dialog input.
	case dialog.ClaudeLoginCodeSubmitMsg:
		a.showClaudeLoginDialog = false
		session := a.claudeLoginSession
		a.claudeLoginSession = nil
		return a, dialog.ClaudeExchangeCodeCmd(session, msg.Code, msg.RedirectURI)

	// Claude OAuth login — user cancelled the dialog.
	case dialog.ClaudeLoginDialogCancelMsg:
		if a.claudeLoginSession != nil {
			a.claudeLoginSession.Cancel()
			a.claudeLoginSession = nil
		}
		a.showClaudeLoginDialog = false
		return a, nil

	// Claude account messages
	case dialog.ClaudeStatsMsg:
		a.infoDialog.SetContent("Usage Statistics", msg.Content)
		a.showInfoDialog = true
		return a, nil

	case dialog.ShowInfoDialogMsg:
		a.infoDialog.SetContent(msg.Title, msg.Content)
		a.showInfoDialog = true
		return a, nil

	case dialog.CloseInfoDialogMsg:
		a.showInfoDialog = false
		return a, nil

	case dialog.ClaudeLoginDoneMsg:
		if msg.Err != nil {
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.ErrorKey, fmt.Sprintf("Claude login failed: %v", msg.Err)))
		} else {
			name := msg.DisplayName
			if name == "" {
				name = "Claude.ai"
			}
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.InfoKey, "Logged in as "+name))
		}
		outAlert, outCmd := a.alert.Update(msg)
		a.alert = outAlert.(bubbleup.AlertModel)
		cmds = append(cmds, outCmd)
		return a, tea.Batch(cmds...)

	case dialog.ClaudeLogoutDoneMsg:
		if msg.Err != nil {
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.ErrorKey, fmt.Sprintf("Claude logout failed: %v", msg.Err)))
		} else {
			cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.InfoKey, "Logged out from Claude.ai"))
		}
		outAlert, outCmd := a.alert.Update(msg)
		a.alert = outAlert.(bubbleup.AlertModel)
		cmds = append(cmds, outCmd)
		return a, tea.Batch(cmds...)
	case pubsub.Event[logging.LogMessage]:
		if msg.Payload.Persist {
			switch msg.Payload.Level {
			case "error":
				// Show errors as bubbleup overlay alerts
				cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.ErrorKey, msg.Payload.Message))
				outAlert, outCmd := a.alert.Update(msg)
				a.alert = outAlert.(bubbleup.AlertModel)
				cmds = append(cmds, outCmd)
			case "info":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)

			case "warn":
				// Show warnings as bubbleup overlay alerts (top-right toast style)
				cmds = append(cmds, a.alert.NewAlertCmd(bubbleup.WarnKey, msg.Payload.Message))
				outAlert, outCmd := a.alert.Update(msg)
				a.alert = outAlert.(bubbleup.AlertModel)
				cmds = append(cmds, outCmd)
			default:
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			}
		}
	case util.ClearStatusMsg:
		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)

	// Permission
	case pubsub.Event[permission.PermissionRequest]:
		a.showPermissions = true
		return a, a.permissions.SetPermissions(msg.Payload)
	case dialog.PermissionResponseMsg:
		var cmd tea.Cmd
		switch msg.Action {
		case dialog.PermissionAllow:
			a.app.Permissions.Grant(msg.Permission)
		case dialog.PermissionAllowForSession:
			a.app.Permissions.GrantPersistant(msg.Permission)
		case dialog.PermissionDeny:
			a.app.Permissions.Deny(msg.Permission)
		}
		a.showPermissions = false
		return a, cmd

	case page.PageChangeMsg:
		return a, a.moveToPage(msg.ID)

	case dialog.CloseQuitMsg:
		a.showQuit = false
		return a, nil

	case dialog.CloseSessionDialogMsg:
		a.showSessionDialog = false
		return a, nil

	case dialog.CloseCommandDialogMsg:
		a.showCommandDialog = false
		return a, nil

	case terminalFocusChangedMsg:
		logging.Info("tui: terminal focus changed", "focused", msg.focused)
		a.terminalFocused = msg.focused
		if msg.focused {
			a.terminalPanel.Focus()
		} else {
			a.terminalPanel.Blur()
		}
		return a, nil

	case startCompactSessionMsg:
		// Start compacting the current session
		a.isCompacting = true
		a.compactingMessage = "Starting summarization..."

		if a.selectedSession.ID == "" {
			a.isCompacting = false
			return a, util.ReportWarn("No active session to summarize")
		}

		// Start the summarization process
		return a, func() tea.Msg {
			ctx := context.Background()
			a.app.CoderAgent.Summarize(ctx, a.selectedSession.ID)
			return nil
		}

	case pubsub.Event[agent.AgentEvent]:
		payload := msg.Payload
		if payload.Error != nil {
			a.isCompacting = false
			return a, util.ReportError(payload.Error)
		}

		a.compactingMessage = payload.Progress

		if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
			a.isCompacting = false
			return a, util.ReportInfo("Session summarization complete")
		} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.selectedSession.ID != "" {
			model := a.app.CoderAgent.Model()
			contextWindow := model.ContextWindow
			tokens := a.selectedSession.CompletionTokens + a.selectedSession.PromptTokens
			if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
				return a, util.CmdHandler(startCompactSessionMsg{})
			}
		}
		// Continue listening for events
		return a, nil

	case dialog.CloseThemeDialogMsg:
		a.showThemeDialog = false
		return a, nil

	case dialog.ThemeChangedMsg:
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		a.showThemeDialog = false
		return a, tea.Batch(cmd, util.ReportInfo("Theme changed to: "+msg.ThemeName))

	case dialog.CloseModelDialogMsg:
		a.showModelDialog = false
		return a, nil

	case dialog.ModelSelectedMsg:
		a.showModelDialog = false

		model, err := a.app.CoderAgent.Update(config.AgentCoder, msg.Model.ID)
		if err != nil {
			return a, util.ReportError(err)
		}

		return a, util.ReportInfo(fmt.Sprintf("Model changed to %s", model.Name))

	case dialog.ShowInitDialogMsg:
		a.showInitDialog = msg.Show
		return a, nil

	case dialog.CloseInitDialogMsg:
		a.showInitDialog = false
		if msg.Initialize {
			// Run the initialization command
			for _, cmd := range a.commands {
				if cmd.ID == "init" {
					// Mark the project as initialized
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, cmd.Handler(cmd)
				}
			}
		} else {
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return a, util.ReportError(err)
			}
		}
		return a, nil

	case chat.SessionSelectedMsg:
		a.selectedSession = msg
		a.sessionDialog.SetSelectedSession(msg.ID)

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.selectedSession.ID {
			a.selectedSession = msg.Payload
		}
	case dialog.SessionSelectedMsg:
		a.showSessionDialog = false
		if a.currentPage == page.ChatPage {
			return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))
		}
		return a, nil

	case dialog.CommandSelectedMsg:
		a.showCommandDialog = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return a, msg.Command.Handler(msg.Command)
		}
		return a, util.ReportInfo("Command selected: " + msg.Command.Title)

	case dialog.OpenSessionDialogMsg:
		if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showCommandDialog {
			return a, a.openSessionDialog()
		}
		return a, nil

	case dialog.OpenModelDialogMsg:
		if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
			return a, a.openModelDialog()
		}
		return a, nil

	case dialog.OpenThemeDialogMsg:
		if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
			return a, a.openThemeDialog()
		}
		return a, nil

	case dialog.OpenFilepickerMsg:
		if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
			a.filepickerMode = "attach"
			return a, a.openFilepicker()
		}
		return a, nil

	case dialog.AttachmentAddedMsg:
		// Intercept when filepicker is in edit mode: open file inline instead of attaching
		if a.filepickerMode == "edit" {
			a.filepickerMode = ""
			a.showFilepicker = false
			a.filepicker.ToggleFilepicker(false)
			return a, util.CmdHandler(editor.OpenEditableFileMsg{Path: msg.Attachment.FileName})
		}
		// Fall through to normal attachment handling below

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.showMultiArgumentsDialog = true
		return a, a.multiArgumentsDialog.Init()

	case dialog.CloseMultiArgumentsDialogMsg:
		// Close multi-arguments dialog
		a.showMultiArgumentsDialog = false

		// If submitted, replace all named arguments and run the command
		if msg.Submit {
			content := msg.Content

			// Replace each named argument with its value
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}

			// Execute the command with arguments
			return a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			})
		}
		return a, nil

	case tea.KeyMsg:
		// If a bubbleup alert is active, let it handle Esc to dismiss
		if msg.String() == "esc" && a.alert.HasActiveAlert() {
			outAlert, outCmd := a.alert.Update(msg)
			a.alert = outAlert.(bubbleup.AlertModel)
			return a, outCmd
		}

		// If Claude login dialog is open, let it handle all key presses.
		if a.showClaudeLoginDialog {
			d, cmd := a.claudeLoginDialog.Update(msg)
			a.claudeLoginDialog = d.(dialog.ClaudeLoginDialogCmp)
			return a, cmd
		}

		// If multi-arguments dialog is open, let it handle the key press first
		if a.showMultiArgumentsDialog {
			args, cmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			return a, cmd
		}

		// Ctrl+Alt+Y: show/hide terminal panel.
		if key.Matches(msg, a.keys.Global.ToggleTerminal) {
			return a, a.toggleTerminalPanel()
		}

		// Ctrl+Y: open a new terminal tab (panel becomes visible and focused).
		if key.Matches(msg, a.keys.Global.NewTerminal) {
			return a, a.openNewTerminalTab()
		}

		// Ctrl+Shift+Y: cycle to next terminal tab.
		if key.Matches(msg, a.keys.Global.NextTerminal) {
			return a, a.nextTerminalTab()
		}

		// When terminal is focused, route all key input to the terminal panel.
		if a.terminalFocused && a.terminalPanel.IsVisible() {
			newPanel, panelCmd := a.terminalPanel.Update(msg)
			a.terminalPanel = newPanel
			// Auto-close panel if all terminals closed.
			if !a.terminalPanel.HasTerminals() {
				a.terminalFocused = false
				a.terminalPanel.Blur()
			}
			return a, panelCmd
		}

		switch {

		case key.Matches(msg, a.keys.Global.Quit) && !(a.tabBar != nil && a.tabBar.IsActiveEditable()):
			a.showQuit = !a.showQuit
			if a.showHelp {
				a.showHelp = false
			}
			if a.showSessionDialog {
				a.showSessionDialog = false
			}
			if a.showCommandDialog {
				a.showCommandDialog = false
			}
			if a.showFilepicker {
				a.showFilepicker = false
				a.filepicker.ToggleFilepicker(a.showFilepicker)
			}
			if a.showModelDialog {
				a.showModelDialog = false
			}
			if a.showMultiArgumentsDialog {
				a.showMultiArgumentsDialog = false
			}
			return a, nil
		case key.Matches(msg, a.keys.Editor.EditExternal):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions {
				if a.tabBar != nil && a.tabBar.ActivePath() != "" {
					// Open current file in inline editable editor
					return a, util.CmdHandler(editor.OpenEditableFileMsg{Path: a.tabBar.ActivePath()})
				}
				// No file open: open filepicker in edit mode
				a.filepickerMode = "edit"
				a.showFilepicker = true
				a.filepicker.ToggleFilepicker(a.showFilepicker)
				return a, nil
			}
		case key.Matches(msg, a.keys.Chat.SwitchSession) && a.currentPage == page.ChatPage && !(a.tabBar != nil && a.tabBar.IsActiveEditable()):
			if !a.showQuit && !a.showPermissions && !a.showCommandDialog {
				return a, a.openSessionDialog()
			}
			return a, nil
		case key.Matches(msg, a.keys.Chat.Commands):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker {
				return a, a.openCommandDialog()
			}
			return a, nil
		case key.Matches(msg, a.keys.Chat.Models):
			if a.showModelDialog {
				a.showModelDialog = false
				return a, nil
			}
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				return a, a.openModelDialog()
			}
			return a, nil
		case key.Matches(msg, a.keys.Global.SwitchTheme):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				return a, a.openThemeDialog()
			}
			return a, nil
		case key.Matches(msg, a.keys.Global.Settings):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				return a, a.moveToPage(page.SettingsPage)
			}
			return a, nil
		case key.Matches(msg, a.keys.Global.Orchestrator):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				return a, a.moveToPage(page.OrchestratorPage)
			}
			return a, nil
		case key.Matches(msg, returnKey) || key.Matches(msg):
			if msg.String() == quitKey {
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			} else if !a.filepicker.IsCWDFocused() {
				if a.showQuit {
					a.showQuit = !a.showQuit
					return a, nil
				}
				if a.showHelp {
					a.showHelp = !a.showHelp
					return a, nil
				}
				if a.showInitDialog {
					a.showInitDialog = false
					// Mark the project as initialized without running the command
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, nil
				}
				if a.showFilepicker {
					a.showFilepicker = false
					a.filepicker.ToggleFilepicker(a.showFilepicker)
					return a, nil
				}
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
				if a.currentPage == page.SettingsPage {
					// If the settings page has an active modal (e.g. skills catalog dialog),
					// forward Esc to it so the dialog can close instead of navigating away.
					if mp, ok := a.pages[a.currentPage].(page.ModalPage); ok && mp.HasActiveModal() {
						a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
						return a, cmd
					}
					return a, a.moveToPage(page.ChatPage)
				}
				if a.currentPage == page.OrchestratorPage {
					return a, a.moveToPage(page.ChatPage)
				}
				if a.currentPage == page.SnapshotsPage {
					return a, a.moveToPage(page.ChatPage)
				}
				if a.currentPage == page.EvaluatorPage {
					return a, a.moveToPage(page.ChatPage)
				}
			}
		case key.Matches(msg, a.keys.Global.Logs):
			return a, a.moveToPage(page.LogsPage)
		case key.Matches(msg, a.keys.Global.Snapshots):
			return a, a.moveToPage(page.SnapshotsPage)
		case key.Matches(msg, a.keys.Global.Evaluator):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				return a, a.moveToPage(page.EvaluatorPage)
			}
			return a, nil
		case key.Matches(msg, a.keys.Global.Help):
			if a.showQuit {
				return a, nil
			}
			a.showHelp = !a.showHelp
			return a, nil
		case key.Matches(msg, helpEsc):
			if a.app.CoderAgent.IsBusy() {
				if a.showQuit {
					return a, nil
				}
				a.showHelp = !a.showHelp
				return a, nil
			}
		case key.Matches(msg, a.keys.Global.Filepicker):
			if a.showFilepicker {
				a.showFilepicker = false
				a.filepicker.ToggleFilepicker(a.showFilepicker)
				return a, nil
			}
			return a, a.openFilepicker()
		}
	default:
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		// Forward all non-key messages to the terminal panel (e.g. tick msgs).
		if a.terminalPanel.IsVisible() {
			logging.Debug("tui.Update default: forwarding to terminal panel",
				"msg_type", fmt.Sprintf("%T", msg),
				"terminalFocused", a.terminalFocused,
				"has_terminals", a.terminalPanel.HasTerminals())
			newPanel, panelCmd := a.terminalPanel.Update(msg)
			a.terminalPanel = newPanel
			cmds = append(cmds, panelCmd)
			if !a.terminalPanel.HasTerminals() {
				a.terminalFocused = false
				a.terminalPanel.Blur()
			}
		}

	}

	if a.showFilepicker {
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showQuit {
		q, quitCmd := a.quit.Update(msg)
		a.quit = q.(dialog.QuitDialog)
		cmds = append(cmds, quitCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}
	if a.showPermissions {
		d, permissionsCmd := a.permissions.Update(msg)
		a.permissions = d.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permissionsCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSessionDialog {
		d, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = d.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showCommandDialog {
		d, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = d.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showModelDialog {
		d, modelCmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		cmds = append(cmds, modelCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showInitDialog {
		d, initCmd := a.initDialog.Update(msg)
		a.initDialog = d.(dialog.InitDialogCmp)
		cmds = append(cmds, initCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showThemeDialog {
		d, themeCmd := a.themeDialog.Update(msg)
		a.themeDialog = d.(dialog.ThemeDialog)
		cmds = append(cmds, themeCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showInfoDialog {
		d, infoCmd := a.infoDialog.Update(msg)
		a.infoDialog = d.(dialog.InfoDialogCmp)
		cmds = append(cmds, infoCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showHelp {
		h, helpCmd := a.help.Update(msg)
		a.help = h.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	s, _ := a.status.Update(msg)
	a.status = s.(core.StatusCmp)
	a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
	a.syncChatState()
	cmds = append(cmds, cmd)

	// Update bubbleup alert model (handles ticks, dismiss, etc.)
	outAlert, alertCmd := a.alert.Update(msg)
	a.alert = outAlert.(bubbleup.AlertModel)
	cmds = append(cmds, alertCmd)

	return a, tea.Batch(cmds...)
}

// RegisterCommand adds a command to the command dialog
func (a *appModel) RegisterCommand(cmd dialog.Command) {
	a.commands = append(a.commands, cmd)
}

func (a *appModel) openCommandDialog() tea.Cmd {
	if len(a.commands) == 0 {
		return util.ReportWarn("No commands available")
	}

	a.commandDialog.SetCommands(a.commands)
	a.showCommandDialog = true
	return a.commandDialog.Init()
}

func (a *appModel) openSessionDialog() tea.Cmd {
	sessions, err := a.app.Sessions.List(context.Background())
	if err != nil {
		return util.ReportError(err)
	}
	if len(sessions) == 0 {
		return util.ReportWarn("No sessions available")
	}

	a.sessionDialog.SetSessions(sessions)
	a.showSessionDialog = true
	return nil
}

func (a *appModel) openModelDialog() tea.Cmd {
	a.showModelDialog = true
	return a.modelDialog.Init()
}

func (a *appModel) openThemeDialog() tea.Cmd {
	a.showThemeDialog = true
	return a.themeDialog.Init()
}

func (a *appModel) openFilepicker() tea.Cmd {
	a.showFilepicker = true
	a.filepicker.ToggleFilepicker(a.showFilepicker)
	return nil
}

func (a *appModel) setTerminalFocus(focused bool) tea.Cmd {
	return func() tea.Msg {
		return terminalFocusChangedMsg{focused: focused}
	}
}

// triggerResize emits a synthetic WindowSizeMsg so all components re-layout
// after the terminal panel is shown or hidden.
func (a *appModel) triggerResize() tea.Cmd {
	w := a.width
	h := a.height + 1 // +1 because the handler subtracts 1 for the status bar
	return func() tea.Msg {
		return tea.WindowSizeMsg{Width: w, Height: h}
	}
}

// toggleTerminalPanel implements Ctrl+Alt+Y behaviour:
//   - No terminals: open terminal, show panel, give focus
//   - Panel visible + focused: blur (return focus to chat)
//   - Panel visible + not focused: focus terminal
//   - Panel hidden: show panel, don't auto-focus
func (a *appModel) toggleTerminalPanel() tea.Cmd {
	logging.Info("tui: toggle-terminal invoked",
		"panel_visible", a.terminalPanel.IsVisible(),
		"has_terminals", a.terminalPanel.HasTerminals(),
		"focused", a.terminalFocused,
	)

	// Open a new terminal if none exist yet.
	if !a.terminalPanel.HasTerminals() {
		logging.Info("tui: opening first terminal",
			"panel_width", a.width, "panel_height", a.height)
		initCmd := a.terminalPanel.OpenNewTerminal()
		if initCmd == nil {
			logging.Error("tui: OpenNewTerminal returned nil initCmd — terminal creation failed")
			return nil
		}
		return tea.Batch(initCmd, a.setTerminalFocus(true), a.triggerResize())
	}

	// Show panel if hidden.
	if !a.terminalPanel.IsVisible() {
		a.terminalPanel.Show()
		return a.triggerResize()
	}

	// Panel visible: toggle focus.
	if a.terminalFocused {
		return tea.Batch(a.setTerminalFocus(false), a.triggerResize())
	}
	// Panel visible but not focused: hide panel.
	a.terminalPanel.Toggle()
	return tea.Batch(a.setTerminalFocus(false), a.triggerResize())
}

// openNewTerminalTab opens a new terminal tab, makes the panel visible and focuses it.
func (a *appModel) openNewTerminalTab() tea.Cmd {
	logging.Info("tui: open-new-terminal-tab invoked",
		"panel_visible", a.terminalPanel.IsVisible(),
		"tab_count", a.terminalPanel.HasTerminals(),
	)
	initCmd := a.terminalPanel.OpenNewTerminal()
	return tea.Batch(initCmd, a.setTerminalFocus(true), a.triggerResize())
}

// nextTerminalTab cycles to the next terminal tab, showing the panel and focusing it.
func (a *appModel) nextTerminalTab() tea.Cmd {
	if !a.terminalPanel.HasTerminals() {
		return nil
	}
	a.terminalPanel.NextTab()
	if !a.terminalPanel.IsVisible() {
		a.terminalPanel.Show()
	}
	return tea.Batch(a.setTerminalFocus(true), a.triggerResize())
}

func (a *appModel) findCommand(id string) (dialog.Command, bool) {
	for _, cmd := range a.commands {
		if cmd.ID == id {
			return cmd, true
		}
	}
	return dialog.Command{}, false
}

func (a *appModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.app.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn("Agent is busy, please wait...")
	}

	var cmds []tea.Cmd
	if _, ok := a.loadedPages[pageID]; !ok {
		cmd := a.pages[pageID].Init()
		cmds = append(cmds, cmd)
		a.loadedPages[pageID] = true
	}
	// Clear any active modals on the page being navigated away from.
	if a.currentPage != pageID {
		if mp, ok := a.pages[a.currentPage].(page.ModalPage); ok {
			mp.ClearModals()
		}
	}
	a.previousPage = a.currentPage
	a.currentPage = pageID
	if sizable, ok := a.pages[a.currentPage].(layout.Sizeable); ok {
		cmd := sizable.SetSize(a.width, a.height)
		cmds = append(cmds, cmd)
	}
	a.syncChatState()

	return tea.Batch(cmds...)
}

func (a *appModel) syncChatState() {
	chatPage, ok := a.pages[page.ChatPage].(*page.ChatPageModel)
	if !ok {
		return
	}
	a.chatPage = chatPage
	a.fileTree = chatPage.FileTree()
	a.viewer = chatPage.Viewer()
	a.tabBar = chatPage.TabBar()
	a.layoutMode = chatPage.LayoutMode()
}

func (a appModel) helpSections() []dialog.HelpSection {
	globalBindings := append([]key.Binding{}, a.keys.Global.Bindings()...)
	if !a.app.CoderAgent.IsBusy() {
		globalBindings = append(globalBindings, helpEsc)
	}

	sections := []dialog.HelpSection{
		{Title: "Global", Bindings: globalBindings},
	}

	sections = append(sections, a.pageHelpSections()...)
	sections = append(sections, a.dialogHelpSections()...)
	return sections
}

func (a appModel) pageHelpSections() []dialog.HelpSection {
	switch a.currentPage {
	case page.ChatPage:
		a.syncChatState()
		pageBindings := a.pageBindings(a.pages[a.currentPage])
		chatBindings := []key.Binding{
			a.keys.Chat.SwitchSession,
			a.keys.Chat.NewSession,
			a.keys.Chat.Commands,
			a.keys.Chat.Models,
			a.keys.Chat.ShowCompletionDialog,
			a.keys.Chat.ToggleSidebar,
			a.keys.Chat.NextPanel,
			a.keys.Chat.Cancel,
		}
		editorBindings := []key.Binding{
			a.keys.Chat.Send,
			a.keys.Chat.NewLine,
		}
		navigationBindings := []key.Binding{
			a.keys.Chat.PageUp,
			a.keys.Chat.PageDown,
			a.keys.Chat.HalfPageUp,
			a.keys.Chat.HalfPageDown,
		}

		extraEditorBindings := a.excludeBindings(
			pageBindings,
			a.keys.Global.Bindings(),
			chatBindings,
			editorBindings,
			navigationBindings,
			a.keys.FileTree.Bindings(),
			a.keys.Editor.Bindings(),
		)

		sections := []dialog.HelpSection{
			{Title: "Chat", Bindings: chatBindings},
			{Title: "Editor", Bindings: append(editorBindings, extraEditorBindings...)},
			{Title: "Navigation", Bindings: navigationBindings},
		}
		if a.layoutMode != page.ChatOnly {
			sections = append(sections,
				dialog.HelpSection{Title: "File tree", Bindings: a.keys.FileTree.Bindings()},
				dialog.HelpSection{Title: "Layout", Bindings: filterHelpBindings(
					a.keys.Chat.ToggleSidebar,
					a.keys.Chat.NextPanel,
					key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "toggle editor+chat layout")),
				)},
			)
		}
		if a.layoutMode == page.SidebarEditor || a.layoutMode == page.SidebarChat {
			sections = append(sections, dialog.HelpSection{Title: "File", Bindings: filterHelpBindings(
				a.keys.Editor.EditExternal,
				a.keys.Editor.Save,
			)})
		}
		return sections
	case page.LogsPage:
		return []dialog.HelpSection{{
			Title:    "Logs",
			Bindings: append(a.pageBindings(a.pages[a.currentPage]), logsKeyReturnKey),
		}}
	case page.OrchestratorPage:
		return []dialog.HelpSection{{
			Title:    "Orchestrator",
			Bindings: a.pageBindings(a.pages[a.currentPage]),
		}}
	case page.SettingsPage:
		return []dialog.HelpSection{{
			Title:    "Settings",
			Bindings: a.pageBindings(a.pages[a.currentPage]),
		}}
	case page.SnapshotsPage:
		return []dialog.HelpSection{{
			Title:    "Snapshots",
			Bindings: a.pageBindings(a.pages[a.currentPage]),
		}}
	case page.EvaluatorPage:
		return []dialog.HelpSection{{
			Title:    "Self-Improvement",
			Bindings: a.pageBindings(a.pages[a.currentPage]),
		}}
	default:
		return nil
	}
}

func (a appModel) dialogHelpSections() []dialog.HelpSection {
	var sections []dialog.HelpSection

	if a.showPermissions {
		sections = append(sections, dialog.HelpSection{
			Title:    "Permission dialog",
			Bindings: a.permissions.BindingKeys(),
		})
	}
	if a.showQuit {
		sections = append(sections, dialog.HelpSection{
			Title:    "Quit dialog",
			Bindings: a.quit.BindingKeys(),
		})
	}
	if a.showSessionDialog {
		sections = append(sections, dialog.HelpSection{
			Title:    "Session dialog",
			Bindings: a.sessionDialog.BindingKeys(),
		})
	}
	if a.showCommandDialog {
		sections = append(sections, dialog.HelpSection{
			Title:    "Command dialog",
			Bindings: a.commandDialog.BindingKeys(),
		})
	}
	if a.showModelDialog {
		sections = append(sections, dialog.HelpSection{
			Title:    "Model dialog",
			Bindings: a.modelDialog.BindingKeys(),
		})
	}
	if a.showInitDialog {
		sections = append(sections, dialog.HelpSection{
			Title:    "Initialize dialog",
			Bindings: a.initDialog.Bindings(),
		})
	}
	if a.showThemeDialog {
		sections = append(sections, dialog.HelpSection{
			Title:    "Theme dialog",
			Bindings: a.themeDialog.BindingKeys(),
		})
	}
	if a.showFilepicker {
		sections = append(sections, dialog.HelpSection{
			Title:    "File picker",
			Bindings: a.filepicker.BindingKeys(),
		})
	}
	if a.showMultiArgumentsDialog {
		sections = append(sections, dialog.HelpSection{
			Title:    "Arguments dialog",
			Bindings: a.multiArgumentsDialog.Bindings(),
		})
	}

	return sections
}

func (a appModel) pageBindings(model tea.Model) []key.Binding {
	if bindings, ok := model.(layout.Bindings); ok {
		return bindings.BindingKeys()
	}
	return nil
}

func (a appModel) excludeBindings(bindings []key.Binding, groups ...[]key.Binding) []key.Binding {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, binding := range group {
			seen[a.bindingSignature(binding)] = struct{}{}
		}
	}

	filtered := make([]key.Binding, 0, len(bindings))
	for _, binding := range bindings {
		signature := a.bindingSignature(binding)
		if _, ok := seen[signature]; ok {
			continue
		}
		filtered = append(filtered, binding)
		seen[signature] = struct{}{}
	}
	return filtered
}

func (a appModel) bindingSignature(binding key.Binding) string {
	return strings.Join(binding.Keys(), "|")
}

func (a appModel) View() string {
	pageView := a.pages[a.currentPage].View()

	var components []string
	if a.terminalPanel.IsVisible() {
		components = []string{
			pageView,
			tuizone.MarkTerminalPanel(a.terminalPanel.View()),
		}
	} else {
		components = []string{pageView}
	}

	components = append(components, a.status.View())

	appView := lipgloss.JoinVertical(lipgloss.Top, components...)

	if a.showPermissions {
		overlay := a.permissions.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showFilepicker {
		overlay := a.filepicker.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)

	}

	// Show compacting status overlay
	if a.isCompacting {
		t := theme.CurrentTheme()
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocused()).
			BorderBackground(t.Background()).
			Padding(1, 2).
			Background(t.Background()).
			Foreground(t.Text())

		overlay := style.Render("Summarizing\n" + a.compactingMessage)
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showHelp {
		a.help.SetSections(a.helpSections())

		overlay := a.help.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showQuit {
		overlay := a.quit.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showSessionDialog {
		overlay := a.sessionDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showModelDialog {
		overlay := a.modelDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showCommandDialog {
		overlay := a.commandDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showInitDialog {
		overlay := a.initDialog.View()
		appView = layout.PlaceOverlay(
			a.width/2-lipgloss.Width(overlay)/2,
			a.height/2-lipgloss.Height(overlay)/2,
			overlay,
			appView,
			true,
		)
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showMultiArgumentsDialog {
		overlay := a.multiArgumentsDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showClaudeLoginDialog {
		overlay := a.claudeLoginDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	// Render bubbleup alert overlay as the final layer
	appView = a.alert.Render(appView)

	return tuizone.Manager.Scan(appView)
}

// buildDiagnosticsSummary returns a human-readable summary of LSP diagnostics.
func (a *appModel) buildDiagnosticsSummary() string {
	if a.app == nil || len(a.app.LSPClients) == 0 {
		return "LSP: No language servers active"
	}

	var errors, warnings, hints, infos int
	var fileCount int
	for _, client := range a.app.LSPClients {
		for _, diags := range client.GetDiagnostics() {
			if len(diags) > 0 {
				fileCount++
			}
			for _, d := range diags {
				switch d.Severity {
				case protocol.SeverityError:
					errors++
				case protocol.SeverityWarning:
					warnings++
				case protocol.SeverityHint:
					hints++
				case protocol.SeverityInformation:
					infos++
				}
			}
		}
	}

	if errors == 0 && warnings == 0 && hints == 0 && infos == 0 {
		return "LSP: No diagnostics"
	}

	var parts []string
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", errors))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d warnings", warnings))
	}
	if hints > 0 {
		parts = append(parts, fmt.Sprintf("%d hints", hints))
	}
	if infos > 0 {
		parts = append(parts, fmt.Sprintf("%d info", infos))
	}
	return fmt.Sprintf("LSP Diagnostics (%d files): %s", fileCount, strings.Join(parts, ", "))
}

func (a *appModel) handleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if a.showQuit || a.showPermissions || a.showSessionDialog || a.showCommandDialog ||
		a.showModelDialog || a.showInitDialog || a.showFilepicker || a.showThemeDialog ||
		a.showMultiArgumentsDialog {
		return nil, false
	}

	// Mouse wheel: forward to terminal panel for scrollback.
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		if a.terminalPanel.IsVisible() && a.terminalPanel.HasTerminals() &&
			tuizone.InBounds(tuizone.TerminalPanel, msg) {
			newPanel, panelCmd := a.terminalPanel.Update(msg)
			a.terminalPanel = newPanel
			return panelCmd, true
		}
		return nil, false
	}

	// Middle-click: close terminal tab.
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonMiddle {
		if a.terminalPanel.IsVisible() {
			for i := 0; i < a.terminalPanel.TabCount(); i++ {
				if tuizone.InBounds(tuizone.TerminalTabID(i), msg) {
					a.terminalPanel.CloseTabAt(i)
					if !a.terminalPanel.HasTerminals() {
						a.terminalFocused = false
						a.terminalPanel.Blur()
					}
					logging.Debug("tui: middle-click closed terminal tab", "idx", i)
					return a.triggerResize(), true
				}
			}
		}
		return nil, false
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}

	if tuizone.InBounds(tuizone.StatusHelp, msg) {
		a.showHelp = !a.showHelp
		return nil, true
	}

	if tuizone.InBounds(tuizone.StatusDiagnostics, msg) {
		summary := a.buildDiagnosticsSummary()
		if summary != "" {
			return util.CmdHandler(util.InfoMsg{
				Type: util.InfoTypeInfo,
				Msg:  summary,
				TTL:  15 * time.Second,
			}), true
		}
		return nil, true
	}

	if a.currentPage == page.ChatPage && tuizone.InBounds(tuizone.StatusSession, msg) {
		return a.openSessionDialog(), true
	}
	if tuizone.InBounds(tuizone.StatusModel, msg) {
		return a.openModelDialog(), true
	}

	// Left-click on a terminal tab → switch to that tab and focus.
	if a.terminalPanel.IsVisible() {
		for i := 0; i < a.terminalPanel.TabCount(); i++ {
			if tuizone.InBounds(tuizone.TerminalTabID(i), msg) {
				a.terminalPanel.SetActiveTab(i)
				a.terminalFocused = true
				a.terminalPanel.Focus()
				logging.Debug("tui: left-click on terminal tab → switch and focus", "idx", i)
				return nil, true
			}
		}
	}

	// Left-click on the terminal body → give focus to terminal.
	if a.terminalPanel.IsVisible() && a.terminalPanel.HasTerminals() &&
		tuizone.InBounds(tuizone.TerminalPanel, msg) {
		if !a.terminalFocused {
			a.terminalFocused = true
			a.terminalPanel.Focus()
			logging.Debug("tui: mouse click on terminal panel → focus terminal")
		}
		return nil, true
	}

	// Left-click on the chat viewport → return focus to chat (blur terminal).
	// Return handled=false so the click propagates to the chat page.
	if tuizone.InBounds(tuizone.ChatViewport, msg) {
		if a.terminalFocused {
			a.terminalFocused = false
			a.terminalPanel.Blur()
			logging.Debug("tui: mouse click on chat panel → blur terminal")
		}
		return nil, false
	}

	return nil, false
}

func New(app *app.App) tea.Model {
	startPage := page.ChatPage
	chatPage := page.NewChatPage(app)
	model := &appModel{
		currentPage:   startPage,
		loadedPages:   make(map[page.PageID]bool),
		keys:          DefaultKeyMap(),
		status:        core.NewStatusCmp(app.LSPClients),
		help:          dialog.NewHelpCmp(),
		quit:          dialog.NewQuitCmp(),
		sessionDialog: dialog.NewSessionDialogCmp(),
		commandDialog: dialog.NewCommandDialogCmp(),
		modelDialog:   dialog.NewModelDialogCmp(),
		permissions:   dialog.NewPermissionDialogCmp(),
		initDialog:    dialog.NewInitDialogCmp(),
		themeDialog:   dialog.NewThemeDialogCmp(),
		infoDialog:    dialog.NewInfoDialogCmp(),
		app:           app,
		commands:      []dialog.Command{},
		chatPage:      chatPage,
		fileTree:      chatPage.FileTree(),
		viewer:        chatPage.Viewer(),
		tabBar:        chatPage.TabBar(),
		layoutMode:    chatPage.LayoutMode(),
		pages: map[page.PageID]tea.Model{
			page.ChatPage:         chatPage,
			page.LogsPage:         page.NewLogsPage(),
			page.SettingsPage:     page.NewSettingsPage(app),
			page.OrchestratorPage: page.NewOrchestratorPage(app),
			page.SnapshotsPage:    page.NewSnapshotsPage(),
			page.EvaluatorPage:    page.NewEvaluatorPage(app.Evaluator),
		},
		filepicker:    dialog.NewFilepickerCmp(app),
		terminalPanel: terminal.NewTerminalPanel(),
		alert: bubbleup.NewAlertModel(60, false, 5*time.Second).
			WithPosition(bubbleup.TopRightPosition).
			WithMinWidth(20).
			WithUnicodePrefix().
			WithAllowEscToClose(),
	}

	model.RegisterCommand(dialog.Command{
		ID:          "init",
		Title:       "Initialize Project",
		Description: "Create/Update AGENTS.md",
		Category:    dialog.CommandCategoryGeneral,
		Handler: func(cmd dialog.Command) tea.Cmd {
			prompt := `Please analyze this codebase and create or update AGENTS.md containing:
1. Build/lint/test commands - especially for running a single test
2. Code style guidelines including imports, formatting, types, naming conventions, error handling, etc.

The file you create will be given to agentic coding agents (such as yourself) that operate in this repository. Make it about 20 lines long.
If AGENTS.md already exists, improve it.
If PANDO.md or CLAUDE.md already exist, migrate any useful repository guidance from them into AGENTS.md.
If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include them.`
			return tea.Batch(
				util.CmdHandler(chat.SendMsg{
					Text: prompt,
				}),
			)
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compact",
		Title:       "Compact Session",
		Description: "Summarize the current session and create a new one with the summary",
		Category:    dialog.CommandCategoryGeneral,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return startCompactSessionMsg{}
			}
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "switch-session",
		Title:       "Switch Session",
		Description: "Open the session switcher",
		Shortcut:    "Ctrl+S",
		Category:    dialog.CommandCategorySessions,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.OpenSessionDialogMsg{})
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "select-model",
		Title:       "Select Model",
		Description: "Open the model selection dialog",
		Shortcut:    "Ctrl+O",
		Category:    dialog.CommandCategoryModels,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.OpenModelDialogMsg{})
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "select-files",
		Title:       "Select Files",
		Description: "Open the file picker and attach files",
		Shortcut:    "Ctrl+F",
		Category:    dialog.CommandCategoryFiles,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.OpenFilepickerMsg{})
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "edit-file",
		Title:       "Edit File",
		Description: "Open a file in the inline editor (Ctrl+I on active file)",
		Shortcut:    "Ctrl+I",
		Category:    dialog.CommandCategoryFiles,
		Handler: func(cmd dialog.Command) tea.Cmd {
			// If there's an active file, open it in inline editor
			if model.tabBar != nil && model.tabBar.ActivePath() != "" {
				return util.CmdHandler(editor.OpenEditableFileMsg{Path: model.tabBar.ActivePath()})
			}
			// Otherwise open the file picker in edit mode
			model.filepickerMode = "edit"
			model.showFilepicker = true
			model.filepicker.ToggleFilepicker(true)
			return nil
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "settings",
		Title:       "Settings",
		Description: "Open configuration settings",
		Shortcut:    "Ctrl+G",
		Category:    dialog.CommandCategoryView,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(page.PageChangeMsg{ID: page.SettingsPage})
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "skills",
		Title:       "Skills",
		Description: "Manage skills (list, activate, deactivate)",
		Category:    dialog.CommandCategoryGeneral,
		Handler: func(cmd dialog.Command) tea.Cmd {
			if app.SkillManager == nil {
				return util.ReportWarn("Skills system not enabled")
			}
			metadata := app.SkillManager.GetAllMetadata()
			if len(metadata) == 0 {
				return util.ReportInfo("No skills discovered")
			}
			info := "Discovered skills:\n"
			for _, m := range metadata {
				info += fmt.Sprintf("  - %s v%s: %s\n", m.Name, m.Version, m.Description)
			}
			return tea.Batch(
				util.CmdHandler(chat.SendMsg{Text: "/skills list\n" + info}),
			)
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "logs",
		Title:       "View Logs",
		Description: "Open the logs page",
		Shortcut:    "Ctrl+L",
		Category:    dialog.CommandCategoryView,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(page.PageChangeMsg{ID: page.LogsPage})
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "snapshots",
		Title:       "View Snapshots",
		Description: "Browse session snapshots and revert changes",
		Shortcut:    "Ctrl+Shift+S",
		Category:    dialog.CommandCategoryView,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return page.PageChangeMsg{ID: page.SnapshotsPage}
			}
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "evaluator",
		Title:       "Self-Improvement",
		Description: "Open the self-improvement evaluator dashboard",
		Shortcut:    "Ctrl+Shift+E",
		Category:    dialog.CommandCategoryView,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return page.PageChangeMsg{ID: page.EvaluatorPage}
			}
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "create-snapshot",
		Title:       "Create Snapshot",
		Description: "Create a manual snapshot of the current working directory",
		Category:    dialog.CommandCategoryGeneral,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				if app.Snapshots != nil {
					ctx := context.Background()
					sessionID := model.selectedSession.ID
					if sessionID == "" {
						sessionID = "manual"
					}
					_, err := app.Snapshots.Create(ctx, sessionID, "manual", "Manual snapshot")
					if err != nil {
						logging.Error("Failed to create manual snapshot", "error", err)
					} else {
						logging.Info("Manual snapshot created")
					}
				}
				return nil
			}
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "switch-theme",
		Title:       "Switch Theme",
		Description: "Open the theme picker",
		Shortcut:    "Ctrl+T",
		Category:    dialog.CommandCategoryView,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.OpenThemeDialogMsg{})
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "orchestrator",
		Title:       "Orchestrator",
		Description: "Open the Mesnada orchestrator dashboard",
		Shortcut:    "Ctrl+M",
		Category:    dialog.CommandCategoryView,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(page.PageChangeMsg{ID: page.OrchestratorPage})
		},
	})
	       model.RegisterCommand(dialog.Command{
		       ID:          "copilot-login",
		       Title:       "Copilot Login",
		       Description: "Authenticate with GitHub Copilot",
		       Category:    dialog.CommandCategoryGeneral,
		       Handler: func(cmd dialog.Command) tea.Cmd {
				return copilotLoginCommand()
		       },
	       })
	       model.RegisterCommand(dialog.Command{
		       ID:          "copilot-logout",
		       Title:       "Copilot Logout",
		       Description: "Remove Copilot authentication",
		       Category:    dialog.CommandCategoryGeneral,
		       Handler: func(cmd dialog.Command) tea.Cmd {
				return copilotLogoutCommand()
		       },
	       })
	       model.RegisterCommand(dialog.Command{
		       ID:          "copilot-status",
		       Title:       "Copilot Status",
		       Description: "Show Copilot authentication status",
		       Category:    dialog.CommandCategoryGeneral,
		       Handler: func(cmd dialog.Command) tea.Cmd {
				return copilotStatusCommand()
		       },
	       })
	model.RegisterCommand(dialog.Command{
		ID:          "open-terminal",
		Title:       "Open Terminal Emulator Embedded",
		Description: "Open an embedded terminal in the bottom panel",
		Category:    dialog.CommandCategoryView,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return model.toggleTerminalPanel()
		},
	})

	// Claude account commands
	model.RegisterCommand(dialog.Command{
		ID:          "claude:login",
		Title:       "Login with Claude.ai",
		Description: "Authenticate with your claude.ai account (OAuth)",
		Category:    dialog.CommandCategoryAccount,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return dialog.ClaudeLoginCmd()
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "claude:logout",
		Title:       "Logout from Claude.ai",
		Description: "Remove saved Claude.ai credentials",
		Category:    dialog.CommandCategoryAccount,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return dialog.ClaudeLogoutCmd()
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "claude:stats:daily",
		Title:       "Claude: Today's usage",
		Description: "Show today's message and token statistics",
		Category:    dialog.CommandCategoryAccount,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return dialog.LoadClaudeStatsCmd()
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "claude:stats:weekly",
		Title:       "Claude: Weekly usage",
		Description: "Show this week's usage summary",
		Category:    dialog.CommandCategoryAccount,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return dialog.LoadClaudeStatsCmd()
		},
	})
	model.RegisterCommand(dialog.Command{
		ID:          "claude:stats:browser",
		Title:       "Claude: Open usage page",
		Description: "Open claude.ai/settings/usage in browser",
		Category:    dialog.CommandCategoryAccount,
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				_ = auth.OpenBrowser("https://claude.ai/settings/usage")
				return nil
			}
		},
	})

	// Load custom commands
	customCommands, err := dialog.LoadCustomCommands()
	if err != nil {
		logging.Warn("Failed to load custom commands", "error", err)
	} else {
		for _, cmd := range customCommands {
			model.RegisterCommand(cmd)
		}
	}

	return model
}
