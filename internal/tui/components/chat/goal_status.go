package chat

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

type goalStatusCmp struct {
	width   int
	height  int
	session session.Session
	goal    *GoalState
	spinner spinner.Model
}

func (m *goalStatusCmp) Init() tea.Cmd {
	if m.goal != nil && m.goal.IsRunning() {
		return m.spinner.Tick
	}
	return nil
}

func (m *goalStatusCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SessionSelectedMsg:
		m.session = msg
	case GoalUpdatedMsg:
		if msg.SessionID != m.session.ID {
			return m, nil
		}
		m.goal = msg.Goal
		if m.goal != nil && m.goal.IsRunning() {
			return m, m.spinner.Tick
		}
		return m, nil
	case spinner.TickMsg:
		if m.goal == nil || !m.goal.IsRunning() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *goalStatusCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	title := baseStyle.Foreground(t.Primary()).Bold(true).Render("Goal")

	lines := []string{title}
	if m.goal == nil {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Render("Idle"))
		return baseStyle.Width(m.width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	badge := goalStatusBadge(m.goal.Status, m.goal.IsRunning(), m.spinner.View(), t)
	lines = append(lines, badge)
	lines = append(lines, baseStyle.Foreground(t.Text()).Render(m.goal.Objective))
	if m.goal.MaxIterations > 0 {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Render(fmt.Sprintf("Iteration %d/%d", m.goal.Iteration, m.goal.MaxIterations)))
	}
	if elapsed := goalElapsed(m.goal); elapsed != "" {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Render("Elapsed "+elapsed))
	}
	if m.goal.Progress != "" {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Render("Progress: "+m.goal.Progress))
	}
	if m.goal.NextStep != "" && m.goal.IsRunning() {
		lines = append(lines, baseStyle.Foreground(t.TextMuted()).Render("Next: "+m.goal.NextStep))
	}

	return baseStyle.Width(m.width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *goalStatusCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

func (m *goalStatusCmp) GetSize() (int, int) {
	return m.width, m.height
}

func NewGoalStatusCmp() tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return &goalStatusCmp{spinner: s}
}

func goalStatusBadge(status string, running bool, spin string, t theme.Theme) string {
	label := status
	color := t.TextMuted()

	switch status {
	case "running":
		color = t.Warning()
	case "completed":
		color = t.Success()
	case "blocked", "failed", "timeout", "stalled":
		color = t.Error()
	case "cancelled":
		color = t.TextMuted()
	}

	if running {
		label = fmt.Sprintf("%s %s", spin, status)
	}

	return styles.Padded().
		Background(color).
		Foreground(t.Background()).
		Bold(true).
		Render(label)
}

func goalElapsed(goal *GoalState) string {
	if goal == nil || goal.StartedAt == 0 {
		return ""
	}

	end := time.Now()
	if goal.HasCompletedAt && goal.CompletedAt > 0 {
		end = time.Unix(goal.CompletedAt, 0)
	}

	elapsed := end.Sub(time.Unix(goal.StartedAt, 0)).Round(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	return elapsed.String()
}
