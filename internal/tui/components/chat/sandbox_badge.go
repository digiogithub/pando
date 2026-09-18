package chat

import (
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/digiogithub/pando/internal/sandbox"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// sandboxStatusTTL bounds how stale the cached badge may be. The status
// re-resolves the policy (a few stat calls), which is too much to repeat on
// every frame, while a settings toggle should still show up within a moment.
const sandboxStatusTTL = 2 * time.Second

var (
	sandboxStatusMu   sync.Mutex
	sandboxStatusAt   time.Time
	sandboxStatusLast sandbox.Status
)

// SandboxStatus returns sandbox.CurrentStatus(), cached for sandboxStatusTTL.
func SandboxStatus() sandbox.Status {
	sandboxStatusMu.Lock()
	defer sandboxStatusMu.Unlock()
	if sandboxStatusAt.IsZero() || time.Since(sandboxStatusAt) > sandboxStatusTTL {
		sandboxStatusLast = sandbox.CurrentStatus()
		sandboxStatusAt = time.Now()
	}
	return sandboxStatusLast
}

// InvalidateSandboxStatus drops the cached status so the next badge render
// re-resolves it (called after a settings change).
func InvalidateSandboxStatus() {
	sandboxStatusMu.Lock()
	sandboxStatusAt = time.Time{}
	sandboxStatusMu.Unlock()
}

// sandboxStatusColor picks the badge colour: success while enforced, warning
// when on but not enforced on this OS, error when off.
func sandboxStatusColor(s sandbox.Status, t theme.Theme) lipgloss.AdaptiveColor {
	switch {
	case s.Active:
		return t.Success()
	case s.Policy.Enabled():
		return t.Warning()
	default:
		return t.Error()
	}
}

// SandboxFooterBadge renders the compact footer chip: "⛨ <mode>" while
// enforced, "⛨ <mode>!" when not enforced here, "⛨ off" when disabled.
func SandboxFooterBadge() string {
	t := theme.CurrentTheme()
	s := SandboxStatus()
	text := "⛨ " + string(s.Policy.Mode)
	switch {
	case !s.Policy.Enabled():
		text = "⛨ off"
	case !s.Active:
		text += "!"
	}
	return styles.Padded().
		Background(t.BackgroundDarker()).
		Foreground(sandboxStatusColor(s, t)).
		Render(text)
}

// sandboxSection renders the "Sandbox: <label>" row of the chat info sidebar.
func (m *sidebarCmp) sandboxSection() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	s := SandboxStatus()

	key := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Render("Sandbox")
	value := baseStyle.
		Foreground(sandboxStatusColor(s, t)).
		Width(max(0, m.width-lipgloss.Width(key))).
		Render(": " + s.Label())
	return lipgloss.JoinHorizontal(lipgloss.Left, key, value)
}
