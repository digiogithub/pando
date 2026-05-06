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

// View renders the full three-panel instances browser.
func (m Model) View() string {
	t := theme.CurrentTheme()

	// Minimum dimensions guard.
	if m.width < 20 || m.height < 6 {
		return "Instances browser"
	}

	// Column split: left 30% / right 70%
	leftWidth := m.width * 30 / 100
	rightWidth := m.width - leftWidth - 1 // -1 for separator

	// Row split for right column: sessions 40% / liveview 60%
	sessionsHeight := m.height * 40 / 100
	liveHeight := m.height - sessionsHeight - 1 // -1 for separator

	// Header bar
	header := renderHeader(t, m.width, len(m.instances))

	// Left pane: instances list
	leftContent := renderInstancesPane(t, m.instances, m.selectedInst, m.activePane == paneInstances, leftWidth, m.height-2)

	// Top-right pane: sessions list
	var instPath, instRole string
	if entry := m.selectedInstanceEntry(); entry != nil {
		instPath = truncatePath(entry.Path, rightWidth-20)
		if entry.IsPrimary {
			instRole = "PRIMARY"
		} else {
			instRole = "2nd"
		}
	}
	topRightContent := renderSessionsPane(t, m.sessions, m.selectedSession, m.activePane == paneSessions, rightWidth, sessionsHeight, instPath, instRole)

	// Bottom-right pane: live view
	var sessTitle string
	if sess := m.selectedSessionEntry(); sess != nil {
		sessTitle = sess.ID
		if len(sessTitle) > 12 {
			sessTitle = sessTitle[:12] + "..."
		}
	}
	bottomRightContent := renderLiveViewPane(t, m.liveEvents, m.activePane == paneLiveView, rightWidth, liveHeight, sessTitle)

	// Assemble right column
	rightContent := lipgloss.JoinVertical(lipgloss.Left,
		topRightContent,
		strings.Repeat("─", rightWidth),
		bottomRightContent,
	)

	// Assemble main body
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		leftContent,
		"│",
		rightContent,
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

// renderHeader renders the top header bar.
func renderHeader(t theme.Theme, width, count int) string {
	title := fmt.Sprintf("  Instances  [%d running]", count)
	style := lipgloss.NewStyle().
		Width(width).
		Foreground(t.TextEmphasized()).
		Background(t.BackgroundSecondary()).
		Bold(true).
		Padding(0, 1)
	return style.Render(title)
}

// renderInstancesPane renders the left instances list panel.
func renderInstancesPane(
	t theme.Theme,
	instances []*instanceregistry.Entry,
	selected int,
	focused bool,
	width, height int,
) string {
	borderColor := t.BorderDim()
	if focused {
		borderColor = t.BorderFocused()
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Padding(0, 1)

	title := titleStyle.Render("Instances")

	var rows []string
	rows = append(rows, title)

	for i, entry := range instances {
		role := "2nd"
		if entry.IsPrimary {
			role = "PRIMARY"
		}
		path := truncatePath(entry.Path, width-12)
		prefix := "  "
		if i == selected {
			prefix = "▸ "
		}

		line := fmt.Sprintf("%s%-*s %s", prefix, width-len(role)-5, path, role)

		var lineStyle lipgloss.Style
		if i == selected {
			lineStyle = lipgloss.NewStyle().
				Foreground(t.SelectionForeground()).
				Background(t.SelectionBackground())
		} else {
			lineStyle = lipgloss.NewStyle().
				Foreground(t.Text())
		}
		rows = append(rows, lineStyle.Width(width-2).Render(line))
	}

	if len(instances) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Padding(0, 1).
			Render("No instances running"))
	}

	// Pad rows to fill height
	for len(rows) < height {
		rows = append(rows, "")
	}

	content := strings.Join(rows[:min(len(rows), height)], "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(content)
}

// renderSessionsPane renders the top-right sessions list panel.
func renderSessionsPane(
	t theme.Theme,
	sessions []protocol.SessionPayload,
	selected int,
	focused bool,
	width, height int,
	instPath, instRole string,
) string {
	borderColor := t.BorderDim()
	if focused {
		borderColor = t.BorderFocused()
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Padding(0, 1)

	header := fmt.Sprintf("Sessions — %s (%s)", instPath, instRole)
	if instPath == "" {
		header = "Sessions"
	}
	title := titleStyle.Render(header)

	var rows []string
	rows = append(rows, title)

	for i, sess := range sessions {
		prefix := "  "
		if i == selected {
			prefix = "▸ "
		}
		elapsed := relativeTime(sess.UpdatedAt)
		titleStr := sess.Title
		if titleStr == "" {
			titleStr = sess.ID
		}
		maxTitle := width - len(elapsed) - 8
		if len(titleStr) > maxTitle && maxTitle > 3 {
			titleStr = titleStr[:maxTitle-3] + "..."
		}

		line := fmt.Sprintf("%s%-*s %s", prefix, maxTitle, titleStr, elapsed)

		var lineStyle lipgloss.Style
		if i == selected {
			lineStyle = lipgloss.NewStyle().
				Foreground(t.SelectionForeground()).
				Background(t.SelectionBackground())
		} else {
			lineStyle = lipgloss.NewStyle().
				Foreground(t.Text())
		}
		rows = append(rows, lineStyle.Width(width-2).Render(line))
	}

	if len(sessions) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Padding(0, 1).
			Render("No sessions"))
	}

	for len(rows) < height {
		rows = append(rows, "")
	}

	content := strings.Join(rows[:min(len(rows), height)], "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(content)
}

// renderLiveViewPane renders the bottom-right live event stream panel.
func renderLiveViewPane(
	t theme.Theme,
	events []string,
	focused bool,
	width, height int,
	sessTitle string,
) string {
	borderColor := t.BorderDim()
	if focused {
		borderColor = t.BorderFocused()
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Padding(0, 1)

	header := "Live View"
	if sessTitle != "" {
		header = fmt.Sprintf("Live View — %s", sessTitle)
	}
	title := titleStyle.Render(header)

	contentHeight := height - 2 // subtract title row and potential border
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Show last contentHeight lines
	visible := events
	if len(visible) > contentHeight {
		visible = visible[len(visible)-contentHeight:]
	}

	var rows []string
	rows = append(rows, title)

	for _, line := range visible {
		color := t.Text()
		switch {
		case strings.HasPrefix(line, "[Tool]"):
			color = t.Warning()
		case strings.HasPrefix(line, "[LLM]"), strings.HasPrefix(line, "[Asst]"):
			color = t.Info()
		case strings.HasPrefix(line, "[Msg]"):
			color = t.Text()
		case strings.HasPrefix(line, "[Err]"):
			color = t.Error()
		case strings.HasPrefix(line, "[Info]"):
			color = t.Success()
		}

		// Truncate to fit width
		displayLine := line
		if len(displayLine) > width-4 {
			displayLine = displayLine[:width-7] + "..."
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(color).Render(displayLine))
	}

	// Streaming cursor on last line when focused
	if focused && len(events) > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(t.Primary()).Render("▌"))
	}

	if len(events) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Padding(0, 1).
			Render("Select a session and press Enter to watch live events"))
	}

	for len(rows) < height {
		rows = append(rows, "")
	}

	content := strings.Join(rows[:min(len(rows), height)], "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(content)
}

// truncatePath truncates a file path to fit within maxLen characters,
// showing the last components of the path with a leading "...".
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

// relativeTime returns a short relative time string (e.g. "2m ago", "1h ago").
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
