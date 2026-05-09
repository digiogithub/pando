// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package instances

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// View renders the two-panel instances browser.
// Left column: instances list (top) + sessions list (bottom), each scrollable.
// Right column: live chat view with message history + input at bottom.
//
// Height accounting (lipgloss Border adds 2 rows to total rendered height):
//
//	terminal = m.height
//	header   = 1 row
//	panels   = m.height - 1  (leftH)
//	Each panel is rendered with Height(h - 2) so that bordered total == h.
func (m Model) View() string {
	t := theme.CurrentTheme()

	if m.width < 20 || m.height < 6 {
		return "Instances browser"
	}

	// Column widths: left 30% / separator 1 / right rest
	leftWidth := m.width * 30 / 100
	rightWidth := m.width - leftWidth - 1

	// Status for header
	status := ""
	if m.statusLine != "" && time.Now().Before(m.statusExpiry) {
		status = m.statusLine
	}
	header := renderHeader(t, m.width, len(m.instances), status)

	// Total height available below the header row
	leftH := m.height - 1

	// Split left column vertically: instances 40% / sessions 60%
	instH := leftH * 40 / 100
	if instH < 4 {
		instH = 4
	}
	sessH := leftH - instH

	// Left panels (each panel receives its OUTER target height)
	instPanel := renderInstancesPanel(t, m.instances, m.selectedInst, m.scrollInstances,
		m.activePane == paneInstances, leftWidth, instH)
	sessPanel := renderSessionsPanel(t, m.sessions, m.selectedSession, m.scrollSessions,
		m.activePane == paneSessions, leftWidth, sessH)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, instPanel, sessPanel)

	// Right chat panel
	rightPanel := renderChatPanel(t, m.chatViewport.View(), m.msgInput.View(),
		m.activePane == paneChat, rightWidth, leftH)

	// Vertical separator (same height as panels)
	sep := lipgloss.NewStyle().
		Foreground(t.BorderDim()).
		Height(leftH).
		Render("│")

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, sep, rightPanel)

	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

// renderHeader renders the full-width top status bar (always 1 row).
func renderHeader(t theme.Theme, width, count int, status string) string {
	left := fmt.Sprintf("  Instances  [%d running]", count)
	hints := "tab: panel  i: interrupt  s: switch"
	if status != "" {
		hints = status
	}
	gap := width - len(left) - len(hints) - 2
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + hints

	return lipgloss.NewStyle().
		Width(width).
		Foreground(t.TextEmphasized()).
		Background(t.BackgroundSecondary()).
		Bold(true).
		Padding(0, 1).
		Render(line)
}

// renderInstancesPanel renders the top-left instances list.
// height is the OUTER target height (including the border's 2 rows).
func renderInstancesPanel(
	t theme.Theme,
	instances []*instanceregistry.Entry,
	selected, scroll int,
	focused bool,
	width, height int,
) string {
	borderColor := t.BorderDim()
	if focused {
		borderColor = t.BorderFocused()
	}
	bg := t.Background()

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(bg).
		Bold(true).
		Padding(0, 1)
	title := titleStyle.Render("INSTANCES")

	// Inner usable rows: outer - 2 border rows - 1 title row
	innerH := height - 3
	if innerH < 0 {
		innerH = 0
	}
	innerW := width - 2 // inside left+right border

	var rows []string
	rows = append(rows, title)

	visible := instances
	if scroll > 0 && scroll < len(visible) {
		visible = visible[scroll:]
	}

	for i, entry := range visible {
		if i >= innerH {
			break
		}
		absIdx := i + scroll
		ml := modeStr(entry)
		if entry.IsPrimary {
			ml += "*"
		}
		path := truncatePath(entry.Path, innerW-len(ml)-4)
		prefix := "  "
		if absIdx == selected {
			prefix = "▶ "
		}
		line := fmt.Sprintf("%s%-*s %s", prefix, innerW-len(ml)-4, path, ml)

		var ls lipgloss.Style
		if absIdx == selected {
			ls = lipgloss.NewStyle().
				Foreground(t.SelectionForeground()).
				Background(t.SelectionBackground())
		} else {
			ls = lipgloss.NewStyle().Foreground(t.Text()).Background(bg)
		}
		rows = append(rows, ls.Width(innerW).Render(line))
	}

	if len(instances) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(t.TextMuted()).Background(bg).Padding(0, 1).
			Render("No instances running"))
	}

	if remaining := len(instances) - scroll - innerH; remaining > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(t.TextMuted()).Background(bg).
			Render(fmt.Sprintf("  ↓ %d more", remaining)))
	}

	// Pad to fill the inner area
	for len(rows) < height-2 {
		rows = append(rows, "")
	}

	content := strings.Join(rows[:min(len(rows), height-2)], "\n")

	// Height(height-2): lipgloss adds 2 border rows → total output = height
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Background(bg).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		BorderBackground(bg).
		Render(content)
}

// renderSessionsPanel renders the bottom-left sessions list.
// height is the OUTER target height (including the border's 2 rows).
func renderSessionsPanel(
	t theme.Theme,
	sessions []protocol.SessionPayload,
	selected, scroll int,
	focused bool,
	width, height int,
) string {
	borderColor := t.BorderDim()
	if focused {
		borderColor = t.BorderFocused()
	}
	bg := t.Background()

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(bg).
		Bold(true).
		Padding(0, 1)
	title := titleStyle.Render("SESSIONS")

	innerH := height - 3
	if innerH < 0 {
		innerH = 0
	}
	innerW := width - 2

	var rows []string
	rows = append(rows, title)

	visible := sessions
	if scroll > 0 && scroll < len(visible) {
		visible = visible[scroll:]
	}

	for i, sess := range visible {
		if i >= innerH {
			break
		}
		absIdx := i + scroll
		prefix := "  "
		if absIdx == selected {
			prefix = "▶ "
		}
		elapsed := relativeTime(sess.UpdatedAt)
		titleStr := sess.Title
		if titleStr == "" {
			titleStr = sess.ID
		}
		maxTitle := innerW - len(elapsed) - 5
		if len(titleStr) > maxTitle && maxTitle > 3 {
			titleStr = titleStr[:maxTitle-3] + "..."
		}
		line := fmt.Sprintf("%s%-*s %s", prefix, maxTitle, titleStr, elapsed)

		var ls lipgloss.Style
		if absIdx == selected {
			ls = lipgloss.NewStyle().
				Foreground(t.SelectionForeground()).
				Background(t.SelectionBackground())
		} else {
			ls = lipgloss.NewStyle().Foreground(t.Text()).Background(bg)
		}
		rows = append(rows, ls.Width(innerW).Render(line))
	}

	if len(sessions) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(t.TextMuted()).Background(bg).Padding(0, 1).
			Render("No sessions"))
	}

	if remaining := len(sessions) - scroll - innerH; remaining > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(t.TextMuted()).Background(bg).
			Render(fmt.Sprintf("  ↓ %d more", remaining)))
	}

	for len(rows) < height-2 {
		rows = append(rows, "")
	}

	content := strings.Join(rows[:min(len(rows), height-2)], "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Background(bg).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		BorderBackground(bg).
		Render(content)
}

// renderChatPanel renders the right chat panel.
// height is the OUTER target height (including the border's 2 rows).
//
// Inner layout (height - 2 rows inside border):
//
//	1 row  — title
//	N rows — viewport  (height - 2 - 1 - 1 - 1 = height - 5)
//	1 row  — hints
//	1 row  — input bar
func renderChatPanel(
	t theme.Theme,
	viewportContent string,
	inputView string,
	focused bool,
	width, height int,
) string {
	borderColor := t.BorderDim()
	if focused {
		borderColor = t.BorderFocused()
	}
	bg := t.Background()

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(bg).
		Bold(true).
		Padding(0, 1)
	title := titleStyle.Render("CHAT")

	hintsStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(bg)
	hints := hintsStyle.Render("  enter: send  i: interrupt  s: switch  tab: panels  esc: back")

	inputStyle := lipgloss.NewStyle().
		Width(width - 2).
		Foreground(t.Text()).
		Background(t.BackgroundSecondary()).
		Padding(0, 1)
	inputBar := inputStyle.Render(inputView)

	inner := lipgloss.JoinVertical(lipgloss.Left,
		title,
		viewportContent,
		hints,
		inputBar,
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Background(bg).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		BorderBackground(bg).
		Render(inner)
}

// renderChatLines renders all chat lines into a single string for the viewport.
func renderChatLines(t theme.Theme, lines []chatLine, width int) string {
	if len(lines) == 0 {
		return lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Render("\n  Select a session — press Enter to start live chat.\n")
	}

	var sb strings.Builder
	for _, cl := range lines {
		sb.WriteString(renderChatLine(t, cl, width))
		sb.WriteString("\n")
	}
	return sb.String()
}

// renderChatLine renders a single chat line with role-based styling.
func renderChatLine(t theme.Theme, cl chatLine, width int) string {
	bg := t.Background()
	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}

	switch cl.role {
	case "user":
		style := lipgloss.NewStyle().
			Foreground(t.Text()).
			Background(bg).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(t.Secondary()).
			PaddingLeft(1).
			Width(innerW)
		label := lipgloss.NewStyle().Foreground(t.Secondary()).Bold(true).Background(bg).Render("You")
		return style.Render(label + "\n" + wordWrap(cl.content, innerW-2))

	case "assistant":
		style := lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Background(bg).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(t.Primary()).
			PaddingLeft(1).
			Width(innerW)
		label := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true).Background(bg).Render("Assistant")
		return style.Render(label + "\n" + wordWrap(cl.content, innerW-2))

	case "stream":
		style := lipgloss.NewStyle().
			Foreground(t.Info()).
			Background(bg).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(t.Info()).
			PaddingLeft(1).
			Width(innerW)
		label := lipgloss.NewStyle().Foreground(t.Info()).Bold(true).Background(bg).Render("Assistant ▌")
		return style.Render(label + "\n" + wordWrap(cl.content, innerW-2))

	case "tool":
		return lipgloss.NewStyle().
			Foreground(t.Warning()).Background(bg).PaddingLeft(2).
			Width(innerW).Render("⚙ " + cl.content)

	default:
		return lipgloss.NewStyle().
			Foreground(t.TextMuted()).Background(bg).PaddingLeft(2).
			Width(innerW).Render("─ " + cl.content)
	}
}

// wordWrap wraps text at the given width without breaking words.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
		} else if len(cur)+1+len(w) <= width {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// modeStr returns a short display label for the instance mode.
func modeStr(e *instanceregistry.Entry) string {
	switch e.Mode {
	case instanceregistry.ModeTUI:
		return "TUI"
	case instanceregistry.ModeWebUI:
		return "WEB"
	case instanceregistry.ModeDesktop:
		return "DSK"
	case instanceregistry.ModeACP:
		return "ACP"
	case instanceregistry.ModeProxy:
		return "PRX"
	default:
		return "TUI"
	}
}

// truncatePath truncates a file path to fit within maxLen characters.
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen || maxLen < 4 {
		return path
	}
	base := filepath.Base(path)
	if len(base) >= maxLen {
		return base[:maxLen-3] + "..."
	}
	return "..." + path[len(path)-maxLen+3:]
}

// relativeTime returns a compact relative time string.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
