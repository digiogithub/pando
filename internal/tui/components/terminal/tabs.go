package terminal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
)

const (
	termTabOverflow    = "..."
	termDefaultWidth   = 80
	termMinTabWidth    = 8
	termMaxTabWidth    = 24
)

// TerminalTab represents one terminal session in the tab bar.
type TerminalTab struct {
	Title     string
	Terminal  TerminalComponent
	Running   bool
}

// TerminalTabBar manages multiple terminal tabs.
type TerminalTabBar struct {
	tabs      []TerminalTab
	activeIdx int
	width     int
	scrollOff int
	keyMap    termTabKeyMap
}

type termTabKeyMap struct {
	Close key.Binding
	Next  key.Binding
	Prev  key.Binding
}

func defaultTermTabKeyMap() termTabKeyMap {
	return termTabKeyMap{
		Close: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("ctrl+w", "close terminal tab"),
		),
		Next: key.NewBinding(
			key.WithKeys("ctrl+tab", "ctrl+pgdown"),
			key.WithHelp("ctrl+tab", "next terminal"),
		),
		Prev: key.NewBinding(
			key.WithKeys("ctrl+shift+tab", "shift+ctrl+tab", "ctrl+pgup"),
			key.WithHelp("ctrl+shift+tab", "prev terminal"),
		),
	}
}

// NewTerminalTabBar creates a new tab bar for terminals.
func NewTerminalTabBar() *TerminalTabBar {
	return &TerminalTabBar{
		activeIdx: -1,
		width:     termDefaultWidth,
		keyMap:    defaultTermTabKeyMap(),
	}
}

// OpenTab adds a new terminal tab and returns its index.
func (t *TerminalTabBar) OpenTab(term TerminalComponent) int {
	n := len(t.tabs) + 1
	t.tabs = append(t.tabs, TerminalTab{
		Title:    fmt.Sprintf("Terminal %d", n),
		Terminal: term,
		Running:  true,
	})
	t.activeIdx = len(t.tabs) - 1
	t.ensureActiveVisible()
	return t.activeIdx
}

// CloseTab closes the tab at the given index, cleaning up the terminal.
func (t *TerminalTabBar) CloseTab(index int) {
	if index < 0 || index >= len(t.tabs) {
		return
	}
	_ = t.tabs[index].Terminal.Close()
	t.tabs = append(t.tabs[:index], t.tabs[index+1:]...)

	switch {
	case len(t.tabs) == 0:
		t.activeIdx = -1
		t.scrollOff = 0
		return
	case t.activeIdx > index:
		t.activeIdx--
	case t.activeIdx >= len(t.tabs):
		t.activeIdx = len(t.tabs) - 1
	}
	if t.activeIdx < 0 {
		t.activeIdx = 0
	}
	t.ensureActiveVisible()
}

// CloseActiveTab closes the currently focused tab.
func (t *TerminalTabBar) CloseActiveTab() {
	t.CloseTab(t.activeIdx)
}

// ActiveTerminal returns the active terminal component, or nil if none.
func (t *TerminalTabBar) ActiveTerminal() TerminalComponent {
	if t.activeIdx < 0 || t.activeIdx >= len(t.tabs) {
		return nil
	}
	return t.tabs[t.activeIdx].Terminal
}

// NextTab switches to the next tab.
func (t *TerminalTabBar) NextTab() {
	if len(t.tabs) <= 1 {
		return
	}
	t.activeIdx = (t.activeIdx + 1) % len(t.tabs)
	t.ensureActiveVisible()
}

// PrevTab switches to the previous tab.
func (t *TerminalTabBar) PrevTab() {
	if len(t.tabs) <= 1 {
		return
	}
	t.activeIdx = (t.activeIdx - 1 + len(t.tabs)) % len(t.tabs)
	t.ensureActiveVisible()
}

// Count returns the number of open tabs.
func (t *TerminalTabBar) Count() int {
	return len(t.tabs)
}

// SetWidth sets the available render width.
func (t *TerminalTabBar) SetWidth(width int) {
	t.width = max(width, 0)
	t.ensureActiveVisible()
}

// BindingKeys implements layout.Bindings.
func (t *TerminalTabBar) BindingKeys() []key.Binding {
	return []key.Binding{t.keyMap.Close, t.keyMap.Next, t.keyMap.Prev}
}

// Update handles tab keybindings.
func (t *TerminalTabBar) Update(msg tea.Msg) (*TerminalTabBar, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return t, nil
	}
	switch {
	case key.Matches(keyMsg, t.keyMap.Close):
		t.CloseActiveTab()
	case key.Matches(keyMsg, t.keyMap.Next):
		t.NextTab()
	case key.Matches(keyMsg, t.keyMap.Prev):
		t.PrevTab()
	}
	return t, nil
}

// View renders the one-line tab bar.
func (t *TerminalTabBar) View() string {
	if t.width <= 0 {
		return ""
	}
	styles := newTermTabStyles()
	if len(t.tabs) == 0 {
		return styles.Empty.Width(t.width).MaxWidth(t.width).Render("No terminals open")
	}

	rendered := t.renderedTabs(styles)
	parts := t.visibleParts(rendered, styles)
	if len(parts) == 0 {
		return styles.Container.Width(t.width).Render("")
	}
	joined := lipgloss.JoinHorizontal(lipgloss.Left, parts...)
	return styles.Container.Width(t.width).MaxWidth(t.width).Render(joined)
}

func (t *TerminalTabBar) renderedTabs(styles termTabStyles) []string {
	nameWidth := max(min(t.width/4, termMaxTabWidth), termMinTabWidth)
	rendered := make([]string, len(t.tabs))
	for idx, tab := range t.tabs {
		label := truncateRunes(tab.Title, nameWidth)
		icon := tuistyles.FileIconFor("terminal")
		parts := []string{}
		if icon != "" {
			parts = append(parts, icon)
		}
		if !tab.Running {
			parts = append(parts, "✗")
		}
		parts = append(parts, label)
		content := strings.Join(parts, " ")
		if idx == t.activeIdx {
			rendered[idx] = styles.Active.Render(content)
		} else {
			rendered[idx] = styles.Inactive.Render(content)
		}
	}
	return rendered
}

func (t *TerminalTabBar) visibleParts(renderedTabs []string, styles termTabStyles) []string {
	overflowWidth := lipgloss.Width(styles.Overflow.Render(termTabOverflow))
	leftOverflow := t.scrollOff > 0
	available := t.width
	if leftOverflow {
		available -= overflowWidth
	}
	if available <= 0 {
		return []string{styles.Overflow.Render(termTabOverflow)}
	}

	parts := make([]string, 0, len(renderedTabs)+2)
	if leftOverflow {
		parts = append(parts, styles.Overflow.Render(termTabOverflow))
	}

	used := 0
	shown := 0
	for idx := t.scrollOff; idx < len(renderedTabs); idx++ {
		w := lipgloss.Width(renderedTabs[idx])
		reserve := 0
		if idx < len(renderedTabs)-1 {
			reserve = overflowWidth
		}
		if used+w+reserve > available {
			break
		}
		parts = append(parts, renderedTabs[idx])
		used += w
		shown++
	}
	if t.scrollOff+shown < len(renderedTabs) {
		parts = append(parts, styles.Overflow.Render(termTabOverflow))
	}
	return parts
}

func (t *TerminalTabBar) ensureActiveVisible() {
	if len(t.tabs) == 0 || t.activeIdx < 0 {
		t.scrollOff = 0
		return
	}
	if t.activeIdx >= len(t.tabs) {
		t.activeIdx = len(t.tabs) - 1
	}
	if t.scrollOff > t.activeIdx {
		t.scrollOff = t.activeIdx
	}
}

// --- styles ---

type termTabStyles struct {
	Container lipgloss.Style
	Active    lipgloss.Style
	Inactive  lipgloss.Style
	Overflow  lipgloss.Style
	Empty     lipgloss.Style
}

func newTermTabStyles() termTabStyles {
	th := tuitheme.CurrentTheme()
	base := tuistyles.BaseStyle()
	return termTabStyles{
		Container: base.Background(th.Background()),
		Active: base.
			Background(th.BackgroundSecondary()).
			Foreground(th.TextEmphasized()).
			Bold(true).
			Padding(0, 1),
		Inactive: base.
			Background(th.BackgroundDarker()).
			Foreground(th.TextMuted()).
			Padding(0, 1),
		Overflow: base.
			Background(th.Background()).
			Foreground(th.TextMuted()).
			Padding(0, 1),
		Empty: base.
			Background(th.Background()).
			Foreground(th.TextMuted()).
			Padding(0, 1),
	}
}

// truncateRunes truncates a string to at most maxRunes visible runes.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}
