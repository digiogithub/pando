package page

import (
	"github.com/charmbracelet/lipgloss"
	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

// mainTabBarHeight is the fixed height, in rows, of the workspace tab header
// rendered at the top of the chat page.
const mainTabBarHeight = 1

// mainTab describes a single clickable top-level workspace tab.
type mainTab struct {
	icon  string
	label string
	mode  ChatLayoutMode
}

// mainTabs are the workspace tabs shown in the header. The first one is the
// default selection. Each tab is a shortcut to a chat-page layout mode that the
// TUI already supports through keybindings.
var mainTabs = []mainTab{
	{icon: tuistyles.ChatIcon, label: "Chat", mode: ChatOnly},
	{icon: tuistyles.EditorIcon, label: "Editor", mode: SidebarEditor},
	{icon: tuistyles.SplitViewIcon, label: "Editor+Chat", mode: EditorChatSplit},
}

// mainTabBar renders the clickable workspace tab header on the chat page.
type mainTabBar struct {
	width int
}

func newMainTabBar() *mainTabBar {
	return &mainTabBar{}
}

// SetWidth sets the available render width.
func (b *mainTabBar) SetWidth(width int) {
	b.width = max(width, 0)
}

// Count returns the number of workspace tabs.
func (b *mainTabBar) Count() int {
	return len(mainTabs)
}

// mainTabIndexFor maps a chat layout mode to the header tab that represents it.
// Several internal modes (toggled via ctrl+b / ctrl+r) collapse onto the same
// tab so the header still highlights a sensible selection.
func mainTabIndexFor(mode ChatLayoutMode) int {
	switch mode {
	case SidebarEditor:
		return 1
	case EditorChatSplit, EditorChatTab:
		return 2
	default: // ChatOnly, SidebarChat
		return 0
	}
}

// mainTabModeFor returns the layout mode a header tab index activates.
func mainTabModeFor(idx int) (ChatLayoutMode, bool) {
	if idx < 0 || idx >= len(mainTabs) {
		return ChatOnly, false
	}
	return mainTabs[idx].mode, true
}

// View renders the one-line tab header, highlighting the tab that corresponds
// to the given active layout mode.
func (b *mainTabBar) View(mode ChatLayoutMode) string {
	if b.width <= 0 {
		return ""
	}

	th := tuitheme.CurrentTheme()
	base := tuistyles.BaseStyle()

	container := base.
		Background(th.BackgroundDarker()).
		Width(b.width).
		MaxWidth(b.width)
	active := base.
		Background(th.Primary()).
		Foreground(th.BadgeText()).
		Bold(true).
		Padding(0, 2)
	inactive := base.
		Background(th.BackgroundDarker()).
		Foreground(th.TextMuted()).
		Padding(0, 2)

	activeIdx := mainTabIndexFor(mode)
	parts := make([]string, len(mainTabs))
	for i, t := range mainTabs {
		content := t.icon + " " + t.label
		style := inactive
		if i == activeIdx {
			style = active
		}
		parts[i] = tuizone.MarkMainTab(i, style.Render(content))
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return container.Render(joined)
}
