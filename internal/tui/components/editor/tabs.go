package editor

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

const (
	tabOverflowIndicator = "..."
	defaultTabWidth      = 80
	minTabNameWidth      = 8
	maxTabNameWidth      = 24
)

// Tab represents a file opened in the editor area.
type Tab struct {
	Path       string
	Name       string
	Dirty      bool
	Icon       string
	IsEditable bool // true when the tab should be opened in editable mode
}

// TabBar keeps track of open files and renders the compact one-line tab strip.
type TabBar struct {
	tabs      []Tab
	activeIdx int
	width     int
	scrollOff int
	keyMap    TabKeyMap
}

// TabKeyMap defines the keybindings handled by the tab bar.
type TabKeyMap struct {
	Close key.Binding
	Next  key.Binding
	Prev  key.Binding
}

// NewTabBar creates a new tab bar with default keybindings.
func NewTabBar() *TabBar {
	return &TabBar{
		activeIdx: -1,
		width:     defaultTabWidth,
		keyMap:    defaultTabKeyMap(),
	}
}

// OpenTab opens a new tab or focuses an existing one, returning its index.
func (t *TabBar) OpenTab(path string) int {
	normalized := normalizeTabPath(path)
	if normalized == "" {
		return t.activeIdx
	}

	for idx := range t.tabs {
		if t.tabs[idx].Path == normalized {
			t.activeIdx = idx
			t.ensureActiveVisible()
			return idx
		}
	}

	name := filepath.Base(normalized)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = normalized
	}

	t.tabs = append(t.tabs, Tab{
		Path: normalized,
		Name: name,
		Icon: tuistyles.FileIconFor(normalized),
	})
	t.activeIdx = len(t.tabs) - 1
	t.ensureActiveVisible()
	return t.activeIdx
}

// OpenEditableTab opens a new editable tab or focuses an existing one, returning its index.
func (t *TabBar) OpenEditableTab(path string) int {
	normalized := normalizeTabPath(path)
	if normalized == "" {
		return t.activeIdx
	}

	for idx := range t.tabs {
		if t.tabs[idx].Path == normalized {
			t.tabs[idx].IsEditable = true
			t.activeIdx = idx
			t.ensureActiveVisible()
			return idx
		}
	}

	name := filepath.Base(normalized)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = normalized
	}

	t.tabs = append(t.tabs, Tab{
		Path:       normalized,
		Name:       name,
		Icon:       tuistyles.FileIconFor(normalized),
		IsEditable: true,
	})
	t.activeIdx = len(t.tabs) - 1
	t.ensureActiveVisible()
	return t.activeIdx
}

// IsActiveEditable returns true when the active tab is in editable mode.
func (t *TabBar) IsActiveEditable() bool {
	if tab := t.ActiveTab(); tab != nil {
		return tab.IsEditable
	}
	return false
}

// CloseTab closes the tab at index and keeps the active selection in range.
func (t *TabBar) CloseTab(index int) {
	if index < 0 || index >= len(t.tabs) {
		return
	}

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
func (t *TabBar) CloseActiveTab() {
	t.CloseTab(t.activeIdx)
}

// ActiveTab returns the active tab, if any.
func (t *TabBar) ActiveTab() *Tab {
	if t.activeIdx < 0 || t.activeIdx >= len(t.tabs) {
		return nil
	}
	return &t.tabs[t.activeIdx]
}

// ActivePath returns the active tab path or an empty string when none is selected.
func (t *TabBar) ActivePath() string {
	if tab := t.ActiveTab(); tab != nil {
		return tab.Path
	}
	return ""
}

// SetDirty updates the dirty state for the tab identified by path.
func (t *TabBar) SetDirty(path string, dirty bool) {
	normalized := normalizeTabPath(path)
	if normalized == "" {
		return
	}

	for idx := range t.tabs {
		if t.tabs[idx].Path == normalized {
			t.tabs[idx].Dirty = dirty
			return
		}
	}
}

// NextTab focuses the next tab, wrapping around.
func (t *TabBar) NextTab() {
	if len(t.tabs) <= 1 {
		return
	}
	t.activeIdx = (t.activeIdx + 1) % len(t.tabs)
	t.ensureActiveVisible()
}

// PrevTab focuses the previous tab, wrapping around.
func (t *TabBar) PrevTab() {
	if len(t.tabs) <= 1 {
		return
	}
	t.activeIdx = (t.activeIdx - 1 + len(t.tabs)) % len(t.tabs)
	t.ensureActiveVisible()
}

// SetSize stores the available width so the bar can apply overflow rules.
func (t *TabBar) SetSize(width int) {
	t.width = max(width, 0)
	t.ensureActiveVisible()
}

// Update applies tab-specific keybindings.
func (t *TabBar) Update(msg tea.Msg) (*TabBar, tea.Cmd) {
	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		if mouseMsg.Action == tea.MouseActionPress {
			for idx := range t.tabs {
				if !tuizone.InBounds(tuizone.TabID(idx), mouseMsg) {
					continue
				}
				switch mouseMsg.Button {
				case tea.MouseButtonLeft:
					t.activeIdx = idx
					t.ensureActiveVisible()
				case tea.MouseButtonMiddle:
					t.CloseTab(idx)
				}
				return t, nil
			}
		}
		return t, nil
	}

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

// Count returns the number of open tabs.
func (t *TabBar) Count() int {
	return len(t.tabs)
}

// BindingKeys returns the keybindings handled by the tab bar.
func (t *TabBar) BindingKeys() []key.Binding {
	return []key.Binding{t.keyMap.Close, t.keyMap.Next, t.keyMap.Prev}
}

// View renders the one-line tab bar.
func (t *TabBar) View() string {
	if t.width <= 0 {
		return ""
	}

	styles := newTabStyles()
	if len(t.tabs) == 0 {
		return styles.Empty.Width(t.width).MaxWidth(t.width).Render("No files open")
	}

	renderedTabs := t.renderedTabs(styles)
	parts := t.visibleParts(renderedTabs, styles)
	if len(parts) == 0 {
		return styles.Container.Width(t.width).Render("")
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Left, parts...)
	return styles.Container.Width(t.width).MaxWidth(t.width).Render(joined)
}

func (t *TabBar) visibleParts(renderedTabs []string, styles tabStyles) []string {
	overflowWidth := lipgloss.Width(styles.Overflow.Render(tabOverflowIndicator))
	leftOverflow := t.scrollOff > 0
	available := t.width
	if leftOverflow {
		available -= overflowWidth
	}
	if available <= 0 {
		return []string{styles.Container.MaxWidth(t.width).Render(styles.Overflow.Render(tabOverflowIndicator))}
	}

	parts := make([]string, 0, len(renderedTabs)+2)
	if leftOverflow {
		parts = append(parts, styles.Overflow.Render(tabOverflowIndicator))
	}

	used := 0
	shownTabs := 0
	for idx := t.scrollOff; idx < len(renderedTabs); idx++ {
		tabWidth := lipgloss.Width(renderedTabs[idx])
		reserveRight := 0
		if idx < len(renderedTabs)-1 {
			reserveRight = overflowWidth
		}
		if used+tabWidth+reserveRight > available {
			if len(parts) == 0 || (leftOverflow && len(parts) == 1) {
				clippedWidth := max(available-reserveRight, 0)
				if clippedWidth > 0 {
					parts = append(parts, lipgloss.NewStyle().MaxWidth(clippedWidth).Render(renderedTabs[idx]))
					shownTabs++
				}
			}
			break
		}
		parts = append(parts, renderedTabs[idx])
		used += tabWidth
		shownTabs++
	}

	if t.scrollOff+shownTabs < len(renderedTabs) {
		parts = append(parts, styles.Overflow.Render(tabOverflowIndicator))
	}

	return parts
}

func (t *TabBar) renderedTabs(styles tabStyles) []string {
	nameWidth := t.maxNameWidth()
	rendered := make([]string, len(t.tabs))
	for idx, tab := range t.tabs {
		label := truncateRunes(tab.Name, nameWidth)
		parts := make([]string, 0, 4)
		if tab.Icon != "" {
			parts = append(parts, tab.Icon)
		}
		if tab.Dirty {
			parts = append(parts, styles.Dirty.Render("●"))
		}
		parts = append(parts, label)

		content := strings.Join(parts, " ")
		if idx == t.activeIdx {
			rendered[idx] = tuizone.MarkTab(idx, styles.Active.Render(content))
			continue
		}
		rendered[idx] = tuizone.MarkTab(idx, styles.Inactive.Render(content))
	}
	return rendered
}

func (t *TabBar) maxNameWidth() int {
	if t.width <= 0 {
		return minTabNameWidth
	}
	return max(min(t.width/4, maxTabNameWidth), minTabNameWidth)
}

func (t *TabBar) ensureActiveVisible() {
	if len(t.tabs) == 0 || t.activeIdx < 0 {
		t.scrollOff = 0
		return
	}
	if t.activeIdx >= len(t.tabs) {
		t.activeIdx = len(t.tabs) - 1
	}
	if t.width <= 0 {
		t.scrollOff = max(min(t.activeIdx, len(t.tabs)-1), 0)
		return
	}

	styles := newTabStyles()
	rendered := t.renderedTabs(styles)
	if len(rendered) == 0 {
		t.scrollOff = 0
		return
	}
	overflowWidth := lipgloss.Width(styles.Overflow.Render(tabOverflowIndicator))
	if t.scrollOff > t.activeIdx {
		t.scrollOff = t.activeIdx
	}
	if t.scrollOff < 0 {
		t.scrollOff = 0
	}

	for {
		visibleCount := t.visibleTabCount(rendered, overflowWidth)
		if visibleCount == 0 {
			t.scrollOff = min(t.activeIdx, len(t.tabs)-1)
			return
		}
		end := t.scrollOff + visibleCount - 1
		if t.activeIdx >= t.scrollOff && t.activeIdx <= end {
			return
		}
		if t.activeIdx < t.scrollOff {
			t.scrollOff = t.activeIdx
			continue
		}
		if t.scrollOff >= len(t.tabs)-1 {
			return
		}
		t.scrollOff++
	}
}

func (t *TabBar) visibleTabCount(renderedTabs []string, overflowWidth int) int {
	if len(renderedTabs) == 0 || t.width <= 0 || t.scrollOff >= len(renderedTabs) {
		return 0
	}

	available := t.width
	if t.scrollOff > 0 {
		available -= overflowWidth
	}
	if available <= 0 {
		return 0
	}

	used := 0
	count := 0
	for idx := t.scrollOff; idx < len(renderedTabs); idx++ {
		reserveRight := 0
		if idx < len(renderedTabs)-1 {
			reserveRight = overflowWidth
		}
		tabWidth := lipgloss.Width(renderedTabs[idx])
		if used+tabWidth+reserveRight > available {
			break
		}
		used += tabWidth
		count++
	}
	return count
}

func defaultTabKeyMap() TabKeyMap {
	return TabKeyMap{
		Close: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("ctrl+w", "close tab"),
		),
		Next: key.NewBinding(
			key.WithKeys("ctrl+tab", "ctrl+pgdown"),
			key.WithHelp("ctrl+tab", "next tab"),
		),
		Prev: key.NewBinding(
			key.WithKeys("ctrl+shift+tab", "shift+ctrl+tab", "ctrl+pgup"),
			key.WithHelp("ctrl+shift+tab", "prev tab"),
		),
	}
}

type tabStyles struct {
	Container lipgloss.Style
	Active    lipgloss.Style
	Inactive  lipgloss.Style
	Dirty     lipgloss.Style
	Overflow  lipgloss.Style
	Empty     lipgloss.Style
}

func newTabStyles() tabStyles {
	t := tuitheme.CurrentTheme()
	base := tuistyles.BaseStyle()

	return tabStyles{
		Container: base.Background(t.Background()),
		Active: base.
			Background(t.BackgroundSecondary()).
			Foreground(t.TextEmphasized()).
			Bold(true).
			Padding(0, 1),
		Inactive: base.
			Background(t.BackgroundDarker()).
			Foreground(t.TextMuted()).
			Padding(0, 1),
		Dirty: lipgloss.NewStyle().
			Foreground(t.Warning()).
			Bold(true),
		Overflow: base.
			Background(t.Background()).
			Foreground(t.TextMuted()).
			Padding(0, 1),
		Empty: base.
			Background(t.Background()).
			Foreground(t.TextMuted()).
			Padding(0, 1),
	}
}

func normalizeTabPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}
