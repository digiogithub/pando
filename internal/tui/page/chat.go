package page

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/agentsmd"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/completions"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/learning"
	agentpkg "github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/ponytail"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/superpowers"
	"github.com/digiogithub/pando/internal/tui/components/chat"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
	"github.com/digiogithub/pando/internal/tui/components/editor"
	"github.com/digiogithub/pando/internal/tui/components/filetree"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/util"
)

type ChatLayoutMode int

const (
	ChatOnly ChatLayoutMode = iota
	SidebarChat
	SidebarEditor
	EditorChatSplit
	EditorChatTab
)

type panelFocus int

const (
	focusChat panelFocus = iota
	focusFileTree
	focusEditor
	focusChatRight
)

type ChatPageModel struct {
	app *app.App

	width  int
	height int

	layout                    layout.SplitPaneLayout
	sidebar                   layout.SplitPaneLayout
	chatLayout                layout.SplitPaneLayout
	chatContainer             layout.Container
	fileTreePanel             layout.Container
	editorPanel               layout.Container
	editorChatPanel           layout.Container
	messages                  layout.Container
	editor                    layout.Container
	completionDialog          dialog.CompletionDialog
	showCompletionDialog      bool
	slashCompletionDialog     dialog.CompletionDialog
	showSlashCompletionDialog bool

	chatTabWorkspace *editorChatTabWorkspace

	tabHeader *mainTabBar

	// infoSidebar is the right-hand information column (session/Plan/Modified
	// Files) shown only in the Chat tab (ChatOnly) when the terminal is wide
	// enough. infoSidebarContainer wraps it for placement in the split layout.
	infoSidebar          chat.ChatInfoSidebar
	infoSidebarContainer layout.Container

	session    session.Session
	layoutMode ChatLayoutMode
	focus      panelFocus

	fileTree filetree.Component
	viewer   editor.FileViewerComponent
	tabBar   *editor.TabBar

	editorWorkspace *editorWorkspace

	goalRunner           *agentpkg.GoalRunner
	goal                 *chat.GoalState
	goalCancel           context.CancelFunc
	goalCancelSessionID  string
	goalObjectivePending string
	goalEventCh          <-chan agentpkg.AgentEvent
}

// goalEventChanMsg pumps the goal runner's event channel into the Bubble Tea
// loop. Per-iteration UI updates already arrive through the agent pubsub
// stream; the important signal here is the channel CLOSING, which is the only
// notification that the goal reached a terminal state (completed/blocked/
// failed/...). finishGoal only writes the new status to the DB and publishes
// nothing, so without draining this channel the TUI would keep believing the
// goal is still running (stale input lock and stuck Ctrl+C).
type goalEventChanMsg struct {
	sessionID string
	ch        <-chan agentpkg.AgentEvent
	ok        bool
}

type ChatKeyMap struct {
	ShowCompletionDialog      key.Binding
	ShowSlashCompletionDialog key.Binding
	NewSession                key.Binding
	Cancel                    key.Binding
	ToggleSidebar             key.Binding
	NextPanel                 key.Binding
	ToggleEditorChat          key.Binding
	SelectTab                 key.Binding
	ToggleHiddenFiles         key.Binding
	ToggleInfoSidebar         key.Binding
}

type editorWorkspace struct {
	width  int
	height int

	viewer     editor.FileViewerComponent
	fileEditor editor.FileEditableComponent
	tabBar     *editor.TabBar
}

var keyMap = ChatKeyMap{
	ShowCompletionDialog: key.NewBinding(
		key.WithKeys("@"),
		key.WithHelp("@", "complete"),
	),
	ShowSlashCompletionDialog: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "slash commands"),
	),
	NewSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	ToggleSidebar: key.NewBinding(
		key.WithKeys("ctrl+b"),
		key.WithHelp("ctrl+b", "toggle sidebar"),
	),
	NextPanel: key.NewBinding(
		key.WithKeys("tab", "ctrl+["),
		key.WithHelp("tab", "switch panel"),
	),
	ToggleEditorChat: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "toggle editor+chat layout"),
	),
	SelectTab: key.NewBinding(
		key.WithKeys("alt+1", "alt+2", "alt+3"),
		key.WithHelp("alt+1/2/3", "switch workspace tab"),
	),
	ToggleHiddenFiles: key.NewBinding(
		key.WithKeys("ctrl+shift+h"),
		key.WithHelp("ctrl+shift+h", "toggle hidden files"),
	),
	ToggleInfoSidebar: key.NewBinding(
		key.WithKeys("ctrl+shift+b"),
		key.WithHelp("ctrl+shift+b", "toggle info sidebar"),
	),
}

func newEditorWorkspace(viewer editor.FileViewerComponent, tabBar *editor.TabBar) *editorWorkspace {
	return &editorWorkspace{
		viewer:     viewer,
		fileEditor: editor.NewFileEditor(),
		tabBar:     tabBar,
	}
}

func (w *editorWorkspace) Init() tea.Cmd {
	return w.viewer.Init()
}

func (w *editorWorkspace) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	prevActive := w.tabBar.ActivePath()
	prevCount := w.tabBar.Count()
	w.tabBar.Update(msg)

	activePath := w.tabBar.ActivePath()
	isEditable := w.tabBar.IsActiveEditable()

	// Route messages to the correct active component
	if isEditable {
		editorModel, editorCmd := w.fileEditor.Update(msg)
		w.fileEditor = editorModel.(editor.FileEditableComponent)
		if editorCmd != nil {
			cmds = append(cmds, editorCmd)
		}
		// Sync dirty state to tab
		if w.fileEditor.IsDirty() {
			w.tabBar.SetDirty(activePath, true)
		}
	} else {
		viewerModel, viewerCmd := w.viewer.Update(msg)
		w.viewer = viewerModel.(editor.FileViewerComponent)
		if viewerCmd != nil {
			cmds = append(cmds, viewerCmd)
		}
	}

	// Handle ExitEditModeMsg: switch the active tab back to view-only mode.
	if exitMsg, ok := msg.(editor.ExitEditModeMsg); ok {
		w.tabBar.SetActiveEditable(false)
		if exitMsg.Path != "" {
			cmds = append(cmds, w.viewer.OpenFile(exitMsg.Path))
		}
		return w, tea.Batch(cmds...)
	}

	// Handle FileEditSavedMsg: clear dirty flag on the tab
	if savedMsg, ok := msg.(editor.FileEditSavedMsg); ok {
		w.tabBar.SetDirty(savedMsg.Path, false)
		cmds = append(cmds, util.ReportInfo("Saved: "+savedMsg.Path))
	}

	switch {
	case prevCount > 0 && w.tabBar.Count() == 0:
		cmds = append(cmds, util.CmdHandler(editor.CloseViewerMsg{Path: prevActive}))
	case activePath != "" && activePath != prevActive:
		if isEditable {
			cmds = append(cmds, w.fileEditor.OpenFile(activePath))
		} else {
			cmds = append(cmds, w.viewer.OpenFile(activePath))
		}
	}

	return w, tea.Batch(cmds...)
}

func (w *editorWorkspace) View() string {
	if w.width <= 0 || w.height <= 0 {
		return ""
	}

	tabView := w.tabBar.View()
	viewHeight := max(w.height-lipgloss.Height(tabView), 0)

	if w.tabBar.IsActiveEditable() {
		_ = w.fileEditor.SetSize(w.width, viewHeight)
		return lipgloss.JoinVertical(lipgloss.Left, tabView, w.fileEditor.View())
	}

	if sizeable, ok := w.viewer.(layout.Sizeable); ok {
		_ = sizeable.SetSize(w.width, viewHeight)
	}
	return lipgloss.JoinVertical(lipgloss.Left, tabView, w.viewer.View())
}

func (w *editorWorkspace) SetSize(width, height int) tea.Cmd {
	w.width = max(width, 0)
	w.height = max(height, 0)
	w.tabBar.SetSize(w.width)
	if w.tabBar.IsActiveEditable() {
		return w.fileEditor.SetSize(w.width, max(w.height-1, 0))
	}
	return w.viewer.SetSize(w.width, max(w.height-1, 0))
}

func (w *editorWorkspace) GetSize() (int, int) {
	return w.width, w.height
}

func (w *editorWorkspace) BindingKeys() []key.Binding {
	bindings := append([]key.Binding{}, w.tabBar.BindingKeys()...)
	bindings = append(bindings, w.viewer.BindingKeys()...)
	return bindings
}

func (w *editorWorkspace) OpenFile(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	w.tabBar.OpenTab(path)
	return w.viewer.OpenFile(path)
}

// OpenEditableFile opens a file in editable mode in a new or existing tab.
func (w *editorWorkspace) OpenEditableFile(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	w.tabBar.OpenEditableTab(path)
	return w.fileEditor.OpenFile(path)
}

func (w *editorWorkspace) HasTabs() bool {
	return w.tabBar.Count() > 0
}

func (w *editorWorkspace) ActivePath() string {
	return w.tabBar.ActivePath()
}

type editorChatTabWorkspace struct {
	width  int
	height int

	viewer    editor.FileViewerComponent
	tabBar    *editor.TabBar
	chatView  layout.SplitPaneLayout
	showChat  bool
	chatTabID string
}

func newEditorChatTabWorkspace(viewer editor.FileViewerComponent, tabBar *editor.TabBar, chatView layout.SplitPaneLayout) *editorChatTabWorkspace {
	return &editorChatTabWorkspace{
		viewer:    viewer,
		tabBar:    tabBar,
		chatView:  chatView,
		showChat:  false,
		chatTabID: "__chat__",
	}
}

func (w *editorChatTabWorkspace) Init() tea.Cmd {
	return tea.Batch(w.viewer.Init(), w.chatView.Init())
}

func (w *editorChatTabWorkspace) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	wasShowingChat := w.showChat
	prevActive := w.tabBar.ActivePath()
	prevCount := w.tabBar.Count()

	// Don't forward key events to the tabBar when chat is active — otherwise
	// the tabBar intercepts characters and navigation keys meant for the chat
	// input textarea.
	if _, isKey := msg.(tea.KeyMsg); !isKey || !w.showChat {
		w.tabBar.Update(msg)
	}

	activePath := w.tabBar.ActivePath()
	if activePath == w.chatTabID {
		w.showChat = true
		// When the chat tab just became active, focus the textarea.
		if !wasShowingChat {
			cmds = append(cmds, util.CmdHandler(chat.FocusChatEditorMsg{}))
		}
		chatModel, chatCmd := w.chatView.Update(msg)
		w.chatView = chatModel.(layout.SplitPaneLayout)
		if chatCmd != nil {
			cmds = append(cmds, chatCmd)
		}
		return w, tea.Batch(cmds...)
	}
	w.showChat = false

	viewerModel, viewerCmd := w.viewer.Update(msg)
	w.viewer = viewerModel.(editor.FileViewerComponent)
	if viewerCmd != nil {
		cmds = append(cmds, viewerCmd)
	}

	switch {
	case prevCount > 0 && w.tabBar.Count() == 0:
		cmds = append(cmds, util.CmdHandler(editor.CloseViewerMsg{Path: prevActive}))
	case activePath != "" && activePath != w.chatTabID && activePath != prevActive:
		cmds = append(cmds, w.viewer.OpenFile(activePath))
	}

	return w, tea.Batch(cmds...)
}

func (w *editorChatTabWorkspace) View() string {
	if w.width <= 0 || w.height <= 0 {
		return ""
	}

	tabView := w.tabBar.View()
	viewHeight := max(w.height-lipgloss.Height(tabView), 0)

	if w.showChat {
		w.chatView.SetSize(w.width, viewHeight)
		return lipgloss.JoinVertical(lipgloss.Left, tabView, w.chatView.View())
	}

	if sizeable, ok := w.viewer.(layout.Sizeable); ok {
		_ = sizeable.SetSize(w.width, viewHeight)
	}
	return lipgloss.JoinVertical(lipgloss.Left, tabView, w.viewer.View())
}

func (w *editorChatTabWorkspace) SetSize(width, height int) tea.Cmd {
	w.width = max(width, 0)
	w.height = max(height, 0)
	w.tabBar.SetSize(w.width)
	if w.showChat {
		return w.chatView.SetSize(w.width, max(w.height-1, 0))
	}
	return w.viewer.SetSize(w.width, max(w.height-1, 0))
}

func (w *editorChatTabWorkspace) GetSize() (int, int) {
	return w.width, w.height
}

func (w *editorChatTabWorkspace) BindingKeys() []key.Binding {
	bindings := append([]key.Binding{}, w.tabBar.BindingKeys()...)
	bindings = append(bindings, w.viewer.BindingKeys()...)
	return bindings
}

func (w *editorChatTabWorkspace) EnsureChatTab() {
	for i := 0; i < w.tabBar.Count(); i++ {
		if tab := w.tabBar.ActiveTab(); tab != nil && tab.Path == w.chatTabID {
			return
		}
	}
	w.tabBar.OpenTab(w.chatTabID)
}

func (w *editorChatTabWorkspace) SelectChatTab() {
	w.tabBar.OpenTab(w.chatTabID)
	w.showChat = true
}

func (p *ChatPageModel) Init() tea.Cmd {
	p.rebuildLayout()
	cmds := []tea.Cmd{
		p.chatLayout.Init(),
		p.sidebar.Init(),
		p.editorWorkspace.Init(),
		p.completionDialog.Init(),
		logoTickCmd(),
	}
	if p.infoSidebar != nil {
		// Starts the modified-files preload + history subscription loop that
		// keeps the Modified Files section live.
		cmds = append(cmds, p.infoSidebar.Init())
	}
	if p.width > 0 && p.height > 0 {
		p.tabHeader.SetWidth(p.width)
		cmds = append(cmds, p.layout.SetSize(p.width, p.layoutHeight()))
	}
	return tea.Batch(cmds...)
}

func (p *ChatPageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Keep the chat info sidebar (Plan + Modified Files) in sync even while it is
	// hidden, so its content is ready the moment it becomes visible.
	if cmd := p.routeInfoSidebar(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case logoTickMsg:
		// The logo cycles through the growth glyphs while a run (or one of its
		// subagent runs) is in flight, and rests on the Pando tree icon otherwise.
		p.tabHeader.AdvanceLogo(p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy())
		return p, logoTickCmd()
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.tabHeader.SetWidth(msg.Width)
		// Re-evaluate the info sidebar's visibility/width against the new size so
		// it appears or disappears as the window crosses the width threshold.
		if p.layoutMode == ChatOnly {
			p.rebuildLayout()
		}
		cmds = append(cmds, p.layout.SetSize(msg.Width, p.layoutHeight()))
		return p, tea.Batch(cmds...)
	case chat.ShowSlashCompletionMsg:
		p.showSlashCompletionDialog = true
		// The "/" trigger is detected in the editor and arrives here as a
		// separate message, so (unlike "@") the original key never reaches the
		// dialog. Feed it a synthetic "/" key so the dialog focuses its search
		// field and loads the slash command entries; otherwise the list stays
		// empty and renders "No file matches found".
		slashKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
		dialogModel, dialogCmd := p.slashCompletionDialog.Update(slashKey)
		p.slashCompletionDialog = dialogModel.(dialog.CompletionDialog)
		return p, dialogCmd
	case dialog.CompletionDialogCloseMsg:
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
	case filetree.FileSelectedMsg:
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
		p.focus = focusEditor
		cmds = append(cmds, p.applyLayoutMode(SidebarEditor), p.editorWorkspace.OpenFile(msg.Path), p.ensureLSPCmd(msg.Path))
		return p, tea.Batch(cmds...)
	case editor.OpenEditableFileMsg:
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
		p.focus = focusEditor
		cmds = append(cmds, p.applyLayoutMode(SidebarEditor), p.editorWorkspace.OpenEditableFile(msg.Path), p.ensureLSPCmd(msg.Path))
		return p, tea.Batch(cmds...)
	case editor.CloseViewerMsg:
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
		p.focus = focusFileTree
		return p, p.applyLayoutMode(SidebarChat)
	case chat.EditorHeightChangedMsg:
		_, totalH := p.chatLayout.GetSize()
		// editorContainer has a 1-row top border, so container height = Lines + 1.
		fixedH := msg.Lines + 1
		// Always leave at least 3 rows for the messages area.
		if totalH > 0 && fixedH > totalH-3 {
			if totalH-3 > 2 {
				fixedH = totalH - 3
			} else {
				fixedH = 2
			}
		}
		return p, p.chatLayout.SetFixedBottomHeight(fixedH)
	case chat.SendMsg:
		cmd := p.sendMessage(msg.Text, msg.Attachments)
		if cmd != nil {
			return p, cmd
		}
	case dialog.CommandRunCustomMsg:
		if p.app.CoderAgent.IsBusy() {
			return p, util.ReportWarn("Agent is busy, please wait before executing a command...")
		}

		content := msg.Content
		if msg.Args != nil {
			for name, value := range msg.Args {
				content = strings.ReplaceAll(content, "$"+name, value)
			}
		}

		cmd := p.sendMessage(content, nil)
		if cmd != nil {
			return p, cmd
		}
	case chat.ChatSidebarConfigChangedMsg:
		// The info sidebar config changed (mode or min width); rebuild so it
		// appears or disappears immediately while in the Chat tab.
		if p.layoutMode == ChatOnly {
			p.rebuildLayout()
			cmds = append(cmds, p.layout.SetSize(p.width, p.layoutHeight()))
		}
		return p, tea.Batch(cmds...)
	case chat.SessionSelectedMsg:
		p.session = msg
		if goalMsg, err := p.loadGoalUpdate(msg.ID, false); err != nil {
			return p, util.ReportError(err)
		} else {
			p.routeGoalUpdate(goalMsg, &cmds)
		}
	case chat.GoalUpdatedMsg:
		if msg.SessionID == p.session.ID {
			if cmd := p.applyGoalUpdate(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, p.routeMessage(msg)...)
		return p, tea.Batch(cmds...)
	case goalEventChanMsg:
		// Ignore events from a stale channel (e.g. a previous goal).
		if msg.ch != p.goalEventCh {
			return p, nil
		}
		if msg.ok {
			// Keep pumping the channel. Live progress arrives via the agent
			// pubsub stream; we only need to detect the channel closing.
			return p, p.waitForGoalEvent(msg.sessionID, msg.ch)
		}
		// Channel closed: the goal loop finished. Reconcile the terminal
		// state, which is otherwise never pushed to the TUI, so the input is
		// re-enabled and HasRunningGoal() reflects reality again.
		p.goalEventCh = nil
		p.clearGoalExecution(msg.sessionID)
		if goalMsg, err := p.loadGoalUpdate(msg.sessionID, false); err != nil {
			return p, util.ReportError(err)
		} else {
			p.routeGoalUpdate(goalMsg, &cmds)
		}
		return p, tea.Batch(cmds...)
	case pubsub.Event[agentpkg.AgentEvent]:
		if msg.Payload.SessionID == p.session.ID && msg.Payload.Type != agentpkg.AgentEventTypeContentDelta && msg.Payload.Type != agentpkg.AgentEventTypeThinkingDelta && msg.Payload.Type != agentpkg.AgentEventTypeTokenUsage {
			if goalMsg, err := p.loadGoalUpdate(msg.Payload.SessionID, false); err != nil {
				return p, util.ReportError(err)
			} else {
				p.routeGoalUpdate(goalMsg, &cmds)
			}
		}
	case tea.KeyMsg:
		handled, cmd := p.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if handled {
			return p, tea.Batch(cmds...)
		}
	}

	if p.showCompletionDialog {
		dialogModel, dialogCmd := p.completionDialog.Update(msg)
		p.completionDialog = dialogModel.(dialog.CompletionDialog)
		if dialogCmd != nil {
			cmds = append(cmds, dialogCmd)
		}

		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
			return p, tea.Batch(cmds...)
		}
	}

	if p.showSlashCompletionDialog {
		dialogModel, dialogCmd := p.slashCompletionDialog.Update(msg)
		p.slashCompletionDialog = dialogModel.(dialog.CompletionDialog)
		if dialogCmd != nil {
			cmds = append(cmds, dialogCmd)
		}

		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
			return p, tea.Batch(cmds...)
		}
	}

	cmds = append(cmds, p.routeMessage(msg)...)
	return p, tea.Batch(cmds...)
}

func (p *ChatPageModel) handleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c" && p.HasRunningGoal():
		return true, p.cancelGoal()
	case key.Matches(msg, keyMap.ToggleEditorChat):
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
		switch p.layoutMode {
		case ChatOnly:
			return true, p.applyLayoutMode(SidebarChat)
		case SidebarChat:
			return true, p.applyLayoutMode(SidebarEditor)
		case SidebarEditor:
			return true, p.applyLayoutMode(EditorChatSplit)
		case EditorChatSplit:
			return true, p.applyLayoutMode(EditorChatTab)
		case EditorChatTab:
			p.focus = focusChat
			return true, p.applyLayoutMode(ChatOnly)
		}
		return true, nil
	case key.Matches(msg, keyMap.ToggleHiddenFiles):
		return true, p.ToggleHiddenFiles()
	case key.Matches(msg, keyMap.ToggleInfoSidebar):
		return true, p.ToggleInfoSidebar()
	case key.Matches(msg, keyMap.SelectTab):
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
		// keys are "alt+1".."alt+3"; the trailing digit selects the tab.
		s := msg.String()
		idx := int(s[len(s)-1] - '1')
		mode, ok := mainTabModeFor(idx)
		if !ok {
			return true, nil
		}
		if mode == ChatOnly {
			p.focus = focusChat
		}
		cmd, _ := p.SelectMainTab(idx)
		return true, cmd
	case key.Matches(msg, keyMap.ToggleSidebar):
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
		switch p.layoutMode {
		case ChatOnly:
			return true, p.applyLayoutMode(SidebarChat)
		case SidebarChat:
			p.focus = focusChat
			return true, p.applyLayoutMode(ChatOnly)
		default:
			return true, nil
		}
	case key.Matches(msg, keyMap.NextPanel):
		if p.layoutMode == ChatOnly {
			return true, nil
		}
		p.showCompletionDialog = false
		p.showSlashCompletionDialog = false
		p.advanceFocus()
		if p.chatHasFocus() {
			return true, util.CmdHandler(chat.FocusChatEditorMsg{})
		}
		return true, util.CmdHandler(chat.BlurChatEditorMsg{})
	case key.Matches(msg, keyMap.ShowCompletionDialog):
		if p.chatHasFocus() {
			p.showCompletionDialog = true
		}
	case key.Matches(msg, keyMap.NewSession):
		p.session = session.Session{}
		return true, util.CmdHandler(chat.SessionClearedMsg{})
	case key.Matches(msg, keyMap.Cancel):
		if p.chatHasFocus() && p.session.ID != "" {
			p.app.CoderAgent.Cancel(p.session.ID)
			return true, nil
		}
	}

	return false, nil
}

func (p *ChatPageModel) routeMessage(msg tea.Msg) []tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch p.activeFocus() {
		case focusFileTree:
			return []tea.Cmd{p.updateFileTree(msg)}
		case focusEditor:
			if p.layoutMode == EditorChatTab && p.chatTabWorkspace != nil {
				return []tea.Cmd{p.updateChatTabWorkspace(msg)}
			}
			return []tea.Cmd{p.updateEditorWorkspace(msg)}
		case focusChatRight:
			return []tea.Cmd{p.updateChatLayout(msg)}
		default:
			return []tea.Cmd{p.updateChatLayout(msg)}
		}
	case tea.MouseMsg:
		// Non-scroll mouse events: broadcast to all panels (e.g. clicks for zone handling)
		if msg.Button != tea.MouseButtonWheelUp && msg.Button != tea.MouseButtonWheelDown &&
			msg.Button != tea.MouseButtonWheelLeft && msg.Button != tea.MouseButtonWheelRight {
			cmds := []tea.Cmd{
				p.updateFileTree(msg),
				p.updateEditorWorkspace(msg),
				p.updateChatLayout(msg),
			}
			if p.layoutMode == EditorChatTab && p.chatTabWorkspace != nil {
				cmds = append(cmds, p.updateChatTabWorkspace(msg))
			}
			return cmds
		}

		// Scroll events: route only to the panel under the mouse pointer
		x := msg.X

		if p.layoutMode == ChatOnly {
			return []tea.Cmd{p.updateChatLayout(msg)}
		}

		ftWidth, _ := p.fileTreePanel.GetSize()
		if x < ftWidth {
			return []tea.Cmd{p.updateFileTree(msg)}
		}

		switch p.layoutMode {
		case SidebarChat:
			return []tea.Cmd{p.updateChatLayout(msg)}
		case SidebarEditor:
			return []tea.Cmd{p.updateEditorWorkspace(msg)}
		case EditorChatSplit:
			edWidth, _ := p.editorPanel.GetSize()
			if x < ftWidth+edWidth {
				return []tea.Cmd{p.updateEditorWorkspace(msg)}
			}
			return []tea.Cmd{p.updateChatLayout(msg)}
		case EditorChatTab:
			if p.chatTabWorkspace != nil {
				return []tea.Cmd{p.updateChatTabWorkspace(msg)}
			}
			return []tea.Cmd{p.updateEditorWorkspace(msg)}
		}
		return nil
	default:
		cmds := []tea.Cmd{
			p.updateFileTree(msg),
			p.updateEditorWorkspace(msg),
			p.updateChatLayout(msg),
		}
		if p.layoutMode == EditorChatTab && p.chatTabWorkspace != nil {
			cmds = append(cmds, p.updateChatTabWorkspace(msg))
		}
		return cmds
	}
}

func (p *ChatPageModel) updateFileTree(msg tea.Msg) tea.Cmd {
	model, cmd := p.fileTree.Update(msg)
	p.fileTree = model.(filetree.Component)
	return cmd
}

// ensureLSPCmd lazily activates the language server matching the given file when
// it is opened in the editor/file-tree. The work runs off the UI goroutine and
// is a no-op when on-demand activation is disabled or the binary is missing.
func (p *ChatPageModel) ensureLSPCmd(path string) tea.Cmd {
	if p.app == nil || path == "" {
		return nil
	}
	app := p.app
	return func() tea.Msg {
		app.EnsureLSPForFile(context.Background(), path)
		return nil
	}
}

func (p *ChatPageModel) updateSidebar(msg tea.Msg) tea.Cmd {
	if p.sidebar == nil {
		return nil
	}
	model, cmd := p.sidebar.Update(msg)
	p.sidebar = model.(layout.SplitPaneLayout)
	return cmd
}

func (p *ChatPageModel) updateEditorWorkspace(msg tea.Msg) tea.Cmd {
	model, cmd := p.editorWorkspace.Update(msg)
	p.editorWorkspace = model.(*editorWorkspace)
	p.viewer = p.editorWorkspace.viewer
	return cmd
}

func (p *ChatPageModel) updateChatLayout(msg tea.Msg) tea.Cmd {
	model, cmd := p.chatLayout.Update(msg)
	p.chatLayout = model.(layout.SplitPaneLayout)
	return cmd
}

func (p *ChatPageModel) updateChatTabWorkspace(msg tea.Msg) tea.Cmd {
	if p.chatTabWorkspace == nil {
		return nil
	}
	model, cmd := p.chatTabWorkspace.Update(msg)
	p.chatTabWorkspace = model.(*editorChatTabWorkspace)
	p.viewer = p.chatTabWorkspace.viewer
	return cmd
}

func (p *ChatPageModel) sendMessage(text string, attachments []message.Attachment) tea.Cmd {
	if cmd, ok := p.handleGoalCommand(text); ok {
		return cmd
	}
	if cmd, ok := p.handleCompactCommand(text); ok {
		return cmd
	}
	if cmd, ok := p.handleDBCompactCommand(text); ok {
		return cmd
	}
	if cmd, ok := p.handlePonytailCommand(text); ok {
		return cmd
	}
	if cmd, ok := p.handleCavemanCommand(text); ok {
		return cmd
	}
	if cmd, ok := p.handleSuperpowersCommand(text); ok {
		return cmd
	}
	if cmd, ok := p.handleLearningCommand(text); ok {
		return cmd
	}
	// /improve-agents-md expands into a full instruction prompt that is then run
	// as a normal agent turn (so it streams, steers, and persists like any other
	// message). Replace the text and fall through to the normal Run/Steer flow.
	if expanded, ok := expandImproveAgentsMdCommand(text); ok {
		text = expanded
	}

	// If a run is already active for this session, route the message as steering
	// feedback: it is queued and injected into the agent loop at the next safe
	// boundary instead of starting a new run or being rejected.
	if p.session.ID != "" && p.app.CoderAgent.IsSessionBusy(p.session.ID) {
		if err := p.app.CoderAgent.Steer(p.session.ID, text, attachments...); err != nil {
			if errors.Is(err, agentpkg.ErrSessionNotBusy) {
				// Race: the run finished between the busy check and Steer. Fall
				// through to a normal Run below.
			} else {
				return util.ReportError(err)
			}
		} else {
			return util.ReportInfo("💬 Feedback queued — it will be injected at the next step.")
		}
	}

	cmds, err := p.ensureSession()
	if err != nil {
		return util.ReportError(err)
	}

	_, err = p.app.CoderAgent.Run(context.Background(), p.session.ID, text, attachments...)
	if err != nil {
		if errors.Is(err, agentpkg.ErrNoModel) {
			return util.CmdHandler(dialog.OpenModelDialogMsg{})
		}
		return util.ReportError(err)
	}
	return tea.Batch(cmds...)
}

func (p *ChatPageModel) SetSize(width, height int) tea.Cmd {
	p.width = width
	p.height = height
	p.tabHeader.SetWidth(width)
	// Re-evaluate the info sidebar's visibility/width against the new size so it
	// appears or disappears as the window crosses the width threshold.
	if p.layoutMode == ChatOnly {
		p.rebuildLayout()
	}
	return p.layout.SetSize(width, p.layoutHeight())
}

// layoutHeight is the height available to the main split layout once the
// workspace tab header row is reserved at the top of the page.
func (p *ChatPageModel) layoutHeight() int {
	h := p.height - mainTabBarHeight
	if h < 0 {
		return 0
	}
	return h
}

func (p *ChatPageModel) GetSize() (int, int) {
	return p.width, p.height
}

func (p *ChatPageModel) View() string {
	layoutView := p.layout.View()

	if (p.showCompletionDialog || p.showSlashCompletionDialog) && p.chatHasFocus() {
		_, layoutHeight := p.layout.GetSize()
		editorWidth, editorHeight := p.editor.GetSize()

		var overlay string
		if p.showSlashCompletionDialog {
			p.slashCompletionDialog.SetWidth(editorWidth)
			overlay = p.slashCompletionDialog.View()
		} else {
			p.completionDialog.SetWidth(editorWidth)
			overlay = p.completionDialog.View()
		}

		layoutView = layout.PlaceOverlay(
			p.editorOverlayX(),
			layoutHeight-editorHeight-lipgloss.Height(overlay),
			overlay,
			layoutView,
			false,
		)
	}

	p.tabHeader.SetWidth(p.width)
	return lipgloss.JoinVertical(lipgloss.Left, p.tabHeader.View(p.layoutMode), layoutView)
}

// SelectMainTab switches the workspace to the layout mode bound to the given
// header tab index. It is used by the app-level mouse handler when a tab is
// clicked. Returns handled=false if the index is out of range.
func (p *ChatPageModel) SelectMainTab(idx int) (tea.Cmd, bool) {
	mode, ok := mainTabModeFor(idx)
	if !ok {
		return nil, false
	}
	if mode == p.layoutMode {
		return nil, true
	}
	return p.applyLayoutMode(mode), true
}

// MainTabCount returns the number of workspace header tabs.
func (p *ChatPageModel) MainTabCount() int {
	return p.tabHeader.Count()
}

// ToggleHiddenFiles flips whether hidden files/directories are shown in the
// file tree, reloads the tree immediately, and persists the new value to the
// config file so it survives restarts.
func (p *ChatPageModel) ToggleHiddenFiles() tea.Cmd {
	cmd := p.fileTree.ToggleHidden()
	show := p.fileTree.ShowHidden()
	var infoCmd tea.Cmd
	if err := config.UpdateShowHiddenFiles(show); err != nil {
		infoCmd = util.ReportError(fmt.Errorf("failed to save hidden-files setting: %w", err))
	} else if show {
		infoCmd = util.ReportInfo("Hidden files: shown")
	} else {
		infoCmd = util.ReportInfo("Hidden files: hidden")
	}
	return tea.Batch(cmd, infoCmd)
}

// ToggleInfoSidebar flips the chat info sidebar between "auto" and "off",
// persists the new value to the config file, and rebuilds the layout so the
// change is visible immediately while in the Chat tab.
func (p *ChatPageModel) ToggleInfoSidebar() tea.Cmd {
	mode := "off"
	if !config.ChatSidebarEnabled() {
		mode = "auto"
	}
	if err := config.UpdateChatSidebar(mode); err != nil {
		return util.ReportError(fmt.Errorf("failed to save info sidebar setting: %w", err))
	}

	var cmds []tea.Cmd
	if p.layoutMode == ChatOnly {
		p.rebuildLayout()
		cmds = append(cmds, p.layout.SetSize(p.width, p.layoutHeight()))
	}
	if mode == "auto" {
		cmds = append(cmds, util.ReportInfo("Info sidebar: auto"))
	} else {
		cmds = append(cmds, util.ReportInfo("Info sidebar: off"))
	}
	return tea.Batch(cmds...)
}

func (p *ChatPageModel) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(keyMap)
	bindings = append(bindings, p.messages.BindingKeys()...)
	bindings = append(bindings, p.editor.BindingKeys()...)
	if p.layoutMode != ChatOnly {
		bindings = append(bindings, p.fileTree.BindingKeys()...)
	}
	switch p.layoutMode {
	case SidebarEditor, EditorChatSplit:
		bindings = append(bindings, p.editorWorkspace.BindingKeys()...)
	case EditorChatTab:
		if p.chatTabWorkspace != nil {
			bindings = append(bindings, p.chatTabWorkspace.BindingKeys()...)
		}
	}
	return bindings
}

func (p *ChatPageModel) FileTree() filetree.Component {
	return p.fileTree
}

func (p *ChatPageModel) Viewer() editor.FileViewerComponent {
	return p.viewer
}

func (p *ChatPageModel) TabBar() *editor.TabBar {
	return p.tabBar
}

func (p *ChatPageModel) LayoutMode() ChatLayoutMode {
	return p.layoutMode
}

// chatInfoSidebarWidth is the target width (in columns) reserved for the info
// sidebar when it is visible.
const chatInfoSidebarWidth = 42

// chatSidebarVisible reports whether the info sidebar should currently be shown.
// It returns false when the sidebar is disabled in config (ChatSidebar = "off"),
// when not in the Chat tab (ChatOnly), when no sidebar component is present, or
// when the terminal is narrower than the configured minimum width threshold.
func (p *ChatPageModel) chatSidebarVisible() bool {
	return config.ChatSidebarEnabled() &&
		p.layoutMode == ChatOnly &&
		p.infoSidebar != nil &&
		p.width >= config.ChatSidebarMinWidth()
}

// chatSidebarRatio returns the left-panel (chat) ratio that reserves roughly
// chatInfoSidebarWidth columns for the sidebar, capping the sidebar width on
// ultra-wide terminals so the chat does not shrink unnecessarily.
func (p *ChatPageModel) chatSidebarRatio() float64 {
	if p.width <= chatInfoSidebarWidth {
		return 0.75
	}
	left := p.width - chatInfoSidebarWidth
	return float64(left) / float64(p.width)
}

// routeInfoSidebar forwards the messages the info sidebar depends on so its
// Plan and Modified Files stay in sync even while the sidebar is hidden.
func (p *ChatPageModel) routeInfoSidebar(msg tea.Msg) tea.Cmd {
	if p.infoSidebar == nil {
		return nil
	}
	switch m := msg.(type) {
	case chat.SessionSelectedMsg, chat.TodosUpdatedMsg, chat.GoalUpdatedMsg,
		pubsub.Event[session.Session], pubsub.Event[history.File]:
		model, cmd := p.infoSidebar.Update(msg)
		p.infoSidebar = model.(chat.ChatInfoSidebar)
		return cmd
	case tea.MouseMsg:
		// Only forward wheel scroll events, and only when the sidebar is visible
		// and the pointer is over its column, so scrolling the chat or file tree
		// never bleeds into the sidebar's own viewport.
		if !p.chatSidebarVisible() {
			return nil
		}
		if m.Button != tea.MouseButtonWheelUp && m.Button != tea.MouseButtonWheelDown {
			return nil
		}
		sidebarStartX := int(float64(p.width) * p.chatSidebarRatio())
		if m.X < sidebarStartX {
			return nil
		}
		model, cmd := p.infoSidebar.Update(msg)
		p.infoSidebar = model.(chat.ChatInfoSidebar)
		return cmd
	}
	return nil
}

func (p *ChatPageModel) applyLayoutMode(mode ChatLayoutMode) tea.Cmd {
	p.layoutMode = mode
	p.normalizeFocus()
	p.rebuildLayout()
	var cmds []tea.Cmd
	if p.width > 0 && p.height > 0 {
		cmds = append(cmds, p.layout.SetSize(p.width, p.layoutHeight()))
	}
	if p.chatHasFocus() {
		cmds = append(cmds, util.CmdHandler(chat.FocusChatEditorMsg{}))
	} else {
		cmds = append(cmds, util.CmdHandler(chat.BlurChatEditorMsg{}))
	}
	return tea.Batch(cmds...)
}

func (p *ChatPageModel) rebuildLayout() {
	switch p.layoutMode {
	case SidebarChat:
		p.layout = layout.NewSplitPane(
			layout.WithLeftPanel(p.fileTreePanel),
			layout.WithRightPanel(p.chatContainer),
			layout.WithRatio(0.25),
		)
	case SidebarEditor:
		p.layout = layout.NewSplitPane(
			layout.WithLeftPanel(p.fileTreePanel),
			layout.WithRightPanel(p.editorPanel),
			layout.WithRatio(0.20),
		)
	case EditorChatSplit:
		innerSplit := layout.NewSplitPane(
			layout.WithLeftPanel(p.editorPanel),
			layout.WithRightPanel(p.chatContainer),
			layout.WithRatio(0.56),
		)
		p.editorChatPanel = layout.NewContainer(innerSplit)
		p.layout = layout.NewSplitPane(
			layout.WithLeftPanel(p.fileTreePanel),
			layout.WithRightPanel(p.editorChatPanel),
			layout.WithRatio(0.20),
		)
	case EditorChatTab:
		chatTabWs := newEditorChatTabWorkspace(p.viewer, p.tabBar, p.chatLayout)
		chatTabWs.EnsureChatTab()
		p.chatTabWorkspace = chatTabWs
		editorChatTabPanel := layout.NewContainer(chatTabWs)
		p.layout = layout.NewSplitPane(
			layout.WithLeftPanel(p.fileTreePanel),
			layout.WithRightPanel(editorChatTabPanel),
			layout.WithRatio(0.20),
		)
	default:
		if p.chatSidebarVisible() {
			p.layout = layout.NewSplitPane(
				layout.WithLeftPanel(p.chatContainer),
				layout.WithRightPanel(p.infoSidebarContainer),
				layout.WithRatio(p.chatSidebarRatio()),
			)
		} else {
			p.layout = layout.NewSplitPane(
				layout.WithLeftPanel(p.chatContainer),
			)
		}
	}
}

func (p *ChatPageModel) normalizeFocus() {
	switch p.layoutMode {
	case ChatOnly:
		p.focus = focusChat
	case SidebarChat:
		if p.focus != focusFileTree {
			p.focus = focusChat
		}
	case SidebarEditor:
		if p.focus != focusFileTree {
			p.focus = focusEditor
		}
	case EditorChatSplit:
		switch p.focus {
		case focusFileTree, focusEditor, focusChatRight:
		default:
			p.focus = focusEditor
		}
	case EditorChatTab:
		if p.focus != focusFileTree {
			p.focus = focusEditor
		}
	}
}

func (p *ChatPageModel) advanceFocus() {
	switch p.layoutMode {
	case SidebarChat:
		if p.activeFocus() == focusFileTree {
			p.focus = focusChat
			return
		}
		p.focus = focusFileTree
	case SidebarEditor:
		if p.activeFocus() == focusFileTree {
			p.focus = focusEditor
			return
		}
		p.focus = focusFileTree
	case EditorChatSplit:
		switch p.activeFocus() {
		case focusFileTree:
			p.focus = focusEditor
		case focusEditor:
			p.focus = focusChatRight
		case focusChatRight:
			p.focus = focusFileTree
		}
	case EditorChatTab:
		if p.activeFocus() == focusFileTree {
			p.focus = focusEditor
			return
		}
		p.focus = focusFileTree
	}
}

func (p *ChatPageModel) activeFocus() panelFocus {
	switch p.layoutMode {
	case SidebarChat:
		if p.focus == focusFileTree {
			return focusFileTree
		}
		return focusChat
	case SidebarEditor:
		if p.focus == focusFileTree {
			return focusFileTree
		}
		return focusEditor
	case EditorChatSplit:
		switch p.focus {
		case focusFileTree:
			return focusFileTree
		case focusChatRight:
			return focusChatRight
		default:
			return focusEditor
		}
	case EditorChatTab:
		if p.focus == focusFileTree {
			return focusFileTree
		}
		return focusEditor
	default:
		return focusChat
	}
}

func (p *ChatPageModel) chatHasFocus() bool {
	f := p.activeFocus()
	return f == focusChat || f == focusChatRight
}

func (p *ChatPageModel) editorOverlayX() int {
	switch p.layoutMode {
	case ChatOnly:
		return 0
	default:
		sidebarWidth, _ := p.fileTreePanel.GetSize()
		return sidebarWidth
	}
}

func (p *ChatPageModel) HasRunningGoal() bool {
	return strings.TrimSpace(p.goalObjectivePending) != "" || (p.goal != nil && p.goal.IsRunning())
}

func (p *ChatPageModel) handleGoalCommand(input string) (tea.Cmd, bool) {
	command, ok := parseGoalCommand(input)
	if !ok {
		return nil, false
	}

	switch command.kind {
	case goalCommandStart:
		if strings.TrimSpace(command.objective) == "" {
			return util.ReportWarn("Usage: /goal <objective>"), true
		}
		return p.startGoal(command.objective), true
	case goalCommandStatus:
		if p.session.ID == "" {
			return util.ReportInfo("No goal is currently tracked for this session."), true
		}
		goalMsg, err := p.loadGoalUpdate(p.session.ID, false)
		if err != nil {
			return util.ReportError(err), true
		}
		return tea.Batch(
			util.CmdHandler(goalMsg),
			util.ReportInfo(formatGoalStatus(goalMsg.Goal)),
		), true
	case goalCommandCancel:
		return p.cancelGoal(), true
	default:
		return nil, false
	}
}

// handlePonytailCommand toggles the per-session ponytail ("lazy senior dev")
// mode in response to /ponytail [lite|full|ultra|off]. No argument defaults to
// full. The mode is applied to the agent's per-session registry and reported as
// a toast; it takes effect on the next turn via prompt injection.
func (p *ChatPageModel) handlePonytailCommand(input string) (tea.Cmd, bool) {
	line := strings.TrimSpace(input)
	if line != "/ponytail" && !strings.HasPrefix(line, "/ponytail ") {
		return nil, false
	}
	arg := strings.TrimSpace(strings.TrimPrefix(line, "/ponytail"))
	if arg == "" {
		arg = "full"
	}
	mode, ok := ponytail.ParseMode(arg)
	if !ok {
		return util.ReportWarn("Unknown ponytail mode: " + arg + ". Use lite|full|ultra|off."), true
	}
	if p.session.ID == "" {
		return util.ReportWarn("Open or start a session first."), true
	}
	agentpkg.SetPonytailMode(p.session.ID, mode)
	if mode.IsActive() {
		return util.ReportInfo("Ponytail mode: " + mode.String() + ". " + ponytail.Description(mode)), true
	}
	return util.ReportInfo("Ponytail mode disabled. Back to normal."), true
}

// handleCavemanCommand handles /caveman [lite|full|ultra] and
// /caveman-finish. No argument defaults to full. Both are synchronous (a toast,
// no agent turn); the level takes effect on the next turn via prompt injection.
func (p *ChatPageModel) handleCavemanCommand(input string) (tea.Cmd, bool) {
	line := strings.TrimSpace(input)
	isFinish := line == "/caveman-finish" || strings.HasPrefix(line, "/caveman-finish ")
	isActivate := line == "/caveman" || strings.HasPrefix(line, "/caveman ")
	if !isFinish && !isActivate {
		return nil, false
	}
	if p.session.ID == "" {
		return util.ReportWarn("Open or start a session first."), true
	}

	if isFinish {
		// The command takes no level: a stray argument is reported, not ignored.
		if strings.TrimSpace(strings.TrimPrefix(line, "/caveman-finish")) != "" {
			return util.ReportWarn(caveman.FinishUsage), true
		}
		agentpkg.SetCavemanMode(p.session.ID, caveman.ModeOff)
		return util.ReportInfo(caveman.DisabledMessage), true
	}

	arg := strings.TrimSpace(strings.TrimPrefix(line, "/caveman"))
	if arg == "" {
		arg = "full"
	}
	mode, ok := caveman.ParseMode(arg)
	if !ok {
		return util.ReportWarn("Unknown caveman level: " + arg + ". Use lite|full|ultra."), true
	}
	agentpkg.SetCavemanMode(p.session.ID, mode)
	return util.ReportInfo(caveman.ActivationMessage(mode)), true
}

// handleSuperpowersCommand handles /superpowers [objective] and
// /superpowers-finish. Activation is synchronous (a toast, no agent turn); the
// finish runs a real closing turn through the agent, which streams into the chat
// like any other message. The mode is cleared inside RunSuperpowersFinish only if
// that turn succeeds, so a cancelled or failed close keeps the workflow on.
func (p *ChatPageModel) handleSuperpowersCommand(input string) (tea.Cmd, bool) {
	line := strings.TrimSpace(input)
	isFinish := line == "/superpowers-finish"
	isActivate := line == "/superpowers" || strings.HasPrefix(line, "/superpowers ")
	if !isFinish && !isActivate {
		return nil, false
	}
	if p.session.ID == "" {
		return util.ReportWarn("Open or start a session first."), true
	}

	if isActivate {
		if agentpkg.SuperpowersMode(p.session.ID) {
			return util.ReportInfo(superpowers.AlreadyActiveMessage), true
		}
		agentpkg.SetSuperpowersMode(p.session.ID, true)
		objective := strings.TrimSpace(strings.TrimPrefix(line, "/superpowers"))
		if objective != "" {
			return util.ReportInfo("Superpowers mode enabled. Objective: " + objective), true
		}
		return util.ReportInfo("Superpowers mode enabled. Plan first, verify always. /superpowers-finish to close."), true
	}

	events, err := agentpkg.RunSuperpowersFinish(context.Background(), p.app.CoderAgent, p.session.ID)
	if err != nil {
		if errors.Is(err, agentpkg.ErrSuperpowersNotActive) {
			return util.ReportInfo(superpowers.NotActiveMessage), true
		}
		return util.ReportError(err), true
	}
	// The chat renders the turn from the pubsub stream, so the returned channel is
	// drained only to let RunSuperpowersFinish observe the terminal event.
	go func() {
		for range events {
		}
	}()
	return util.ReportInfo("Closing the Superpowers workflow: verifying and reporting..."), true
}

// handleLearningCommand handles /learning [focus] and /learning-finish.
// Activation is synchronous (a toast, no agent turn); the finish runs a real
// closing turn through the agent, which streams into the chat like any other
// message. The mode is cleared inside RunLearningFinish only if that turn
// succeeds, so a cancelled or failed close keeps learning on.
func (p *ChatPageModel) handleLearningCommand(input string) (tea.Cmd, bool) {
	line := strings.TrimSpace(input)
	isFinish := line == "/learning-finish"
	isActivate := line == "/learning" || strings.HasPrefix(line, "/learning ")
	if !isFinish && !isActivate {
		return nil, false
	}
	if p.session.ID == "" {
		return util.ReportWarn("Open or start a session first."), true
	}

	if isActivate {
		if agentpkg.LearningMode(p.session.ID) {
			return util.ReportInfo(learning.AlreadyActiveMessage), true
		}
		agentpkg.SetLearningMode(p.session.ID, true)
		focus := strings.TrimSpace(strings.TrimPrefix(line, "/learning"))
		if focus != "" {
			return util.ReportInfo("Learning mode enabled. Focus: " + focus), true
		}
		return util.ReportInfo("Learning mode enabled. Read the KB, document discoveries, ask questions. /learning-finish to close."), true
	}

	events, err := agentpkg.RunLearningFinish(context.Background(), p.app.CoderAgent, p.session.ID)
	if err != nil {
		if errors.Is(err, agentpkg.ErrLearningNotActive) {
			return util.ReportInfo(learning.NotActiveMessage), true
		}
		return util.ReportError(err), true
	}
	// The chat renders the turn from the pubsub stream, so the returned channel is
	// drained only to let RunLearningFinish observe the terminal event.
	go func() {
		for range events {
		}
	}()
	return util.ReportInfo("Closing Learning mode: consolidating what was learned..."), true
}

// expandImproveAgentsMdCommand recognizes the /improve-agents-md slash command
// and returns the full instruction prompt the agent should run. The boolean is
// true when the input was the command (with or without extra guidance), in which
// case the caller substitutes the returned prompt for the raw command text.
func expandImproveAgentsMdCommand(input string) (string, bool) {
	line := strings.TrimSpace(input)
	if line != "/improve-agents-md" && !strings.HasPrefix(line, "/improve-agents-md ") {
		return "", false
	}
	extra := strings.TrimSpace(strings.TrimPrefix(line, "/improve-agents-md"))
	return agentsmd.Prompt(extra), true
}

func (p *ChatPageModel) handleCompactCommand(input string) (tea.Cmd, bool) {
	line := strings.TrimSpace(input)
	if line == "/compact" || line == "/summarize" {
		return util.CmdHandler(chat.CompactSessionMsg{}), true
	}
	return nil, false
}

// handleDBCompactCommand runs a database VACUUM in response to /db-compact. The
// work happens off the UI thread (a VACUUM can take a while) and the outcome is
// reported as an info/error toast.
func (p *ChatPageModel) handleDBCompactCommand(input string) (tea.Cmd, bool) {
	if strings.TrimSpace(input) != "/db-compact" {
		return nil, false
	}
	app := p.app
	report := util.ReportInfo("Compacting database (VACUUM)...")
	work := func() tea.Msg {
		res, err := app.CompactDatabase(context.Background(), false, true)
		if err != nil {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: "Database compaction failed: " + err.Error()}
		}
		return util.InfoMsg{
			Type: util.InfoTypeInfo,
			Msg: fmt.Sprintf("Database compacted (%s). Freed %s (%s → %s).",
				res.Mode, formatChatBytes(res.Freed), formatChatBytes(res.SizeBefore), formatChatBytes(res.SizeAfter)),
		}
	}
	return tea.Batch(report, work), true
}

// formatChatBytes renders a byte count in human-readable units.
func formatChatBytes(n int64) string {
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%s%d B", neg, n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%s%.1f %cB", neg, float64(n)/float64(div), "KMGTPE"[exp])
}

func (p *ChatPageModel) startGoal(objective string) tea.Cmd {
	cmds, err := p.ensureSession()
	if err != nil {
		return util.ReportError(err)
	}

	if activeGoal, err := p.loadGoalUpdate(p.session.ID, true); err != nil {
		return util.ReportError(err)
	} else if activeGoal.Goal != nil && activeGoal.Goal.IsRunning() {
		return tea.Batch(
			util.CmdHandler(activeGoal),
			util.ReportWarn("A goal is already running. Press Ctrl+C to cancel it first."),
		)
	}

	goalCtx, cancel := context.WithCancel(context.Background())
	eventCh, err := p.goalRunner.Run(goalCtx, p.session.ID, objective, agentpkg.GoalOptions{})
	if err != nil {
		cancel()
		if errors.Is(err, agentpkg.ErrNoModel) {
			return util.CmdHandler(dialog.OpenModelDialogMsg{})
		}
		return util.ReportError(err)
	}

	p.goalCancel = cancel
	p.goalCancelSessionID = p.session.ID
	p.goalObjectivePending = objective
	p.goalEventCh = eventCh
	p.app.Permissions.RequireExplicitApprovalSession(p.session.ID)
	if goalAutoApproveEnabled() {
		p.app.Permissions.AutoApproveSession(p.session.ID)
	}

	goalMsg, err := p.loadGoalUpdate(p.session.ID, false)
	if err != nil {
		return util.ReportError(err)
	}

	cmds = append(cmds,
		util.CmdHandler(goalMsg),
		util.ReportInfo(fmt.Sprintf("Goal started: %s", objective)),
		p.waitForGoalEvent(p.session.ID, eventCh),
	)
	return tea.Batch(cmds...)
}

// waitForGoalEvent blocks on the goal runner's event channel and turns each
// event (and the eventual channel close) into a goalEventChanMsg so the Bubble
// Tea loop can react to the goal finishing.
func (p *ChatPageModel) waitForGoalEvent(sessionID string, ch <-chan agentpkg.AgentEvent) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		return goalEventChanMsg{sessionID: sessionID, ch: ch, ok: ok}
	}
}

func (p *ChatPageModel) cancelGoal() tea.Cmd {
	if !p.HasRunningGoal() {
		return util.ReportWarn("No active goal is running.")
	}

	if p.goalCancel != nil && p.goalCancelSessionID == p.session.ID {
		p.goalCancel()
	}

	goal, err := p.cancelPersistedGoal(p.session.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The goal already reached a terminal state but the local view was
			// left stale (no completion event was delivered). Clear it so the
			// input is re-enabled and the next Ctrl+C opens the quit dialog
			// instead of looping on this warning.
			p.goalEventCh = nil
			p.goal = nil
			p.clearGoalExecution(p.session.ID)
			return util.ReportWarn("No active goal is running.")
		}
		return util.ReportError(err)
	}

	return util.CmdHandler(chat.GoalUpdatedMsg{
		SessionID: p.session.ID,
		Goal:      goalStateFromDB(goal),
	})
}

func (p *ChatPageModel) ensureSession() ([]tea.Cmd, error) {
	var cmds []tea.Cmd
	if p.session.ID != "" {
		return cmds, nil
	}

	session, err := p.app.Sessions.Create(context.Background(), "New Session")
	if err != nil {
		return nil, err
	}

	p.session = session
	cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(session)))
	return cmds, nil
}

func (p *ChatPageModel) loadGoalUpdate(sessionID string, activeOnly bool) (chat.GoalUpdatedMsg, error) {
	msg := chat.GoalUpdatedMsg{SessionID: sessionID}
	if sessionID == "" {
		return msg, nil
	}

	var (
		goal db.SessionGoal
		err  error
	)
	if activeOnly {
		goal, err = p.app.DBQuerier.GetActiveGoal(context.Background(), sessionID)
	} else {
		goal, err = p.app.DBQuerier.GetGoalBySession(context.Background(), sessionID)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return msg, nil
		}
		return msg, err
	}

	msg.Goal = goalStateFromDB(goal)
	return msg, nil
}

func (p *ChatPageModel) applyGoalUpdate(msg chat.GoalUpdatedMsg) tea.Cmd {
	if msg.SessionID != p.session.ID {
		return nil
	}

	var cmds []tea.Cmd
	prev := p.goal
	p.goal = msg.Goal
	if p.goal != nil && p.goal.IsRunning() {
		p.goalObjectivePending = ""
	}
	if p.goal == nil || !p.goal.IsRunning() {
		p.clearGoalExecution(msg.SessionID)
		if p.chatHasFocus() {
			cmds = append(cmds, util.CmdHandler(chat.FocusChatEditorMsg{}))
		}
	}

	wasRunning := prev != nil && prev.IsRunning()
	if msg.Goal == nil || !wasRunning || msg.Goal.IsRunning() {
		return tea.Batch(cmds...)
	}

	var statusCmd tea.Cmd
	switch msg.Goal.Status {
	case agentpkg.GoalStatusCompleted:
		statusCmd = util.ReportInfo("Goal completed.")
	case agentpkg.GoalStatusCancelled:
		statusCmd = util.ReportInfo("Goal cancelled.")
	case agentpkg.GoalStatusBlocked:
		if msg.Goal.Progress != "" {
			statusCmd = util.ReportWarn("Goal blocked: " + msg.Goal.Progress)
			break
		}
		statusCmd = util.ReportWarn("Goal blocked.")
	case agentpkg.GoalStatusFailed, agentpkg.GoalStatusTimeout, agentpkg.GoalStatusStalled:
		statusCmd = util.ReportWarn("Goal ended with status: " + msg.Goal.Status)
	default:
		statusCmd = util.ReportWarn("Goal ended with status: " + msg.Goal.Status)
	}
	cmds = append(cmds, statusCmd)
	return tea.Batch(cmds...)
}

func (p *ChatPageModel) routeGoalUpdate(msg chat.GoalUpdatedMsg, cmds *[]tea.Cmd) {
	if msg.SessionID != p.session.ID {
		return
	}

	if cmd := p.applyGoalUpdate(msg); cmd != nil {
		*cmds = append(*cmds, cmd)
	}
	*cmds = append(*cmds,
		p.updateSidebar(msg),
		p.updateEditorWorkspace(msg),
		p.updateChatLayout(msg),
	)
	if p.layoutMode == EditorChatTab && p.chatTabWorkspace != nil {
		*cmds = append(*cmds, p.updateChatTabWorkspace(msg))
	}
}

func (p *ChatPageModel) clearGoalExecution(sessionID string) {
	if sessionID != "" {
		p.app.Permissions.RemoveAutoApproveSession(sessionID)
		p.app.Permissions.RemoveExplicitApprovalSession(sessionID)
	}
	if sessionID == p.session.ID {
		p.goalObjectivePending = ""
	}
	if p.goalCancelSessionID == sessionID {
		p.goalCancel = nil
		p.goalCancelSessionID = ""
	}
}

func goalAutoApproveEnabled() bool {
	cfg := config.Get()
	if cfg == nil {
		return true
	}
	return cfg.Goal.AutoApprove
}

func (p *ChatPageModel) cancelPersistedGoal(sessionID string) (db.SessionGoal, error) {
	goal, err := p.app.DBQuerier.GetActiveGoal(context.Background(), sessionID)
	if err != nil {
		return db.SessionGoal{}, err
	}

	return p.app.DBQuerier.UpdateGoalStatus(context.Background(), db.UpdateGoalStatusParams{
		Status:        agentpkg.GoalStatusCancelled,
		Iteration:     goal.Iteration,
		LastProgress:  goal.LastProgress,
		NextStep:      sql.NullString{},
		BlockedReason: sql.NullString{},
		CompletedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		ID:            goal.ID,
	})
}

func goalStateFromDB(goal db.SessionGoal) *chat.GoalState {
	state := &chat.GoalState{
		Objective:     goal.Objective,
		Status:        goal.Status,
		Iteration:     goal.Iteration,
		MaxIterations: goal.MaxIterations,
		StartedAt:     goal.StartedAt,
	}
	if goal.CompletedAt.Valid {
		state.CompletedAt = goal.CompletedAt.Int64
		state.HasCompletedAt = true
	}
	if goal.LastProgress.Valid {
		state.Progress = goal.LastProgress.String
	}
	if goal.NextStep.Valid {
		state.NextStep = goal.NextStep.String
	}
	if state.Progress == "" && goal.BlockedReason.Valid {
		state.Progress = goal.BlockedReason.String
	}
	return state
}

type goalCommandKind int

const (
	goalCommandNone goalCommandKind = iota
	goalCommandStart
	goalCommandStatus
	goalCommandCancel
)

type goalCommand struct {
	kind      goalCommandKind
	objective string
}

func parseGoalCommand(input string) (goalCommand, bool) {
	line := strings.TrimSpace(input)
	switch {
	case line == "/goal", line == "/goal-status":
		return goalCommand{kind: goalCommandStatus}, true
	case line == "/goal-cancel":
		return goalCommand{kind: goalCommandCancel}, true
	case strings.HasPrefix(line, "/goal "):
		return goalCommand{kind: goalCommandStart, objective: strings.TrimSpace(strings.TrimPrefix(line, "/goal "))}, true
	case strings.HasPrefix(line, "/autopilot "):
		return goalCommand{kind: goalCommandStart, objective: strings.TrimSpace(strings.TrimPrefix(line, "/autopilot "))}, true
	default:
		return goalCommand{}, false
	}
}

func formatGoalStatus(goal *chat.GoalState) string {
	if goal == nil {
		return "No goal is currently tracked for this session."
	}

	status := fmt.Sprintf("Goal: %s (%s)", goal.Objective, goal.Status)
	if goal.MaxIterations > 0 {
		status += fmt.Sprintf(" [%d/%d]", goal.Iteration, goal.MaxIterations)
	}
	return status
}

func NewChatPage(app *app.App) *ChatPageModel {
	// Wire the code-index lookup when remembrances indexing is available so the
	// "@" completion can serve indexed files instantly while the filesystem scan
	// runs in the background.
	var fileLister completions.IndexedFileLister
	if app.Remembrances != nil && app.Remembrances.Code != nil {
		fileLister = app.Remembrances.Code
	}
	cg := completions.NewFileAndFolderContextGroup(fileLister)
	completionDialog := dialog.NewCompletionDialogCmp(cg)
	slashCg := completions.NewSlashCommandsProvider()
	slashCompletionDialog := dialog.NewCompletionDialogCmp(slashCg)

	messagesContainer := layout.NewContainer(
		chat.NewMessagesCmp(app),
		layout.WithPadding(1, 1, 0, 1),
	)
	editorContainer := layout.NewContainer(
		chat.NewEditorCmp(app),
		layout.WithBorder(true, false, false, false),
	)
	chatLayout := layout.NewSplitPane(
		layout.WithLeftPanel(messagesContainer),
		layout.WithBottomPanel(editorContainer),
	)
	chatContainer := layout.NewContainer(chatLayout)

	fileTreeCmp := filetree.New(
		config.WorkingDirectory(),
		filetree.WithShowHidden(showHiddenFilesFromConfig()),
	)
	fileTreeContent := layout.NewContainer(
		fileTreeCmp,
		layout.WithPadding(1, 1, 1, 1),
	)
	goalPanel := layout.NewContainer(
		chat.NewGoalStatusCmp(),
		layout.WithPadding(1, 1, 1, 1),
		layout.WithBorder(true, false, false, false),
	)
	sidebarLayout := layout.NewSplitPane(
		layout.WithLeftPanel(fileTreeContent),
		layout.WithBottomPanel(goalPanel),
		layout.WithVerticalRatio(0.72),
	)
	fileTreePanel := layout.NewContainer(
		sidebarLayout,
		layout.WithBorder(false, true, false, false),
	)

	viewer := editor.NewSmartFileViewer()
	tabBar := editor.NewTabBar()
	editorWorkspace := newEditorWorkspace(viewer, tabBar)
	editorPanel := layout.NewContainer(editorWorkspace)

	infoSidebar := chat.NewChatInfoSidebar(session.Session{}, app.History)
	infoSidebar.SetOrchestrator(app.MesnadaOrchestrator)
	infoSidebarContainer := layout.NewContainer(
		infoSidebar,
		layout.WithBorder(false, false, false, true),
		layout.WithPadding(1, 1, 1, 0),
	)

	page := &ChatPageModel{
		app:                   app,
		layoutMode:            ChatOnly,
		focus:                 focusChat,
		messages:              messagesContainer,
		editor:                editorContainer,
		sidebar:               sidebarLayout,
		chatLayout:            chatLayout,
		chatContainer:         chatContainer,
		fileTree:              fileTreeCmp,
		fileTreePanel:         fileTreePanel,
		viewer:                viewer,
		tabBar:                tabBar,
		tabHeader:             newMainTabBar(),
		editorWorkspace:       editorWorkspace,
		editorPanel:           editorPanel,
		infoSidebar:           infoSidebar,
		infoSidebarContainer:  infoSidebarContainer,
		completionDialog:      completionDialog,
		slashCompletionDialog: slashCompletionDialog,
		goalRunner:            agentpkg.NewGoalRunner(app.CoderAgent, app.DBQuerier),
	}
	page.rebuildLayout()
	return page
}

// showHiddenFilesFromConfig reports the configured file-tree hidden-files
// visibility, defaulting to false (hidden) when config is unavailable.
func showHiddenFilesFromConfig() bool {
	if cfg := config.Get(); cfg != nil {
		return cfg.TUI.ShowHiddenFiles
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
