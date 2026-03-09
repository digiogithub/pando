package page

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/completions"
	"github.com/digiogithub/pando/internal/config"
	agentpkg "github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
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

	layout               layout.SplitPaneLayout
	chatLayout           layout.SplitPaneLayout
	chatContainer        layout.Container
	fileTreePanel        layout.Container
	editorPanel          layout.Container
	editorChatPanel      layout.Container
	messages             layout.Container
	editor               layout.Container
	completionDialog     dialog.CompletionDialog
	showCompletionDialog bool

	chatTabWorkspace *editorChatTabWorkspace

	session    session.Session
	layoutMode ChatLayoutMode
	focus      panelFocus

	fileTree filetree.Component
	viewer   editor.FileViewerComponent
	tabBar   *editor.TabBar

	editorWorkspace *editorWorkspace
}

type ChatKeyMap struct {
	ShowCompletionDialog key.Binding
	NewSession           key.Binding
	Cancel               key.Binding
	ToggleSidebar        key.Binding
	NextPanel            key.Binding
	ToggleEditorChat     key.Binding
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
		p.fileTree.Init(),
		p.editorWorkspace.Init(),
		p.completionDialog.Init(),
	}
	if p.width > 0 && p.height > 0 {
		cmds = append(cmds, p.layout.SetSize(p.width, p.height))
	}
	return tea.Batch(cmds...)
}

func (p *ChatPageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		cmds = append(cmds, p.layout.SetSize(msg.Width, msg.Height))
		return p, tea.Batch(cmds...)
	case dialog.CompletionDialogCloseMsg:
		p.showCompletionDialog = false
	case filetree.FileSelectedMsg:
		p.showCompletionDialog = false
		p.focus = focusEditor
		cmds = append(cmds, p.applyLayoutMode(SidebarEditor), p.editorWorkspace.OpenFile(msg.Path))
		return p, tea.Batch(cmds...)
	case editor.OpenEditableFileMsg:
		p.showCompletionDialog = false
		p.focus = focusEditor
		cmds = append(cmds, p.applyLayoutMode(SidebarEditor), p.editorWorkspace.OpenEditableFile(msg.Path))
		return p, tea.Batch(cmds...)
	case editor.CloseViewerMsg:
		p.showCompletionDialog = false
		p.focus = focusFileTree
		return p, p.applyLayoutMode(SidebarChat)
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
	case chat.SessionSelectedMsg:
		p.session = msg
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

	cmds = append(cmds, p.routeMessage(msg)...)
	return p, tea.Batch(cmds...)
}

func (p *ChatPageModel) handleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case key.Matches(msg, keyMap.ToggleEditorChat):
		p.showCompletionDialog = false
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
	case key.Matches(msg, keyMap.ToggleSidebar):
		p.showCompletionDialog = false
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
	var cmds []tea.Cmd
	if p.session.ID == "" {
		session, err := p.app.Sessions.Create(context.Background(), "New Session")
		if err != nil {
			return util.ReportError(err)
		}

		p.session = session
		cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(session)))
	}

	_, err := p.app.CoderAgent.Run(context.Background(), p.session.ID, text, attachments...)
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
	return p.layout.SetSize(width, height)
}

func (p *ChatPageModel) GetSize() (int, int) {
	return p.width, p.height
}

func (p *ChatPageModel) View() string {
	layoutView := p.layout.View()

	if p.showCompletionDialog && p.chatHasFocus() {
		_, layoutHeight := p.layout.GetSize()
		editorWidth, editorHeight := p.editor.GetSize()

		p.completionDialog.SetWidth(editorWidth)
		overlay := p.completionDialog.View()

		layoutView = layout.PlaceOverlay(
			p.editorOverlayX(),
			layoutHeight-editorHeight-lipgloss.Height(overlay),
			overlay,
			layoutView,
			false,
		)
	}

	return layoutView
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

func (p *ChatPageModel) applyLayoutMode(mode ChatLayoutMode) tea.Cmd {
	p.layoutMode = mode
	p.normalizeFocus()
	p.rebuildLayout()
	var cmds []tea.Cmd
	if p.width > 0 && p.height > 0 {
		cmds = append(cmds, p.layout.SetSize(p.width, p.height))
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
		p.layout = layout.NewSplitPane(
			layout.WithLeftPanel(p.chatContainer),
		)
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

func NewChatPage(app *app.App) *ChatPageModel {
	cg := completions.NewFileAndFolderContextGroup()
	completionDialog := dialog.NewCompletionDialogCmp(cg)

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

	fileTreeCmp := filetree.New(config.WorkingDirectory())
	fileTreePanel := layout.NewContainer(
		fileTreeCmp,
		layout.WithPadding(1, 1, 1, 1),
		layout.WithBorder(false, true, false, false),
	)

	viewer := editor.NewFileViewer()
	tabBar := editor.NewTabBar()
	editorWorkspace := newEditorWorkspace(viewer, tabBar)
	editorPanel := layout.NewContainer(editorWorkspace)

	page := &ChatPageModel{
		app:              app,
		layoutMode:       ChatOnly,
		focus:            focusChat,
		messages:         messagesContainer,
		editor:           editorContainer,
		chatLayout:       chatLayout,
		chatContainer:    chatContainer,
		fileTree:         fileTreeCmp,
		fileTreePanel:    fileTreePanel,
		viewer:           viewer,
		tabBar:           tabBar,
		editorWorkspace:  editorWorkspace,
		editorPanel:      editorPanel,
		completionDialog: completionDialog,
	}
	page.rebuildLayout()
	return page
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
