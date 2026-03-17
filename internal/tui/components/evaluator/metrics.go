package evaluator

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// MetricsComponent is the public interface for the evaluator metrics header.
type MetricsComponent interface {
	tea.Model
	View() string
}

type metricsCmp struct {
	stats *evaluator.Stats
}

func (c *metricsCmp) Init() tea.Cmd {
	return nil
}

func (c *metricsCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return c, nil
}

func (c *metricsCmp) View() string {
	t := theme.CurrentTheme()

	if c.stats == nil {
		return ""
	}

	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(t.TextMuted())
	valueStyle := lipgloss.NewStyle().Foreground(t.Text()).Bold(true)
	sepStyle := lipgloss.NewStyle().Foreground(t.BorderNormal())

	sep := sepStyle.Render("  |  ")

	statusStr := "Disabled"
	statusStyle := lipgloss.NewStyle().Foreground(t.Error())
	if c.stats.IsEnabled {
		statusStr = "Active"
		statusStyle = lipgloss.NewStyle().Foreground(t.Success())
	}

	parts := []string{
		labelStyle.Render("Evaluations:") + " " + valueStyle.Render(fmt.Sprintf("%d", c.stats.TotalEvaluations)),
		sep,
		labelStyle.Render("Avg Reward:") + " " + valueStyle.Render(fmt.Sprintf("%.2f", c.stats.AvgReward)),
		sep,
		labelStyle.Render("Skills:") + " " + valueStyle.Render(fmt.Sprintf("%d", c.stats.SkillCount)),
		sep,
		labelStyle.Render("Status:") + " " + statusStyle.Render(statusStr),
	}

	row := lipgloss.JoinHorizontal(lipgloss.Left, parts...)
	return lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(t.Text()).
		Render(row)
}

// NewMetricsCmp creates a new metrics header component.
func NewMetricsCmp(stats *evaluator.Stats) MetricsComponent {
	return &metricsCmp{stats: stats}
}
