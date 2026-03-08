package filetree

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

type Component interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
	SelectedFile() string
}

type FileSelectedMsg struct {
	Path         string
	RelativePath string
}

type Option func(*FileTree)

func WithShowHidden(show bool) Option {
	return func(t *FileTree) {
		t.showHidden = show
	}
}

func WithKeyMap(keys KeyMap) Option {
	return func(t *FileTree) {
		t.keyMap = keys
	}
}

type FileTree struct {
	root         *FileNode
	filteredRoot *FileNode
	flatList     []*FileNode
	cursor       int
	yOffset      int
	width        int
	height       int
	projectPath  string
	filterInput  textinput.Model
	filterMode   bool
	filterQuery  string
	showHidden   bool
	selectedFile string
	gitStatuses  map[string]GitFileStatus
	keyMap       KeyMap
	loading      map[string]bool
	lastErr      error
}

func New(projectPath string, opts ...Option) *FileTree {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		absPath = projectPath
	}

	input := textinput.New()
	input.Prompt = " "
	input.Placeholder = "fuzzy search"
	input.CharLimit = 256
	input.Blur()

	tree := &FileTree{
		root:        NewRootNode(absPath),
		projectPath: absPath,
		filterInput: input,
		gitStatuses: make(map[string]GitFileStatus),
		keyMap:      DefaultKeyMap(),
		loading:     make(map[string]bool),
	}
	for _, opt := range opts {
		opt(tree)
	}
	tree.rebuildFlatList()
	return tree
}

func (t *FileTree) Init() tea.Cmd {
	return tea.Batch(
		LoadFileTree(t.projectPath, LoadOptions{ShowHidden: t.showHidden}),
		LoadGitStatus(t.projectPath),
	)
}

func (t *FileTree) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return t, t.SetSize(msg.Width, msg.Height)
	case FileTreeRefreshMsg:
		if msg.Err != nil {
			t.lastErr = msg.Err
			return t, nil
		}
		if msg.Root != nil {
			t.root = msg.Root
			t.applyStatuses(t.root)
			t.rebuildFlatList()
		}
		return t, nil
	case LoadChildrenMsg:
		delete(t.loading, normalizeTreePath(msg.ParentPath))
		if msg.Err != nil {
			t.lastErr = msg.Err
			return t, nil
		}
		node := t.findNode(normalizeTreePath(msg.ParentPath), t.root)
		if node == nil {
			return t, nil
		}
		node.Children = msg.Children
		node.Loaded = true
		node.SetExpanded(true)
		t.applyStatuses(node)
		t.rebuildFlatList()
		return t, nil
	case GitStatusUpdateMsg:
		if msg.Err != nil {
			t.lastErr = msg.Err
			return t, nil
		}
		t.gitStatuses = msg.Statuses
		t.applyStatuses(t.root)
		t.applyStatuses(t.filteredRoot)
		t.rebuildFlatList()
		return t, nil
	case FilterResultsMsg:
		if strings.TrimSpace(msg.Query) != strings.TrimSpace(t.filterQuery) {
			return t, nil
		}
		if msg.Err != nil {
			t.lastErr = msg.Err
			return t, nil
		}
		t.filteredRoot = msg.Root
		t.applyStatuses(t.filteredRoot)
		t.rebuildFlatList()
		return t, nil
	case tea.KeyMsg:
		if t.filterMode {
			return t.handleFilterInput(msg)
		}
		return t.handleKey(msg)
	case tea.MouseMsg:
		return t.handleMouse(msg)
	}

	return t, nil
}

func (t *FileTree) handleFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, t.keyMap.CancelSearch):
		t.clearFilter()
		return t, nil
	case key.Matches(msg, t.keyMap.Open):
		return t, t.openCurrentNode(true)
	}

	previous := t.filterInput.Value()
	var cmd tea.Cmd
	t.filterInput, cmd = t.filterInput.Update(msg)
	t.filterQuery = strings.TrimSpace(t.filterInput.Value())
	if previous == t.filterInput.Value() {
		return t, cmd
	}
	if t.filterQuery == "" {
		t.filteredRoot = nil
		t.rebuildFlatList()
		return t, cmd
	}
	return t, tea.Batch(cmd, LoadFilteredTree(t.projectPath, t.filterQuery, LoadOptions{ShowHidden: t.showHidden}, t.gitStatuses))
}

func (t *FileTree) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, t.keyMap.Search):
		t.filterMode = true
		t.filterInput.Focus()
		t.filterInput.SetValue(t.filterQuery)
		return t, nil
	case key.Matches(msg, t.keyMap.Refresh):
		return t, tea.Batch(
			LoadFileTree(t.projectPath, LoadOptions{ShowHidden: t.showHidden}),
			LoadGitStatus(t.projectPath),
		)
	case key.Matches(msg, t.keyMap.Up):
		if t.cursor > 0 {
			t.cursor--
			t.ensureCursorVisible()
		}
		return t, nil
	case key.Matches(msg, t.keyMap.Down):
		if t.cursor < len(t.flatList)-1 {
			t.cursor++
			t.ensureCursorVisible()
		}
		return t, nil
	case key.Matches(msg, t.keyMap.Collapse):
		return t, t.collapseCurrentNode()
	case key.Matches(msg, t.keyMap.Expand):
		return t, t.expandCurrentNode()
	case key.Matches(msg, t.keyMap.Open):
		return t, t.openCurrentNode(false)
	default:
		return t, nil
	}
}

func (t *FileTree) collapseCurrentNode() tea.Cmd {
	node := t.currentNode()
	if node == nil {
		return nil
	}
	if node.IsDir && node.IsExpanded {
		node.SetExpanded(false)
		t.rebuildFlatList()
		return nil
	}
	parentPath := normalizeTreePath(filepath.ToSlash(filepath.Dir(filepath.FromSlash(node.Path))))
	if node.Path == "." || (parentPath == "." && node.Depth <= 1) {
		return nil
	}
	parent := t.findNode(parentPath, t.currentRoot())
	if parent == nil {
		parent = t.findNode(parentPath, t.root)
	}
	if parent == nil {
		return nil
	}
	for idx, candidate := range t.flatList {
		if candidate.Path == parent.Path {
			t.cursor = idx
			break
		}
	}
	t.ensureCursorVisible()
	return nil
}

func (t *FileTree) expandCurrentNode() tea.Cmd {
	node := t.currentNode()
	if node == nil {
		return nil
	}
	if !node.IsDir {
		return t.selectFile(node)
	}
	if node.Loaded {
		node.SetExpanded(true)
		t.rebuildFlatList()
		return nil
	}
	if t.loading[node.Path] {
		return nil
	}
	t.loading[node.Path] = true
	return LoadChildren(t.projectPath, node.Path, node.Depth+1, LoadOptions{ShowHidden: t.showHidden}, t.gitStatuses)
}

func (t *FileTree) openCurrentNode(fromFilter bool) tea.Cmd {
	node := t.currentNode()
	if node == nil {
		return nil
	}
	if !node.IsDir {
		return t.selectFile(node)
	}
	if fromFilter {
		return nil
	}
	if node.IsExpanded {
		node.SetExpanded(false)
		t.rebuildFlatList()
		return nil
	}
	return t.expandCurrentNode()
}

func (t *FileTree) selectFile(node *FileNode) tea.Cmd {
	if node == nil || node.IsDir {
		return nil
	}
	selectedPath := filepath.Join(t.projectPath, filepath.FromSlash(node.Path))
	t.selectedFile = selectedPath
	return util.CmdHandler(FileSelectedMsg{Path: selectedPath, RelativePath: node.Path})
}

func (t *FileTree) View() string {
	baseStyle := styles.BaseStyle().Width(max(0, t.width)).Height(max(0, t.height))
	if t.width == 0 || t.height == 0 {
		return ""
	}

	sections := []string{t.renderHeader()}
	if t.filterMode || t.filterQuery != "" {
		sections = append(sections, t.renderFilter())
	}
	sections = append(sections, t.renderBody())
	view := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return baseStyle.Render(view)
}

func (t *FileTree) renderHeader() string {
	th := theme.CurrentTheme()
	title := styles.BaseStyle().
		Foreground(th.Primary()).
		Bold(true).
		Render("Files")

	status := ""
	if t.lastErr != nil {
		status = styles.BaseStyle().Foreground(th.Error()).Render(t.lastErr.Error())
	} else if t.filterQuery != "" {
		status = styles.BaseStyle().Foreground(th.TextMuted()).Render(fmt.Sprintf("%d matches", max(0, len(t.flatList)-1)))
	}
	line := lipgloss.JoinHorizontal(lipgloss.Left, title)
	if status != "" {
		available := max(0, t.width-lipgloss.Width(title)-1)
		status = lipgloss.NewStyle().Width(available).Align(lipgloss.Right).Render(status)
		line = lipgloss.JoinHorizontal(lipgloss.Left, title, status)
	}
	return styles.BaseStyle().Width(t.width).Render(line)
}

func (t *FileTree) renderFilter() string {
	th := theme.CurrentTheme()
	t.filterInput.Width = max(0, t.width-2)
	input := t.filterInput.View()
	return styles.BaseStyle().
		Width(t.width).
		Foreground(th.TextMuted()).
		Render(input)
}

func (t *FileTree) renderBody() string {
	availableHeight := t.bodyHeight()
	if availableHeight <= 0 {
		return ""
	}
	if len(t.flatList) == 0 {
		message := "No files found"
		if t.root != nil && !t.root.Loaded {
			message = "Loading files..."
		}
		return styles.BaseStyle().
			Width(t.width).
			Height(availableHeight).
			Foreground(theme.CurrentTheme().TextMuted()).
			Render(message)
	}

	start := min(t.yOffset, max(0, len(t.flatList)-1))
	end := min(len(t.flatList), start+availableHeight)
	lines := make([]string, 0, availableHeight)
	for idx := start; idx < end; idx++ {
		lines = append(lines, t.renderNode(t.flatList[idx], idx == t.cursor))
	}
	for len(lines) < availableHeight {
		lines = append(lines, styles.BaseStyle().Width(t.width).Render(""))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (t *FileTree) renderNode(node *FileNode, selected bool) string {
	th := theme.CurrentTheme()
	indentDepth := node.Depth
	if indentDepth > 0 {
		indentDepth--
	}
	indent := strings.Repeat("  ", max(0, indentDepth))
	name := node.Name
	if node.IsDir {
		name += "/"
	}
	prefix := node.Icon
	if node.IsDir && t.loading[node.Path] {
		prefix = "…"
	}

	labelStyle := styles.BaseStyle()
	if node.IsDir {
		labelStyle = labelStyle.Foreground(th.TextEmphasized()).Bold(true)
	}
	status := t.renderGitStatus(node.GitStatus)
	content := lipgloss.JoinHorizontal(lipgloss.Left, indent, prefix, " ", labelStyle.Render(name))
	leftWidth := max(0, t.width-lipgloss.Width(status))
	left := lipgloss.NewStyle().Width(leftWidth).MaxWidth(leftWidth).Render(content)
	line := lipgloss.JoinHorizontal(lipgloss.Left, left, status)

	lineStyle := styles.BaseStyle().Width(t.width)
	if selected {
		lineStyle = lineStyle.Background(th.SelectionBackground()).Foreground(th.SelectionForeground()).Bold(true)
	}
	return tuizone.MarkFileTreeItem(node.Path, lineStyle.Render(line))
}

func (t *FileTree) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonLeft:
			for idx, node := range t.flatList {
				if !tuizone.InBounds(tuizone.FileTreeItemID(node.Path), msg) {
					continue
				}
				t.cursor = idx
				t.ensureCursorVisible()
				return t, t.openCurrentNode(false)
			}
		case tea.MouseButtonWheelUp:
			if t.yOffset > 0 {
				t.yOffset--
			}
			if t.cursor > 0 {
				t.cursor = max(t.cursor-1, 0)
			}
			return t, nil
		case tea.MouseButtonWheelDown:
			if maxOffset := max(0, len(t.flatList)-t.bodyHeight()); t.yOffset < maxOffset {
				t.yOffset++
			}
			if t.cursor < len(t.flatList)-1 {
				t.cursor++
			}
			return t, nil
		}
	}

	return t, nil
}

func (t *FileTree) renderGitStatus(status GitFileStatus) string {
	if status == GitStatusClean {
		return ""
	}
	th := theme.CurrentTheme()
	style := styles.BaseStyle().PaddingLeft(1)
	icon := ""
	switch status {
	case GitStatusAdded:
		style = style.Foreground(th.DiffAdded())
		icon = "+"
	case GitStatusDeleted:
		style = style.Foreground(th.DiffRemoved())
		icon = "-"
	case GitStatusModified:
		style = style.Foreground(th.Warning())
		icon = "●"
	case GitStatusUntracked:
		style = style.Foreground(th.TextMuted())
		icon = "?"
	case GitStatusRenamed:
		style = style.Foreground(th.Primary())
		icon = "→"
	}
	return style.Render(icon)
}

func (t *FileTree) SetSize(width, height int) tea.Cmd {
	t.width = width
	t.height = height
	t.ensureCursorVisible()
	return nil
}

func (t *FileTree) GetSize() (int, int) {
	return t.width, t.height
}

func (t *FileTree) BindingKeys() []key.Binding {
	if t.filterMode {
		return append(t.keyMap.ShortHelp(), t.keyMap.CancelSearch)
	}
	return t.keyMap.ShortHelp()
}

func (t *FileTree) SelectedFile() string {
	return t.selectedFile
}

func (t *FileTree) clearFilter() {
	t.filterMode = false
	t.filterQuery = ""
	t.filterInput.SetValue("")
	t.filterInput.Blur()
	t.filteredRoot = nil
	t.rebuildFlatList()
}

func (t *FileTree) currentNode() *FileNode {
	if t.cursor < 0 || t.cursor >= len(t.flatList) {
		return nil
	}
	return t.flatList[t.cursor]
}

func (t *FileTree) currentRoot() *FileNode {
	if t.filterQuery != "" && t.filteredRoot != nil {
		return t.filteredRoot
	}
	return t.root
}

func (t *FileTree) rebuildFlatList() {
	root := t.currentRoot()
	t.flatList = flattenVisible(root)
	if len(t.flatList) == 0 {
		t.cursor = 0
		t.yOffset = 0
		return
	}
	t.cursor = util.Clamp(t.cursor, 0, len(t.flatList)-1)
	t.ensureCursorVisible()
}

func flattenVisible(root *FileNode) []*FileNode {
	if root == nil {
		return nil
	}
	result := []*FileNode{root}
	if !root.IsDir || !root.IsExpanded {
		return result
	}
	for _, child := range root.Children {
		result = append(result, flattenVisible(child)...)
	}
	return result
}

func (t *FileTree) ensureCursorVisible() {
	availableHeight := t.bodyHeight()
	if availableHeight <= 0 {
		t.yOffset = 0
		return
	}
	if t.cursor < t.yOffset {
		t.yOffset = t.cursor
	}
	if t.cursor >= t.yOffset+availableHeight {
		t.yOffset = t.cursor - availableHeight + 1
	}
	maxOffset := max(0, len(t.flatList)-availableHeight)
	t.yOffset = util.Clamp(t.yOffset, 0, maxOffset)
}

func (t *FileTree) bodyHeight() int {
	headerLines := 1
	if t.filterMode || t.filterQuery != "" {
		headerLines++
	}
	return max(0, t.height-headerLines)
}

func (t *FileTree) findNode(path string, root *FileNode) *FileNode {
	if root == nil {
		return nil
	}
	if root.Path == path {
		return root
	}
	for _, child := range root.Children {
		if found := t.findNode(path, child); found != nil {
			return found
		}
	}
	return nil
}

func (t *FileTree) applyStatuses(root *FileNode) {
	if root == nil {
		return
	}
	if status, ok := t.gitStatuses[root.Path]; ok {
		root.GitStatus = status
	} else {
		root.GitStatus = GitStatusClean
	}
	root.Icon = iconForNode(root.Name, root.IsDir, root.IsExpanded)
	for _, child := range root.Children {
		t.applyStatuses(child)
	}
}
