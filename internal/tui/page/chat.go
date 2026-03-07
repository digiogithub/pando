package page

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/completions"
	"github.com/digiogithub/pando/internal/config"
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
)

type panelFocus int

const (
	focusChat panelFocus = iota
	focusFileTree
	focusEditor
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
	messages             layout.Container
	editor               layout.Container
	completionDialog     dialog.CompletionDialog
	showCompletionDialog bool

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
}

type editorWorkspace struct {
	width  int
	height int

	viewer editor.FileViewerComponent
	tabBar *editor.TabBar
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
}

func newEditorWorkspace(viewer editor.FileViewerComponent, tabBar *editor.TabBar) *editorWorkspace {
	return &editorWorkspace{
		viewer: viewer,
		tabBar: tabBar,
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

	viewerModel, viewerCmd := w.viewer.Update(msg)
	w.viewer = viewerModel.(editor.FileViewerComponent)
	if viewerCmd != nil {
		cmds = append(cmds, viewerCmd)
	}

	activePath := w.tabBar.ActivePath()
	switch {
	case prevCount > 0 && w.tabBar.Count() == 0:
		cmds = append(cmds, util.CmdHandler(editor.CloseViewerMsg{Path: prevActive}))
	case activePath != "" && activePath != prevActive:
		cmds = append(cmds, w.viewer.OpenFile(activePath))
	}

	return w, tea.Batch(cmds...)
}

func (w *editorWorkspace) View() string {
	if w.width <= 0 || w.height <= 0 {
		return ""
	}

	tabView := w.tabBar.View()
	viewHeight := max(w.height-lipgloss.Height(tabView), 0)
	if sizeable, ok := w.viewer.(layout.Sizeable); ok {
		_ = sizeable.SetSize(w.width, viewHeight)
	}

	return lipgloss.JoinVertical(lipgloss.Left, tabView, w.viewer.View())
}

func (w *editorWorkspace) SetSize(width, height int) tea.Cmd {
	w.width = max(width, 0)
	w.height = max(height, 0)
	w.tabBar.SetSize(w.width)
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

func (w *editorWorkspace) HasTabs() bool {
	return w.tabBar.Count() > 0
}

func (w *editorWorkspace) ActivePath() string {
	return w.tabBar.ActivePath()
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
		return true, nil
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
			return []tea.Cmd{p.updateEditorWorkspace(msg)}
		default:
			return []tea.Cmd{p.updateChatLayout(msg)}
		}
	default:
		return []tea.Cmd{
			p.updateFileTree(msg),
			p.updateEditorWorkspace(msg),
			p.updateChatLayout(msg),
		}
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
	if p.layoutMode == SidebarEditor {
		bindings = append(bindings, p.editorWorkspace.BindingKeys()...)
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
	if p.width > 0 && p.height > 0 {
		return p.layout.SetSize(p.width, p.height)
	}
	return nil
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
	default:
		return focusChat
	}
}

func (p *ChatPageModel) chatHasFocus() bool {
	return p.activeFocus() == focusChat
}

func (p *ChatPageModel) editorOverlayX() int {
	if p.layoutMode == ChatOnly {
		return 0
	}
	sidebarWidth, _ := p.fileTreePanel.GetSize()
	return sidebarWidth
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
