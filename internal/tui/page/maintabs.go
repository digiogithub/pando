package page

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
	"github.com/digiogithub/pando/internal/version"
)

// mainTabBarHeight is the fixed height, in rows, of the workspace tab header
// rendered at the top of the chat page. The Pando logo and the clickable
// workspace tabs share this single row.
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

// logoAnimFrames are the glyphs cycled in place of the Pando tree icon while the
// agent (or one of its subagents) is working. They spell out the growth of a
// pando colony: root, branch, leaf, grove, dense forest.
var logoAnimFrames = []string{"本", "枝", "葉", "林", "森"}

// logoAnimMinInterval / logoAnimMaxInterval bound the random delay between two
// logo frame swaps while the agent is busy.
const (
	logoAnimMinInterval = 2 * time.Second
	logoAnimMaxInterval = 3 * time.Second
	// logoGlyphWidth is the cell width reserved for the logo glyph, sized for the
	// widest frame (a CJK ideograph).
	logoGlyphWidth = 2
)

// logoTickMsg drives the animated logo. It is re-scheduled on every tick so the
// animation keeps running for as long as the chat page is alive; the frame only
// changes while the agent is actually busy.
type logoTickMsg struct{}

// logoTickCmd schedules the next logo frame swap at a random delay inside
// [logoAnimMinInterval, logoAnimMaxInterval].
func logoTickCmd() tea.Cmd {
	span := logoAnimMaxInterval - logoAnimMinInterval
	delay := logoAnimMinInterval + time.Duration(rand.Int63n(int64(span)+1))
	return tea.Tick(delay, func(time.Time) tea.Msg { return logoTickMsg{} })
}

// mainTabBar renders the clickable workspace tab header on the chat page.
type mainTabBar struct {
	width int
	// logoFrame is the index into logoAnimFrames currently shown, or -1 when the
	// idle Pando tree icon is displayed.
	logoFrame int
}

func newMainTabBar() *mainTabBar {
	return &mainTabBar{logoFrame: -1}
}

// AdvanceLogo moves the logo animation one step. While busy it picks a random
// frame different from the current one; otherwise it restores the idle icon.
func (b *mainTabBar) AdvanceLogo(busy bool) {
	if !busy {
		b.logoFrame = -1
		return
	}
	if len(logoAnimFrames) < 2 {
		b.logoFrame = 0
		return
	}
	if b.logoFrame < 0 || b.logoFrame >= len(logoAnimFrames) {
		b.logoFrame = rand.Intn(len(logoAnimFrames))
		return
	}
	// Draw from the frames other than the current one so every tick is visible.
	next := rand.Intn(len(logoAnimFrames) - 1)
	if next >= b.logoFrame {
		next++
	}
	b.logoFrame = next
}

// logoGlyph returns the glyph to render for the current animation state. The
// animation frames are CJK ideographs (2 cells wide) while the idle icon may be
// a single cell, so the glyph is padded to a fixed width to keep the workspace
// tabs from shifting sideways on every frame swap.
func (b *mainTabBar) logoGlyph() string {
	glyph := tuistyles.PandoIcon
	if b.logoFrame >= 0 && b.logoFrame < len(logoAnimFrames) {
		glyph = logoAnimFrames[b.logoFrame]
	}
	if pad := logoGlyphWidth - lipgloss.Width(glyph); pad > 0 {
		glyph += strings.Repeat(" ", pad)
	}
	return glyph
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
	parts := make([]string, 0, len(mainTabs)+1)
	parts = append(parts, b.logoSegment(th, base))
	for i, t := range mainTabs {
		content := t.icon + " " + t.label
		style := inactive
		if i == activeIdx {
			style = active
		}
		parts = append(parts, tuizone.MarkMainTab(i, style.Render(content)))
	}

	left := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	// Right-align the version, dropping it when the header is too narrow to fit
	// both the logo+tabs and the version on a single row.
	versionText := base.
		Background(th.BackgroundDarker()).
		Foreground(th.TextMuted()).
		Padding(0, 2).
		Render(version.Normalize())

	if gap := b.width - lipgloss.Width(left) - lipgloss.Width(versionText); gap >= 0 {
		filler := base.
			Background(th.BackgroundDarker()).
			Render(strings.Repeat(" ", gap))
		left = lipgloss.JoinHorizontal(lipgloss.Top, left, filler, versionText)
	}

	return container.Render(left)
}

// logoSegment renders the Pando icon and name as the left-most part of the
// header row, sitting inline before the clickable workspace tabs.
func (b *mainTabBar) logoSegment(th tuitheme.Theme, base lipgloss.Style) string {
	return base.
		Background(th.BackgroundDarker()).
		Foreground(th.Text()).
		Bold(true).
		Padding(0, 2).
		Render(fmt.Sprintf("%s %s", b.logoGlyph(), "Pando"))
}
