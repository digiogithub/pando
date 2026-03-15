package terminal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
)

const (
	// tabBarHeight is the fixed height of the terminal tab bar row.
	tabBarHeight = 1
	// terminalHeightRatio is the fraction of total screen height for the panel.
	terminalHeightRatio = 0.40
)

// ToggleTerminalMsg is broadcast to show/hide the terminal panel.
type ToggleTerminalMsg struct{}

// TerminalOpenedMsg is sent after a new terminal is successfully opened.
// It carries the init command so the caller can properly update the model's focus state.
type TerminalOpenedMsg struct {
	InitCmd tea.Cmd
}

// TerminalPanel is the bottom-anchored panel containing the tab bar and
// the active terminal's output.
type TerminalPanel struct {
	tabBar    *TerminalTabBar
	visible   bool
	width     int
	height    int // total panel height (tab bar + terminal body)
	focused   bool
}

// NewTerminalPanel creates an empty, hidden terminal panel.
func NewTerminalPanel() *TerminalPanel {
	return &TerminalPanel{
		tabBar: NewTerminalTabBar(),
	}
}

// OpenNewTerminal creates a new terminal session and adds it to the panel.
// It also makes the panel visible if it was hidden.
// The shell is read from config.Shell; falls back to $SHELL / /bin/bash.
func (p *TerminalPanel) OpenNewTerminal() tea.Cmd {
	termH := p.terminalBodyHeight()
	if termH < 2 {
		termH = 2
	}
	termW := p.width
	if termW < 2 {
		termW = 80
	}

	var shellPath string
	var shellArgs []string
	if cfg := config.Get(); cfg != nil {
		shellPath = cfg.Shell.Path
		shellArgs = cfg.Shell.Args
	}

	logging.Info("TerminalPanel.OpenNewTerminal: starting",
		"width", termW, "height", termH,
		"shell", shellPath, "args", shellArgs)

	term, err := New(termW, termH, shellPath, shellArgs)
	if err != nil {
		logging.Error("TerminalPanel.OpenNewTerminal: New() failed", "error", err)
		return nil
	}

	p.tabBar.OpenTab(term)
	p.visible = true
	logging.Info("TerminalPanel.OpenNewTerminal: terminal opened, panel visible", "tab_count", p.tabBar.Count())

	// Initialise the terminal ticker
	return term.Init()
}

// Show makes the panel visible without toggling.
func (p *TerminalPanel) Show() {
	p.visible = true
}

// Toggle hides or shows the panel.
func (p *TerminalPanel) Toggle() {
	p.visible = !p.visible
}

// IsVisible returns whether the panel is currently shown.
func (p *TerminalPanel) IsVisible() bool {
	return p.visible
}

// IsFocused returns whether the terminal panel currently has focus.
func (p *TerminalPanel) IsFocused() bool {
	return p.focused
}

// Focus gives keyboard focus to the active terminal.
func (p *TerminalPanel) Focus() {
	p.focused = true
}

// Blur removes keyboard focus from the terminal panel.
func (p *TerminalPanel) Blur() {
	p.focused = false
}

// HasTerminals returns true when at least one terminal tab is open.
func (p *TerminalPanel) HasTerminals() bool {
	return p.tabBar.Count() > 0
}

// SetSize sets the total available width and the full-screen height so the
// panel can compute its own height as ~40 % of the screen.
// All open terminals are resized, not just the active one.
func (p *TerminalPanel) SetSize(totalWidth, totalHeight int) tea.Cmd {
	p.width = totalWidth
	p.height = int(float64(totalHeight) * terminalHeightRatio)
	if p.height < tabBarHeight+2 {
		p.height = tabBarHeight + 2
	}

	p.tabBar.SetWidth(totalWidth)

	// Resize ALL open terminals so inactive ones have the right size when switched to.
	bodyH := p.terminalBodyHeight()
	var cmds []tea.Cmd
	for i := range p.tabBar.tabs {
		if cmd := p.tabBar.tabs[i].Terminal.SetSize(totalWidth, bodyH); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// PanelHeight returns the height this panel occupies (tab bar + body).
func (p *TerminalPanel) PanelHeight() int {
	if !p.visible {
		return 0
	}
	return p.height
}

// terminalBodyHeight is the height available for terminal output (below tabs).
func (p *TerminalPanel) terminalBodyHeight() int {
	h := p.height - tabBarHeight
	if h < 2 {
		return 2
	}
	return h
}

// Update propagates Bubble Tea messages to the active terminal and the tab bar.
func (p *TerminalPanel) Update(msg tea.Msg) (*TerminalPanel, tea.Cmd) {
	logging.Debug("TerminalPanel.Update", "msg_type", fmt.Sprintf("%T", msg), "focused", p.focused, "visible", p.visible, "tabs", p.tabBar.Count())
	var cmds []tea.Cmd

	// Let the tab bar handle tab-switching / close keybindings only when focused.
	if p.focused {
		newBar, barCmd := p.tabBar.Update(msg)
		p.tabBar = newBar
		if barCmd != nil {
			cmds = append(cmds, barCmd)
		}
	}

	// Propagate message to the active terminal.
	if term := p.tabBar.ActiveTerminal(); term != nil {
		var cmd tea.Cmd
		if p.focused {
			updatedModel, c := term.Update(msg)
			// Update back into the tab slot.
			if idx := p.tabBar.activeIdx; idx >= 0 && idx < len(p.tabBar.tabs) {
				p.tabBar.tabs[idx].Terminal = updatedModel.(TerminalComponent)
				p.tabBar.tabs[idx].Running = updatedModel.(TerminalComponent).IsRunning()
			}
			cmd = c
		} else {
			// Still need tick messages even when unfocused.
			if _, ok := msg.(terminalTickMsg); ok {
				updatedModel, c := term.Update(msg)
				if idx := p.tabBar.activeIdx; idx >= 0 && idx < len(p.tabBar.tabs) {
					p.tabBar.tabs[idx].Terminal = updatedModel.(TerminalComponent)
					p.tabBar.tabs[idx].Running = updatedModel.(TerminalComponent).IsRunning()
				}
				cmd = c
			}
		}
		cmds = append(cmds, cmd)
	}

	return p, tea.Batch(cmds...)
}

// View renders the tab bar and terminal content.
func (p *TerminalPanel) View() string {
	if !p.visible {
		return ""
	}

	th := tuitheme.CurrentTheme()
	base := tuistyles.BaseStyle()

	// Tab bar.
	tabBarView := p.tabBar.View()

	// Separator line between tab bar and terminal body.
	focused := p.focused
	borderColor := th.BorderNormal()
	if focused {
		borderColor = th.BorderFocused()
	}
	separatorChar := "─"
	separatorLine := strings.Repeat(separatorChar, p.width)
	separator := base.
		Width(p.width).
		Foreground(borderColor).
		Render(lipgloss.NewStyle().Width(p.width).Foreground(borderColor).Render(separatorLine))

	// Terminal body.
	bodyH := p.terminalBodyHeight()
	bodyW := p.width
	var bodyView string
	if term := p.tabBar.ActiveTerminal(); term != nil {
		bodyView = term.View()
	} else {
		bodyView = lipgloss.NewStyle().
			Width(bodyW).
			Height(bodyH).
			Background(th.Background()).
			Render("No terminal open")
	}

	// Wrap body in a fixed-height box so the panel always occupies exactly
	// p.height rows.
	bodyStyle := lipgloss.NewStyle().
		Width(bodyW).
		Height(bodyH).
		MaxWidth(bodyW).
		MaxHeight(bodyH).
		Background(th.Background())

	panelStyle := lipgloss.NewStyle().
		Width(p.width).
		Border(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderForeground(borderColor)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		tabBarView,
		separator,
		bodyStyle.Render(bodyView),
	)

	return panelStyle.Render(content)
}

// BindingKeys returns the keybindings handled by the panel / tab bar.
func (p *TerminalPanel) BindingKeys() []key.Binding {
	return p.tabBar.BindingKeys()
}
