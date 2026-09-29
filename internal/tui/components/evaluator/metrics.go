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
	SetWidth(w int)
}

type metricsCmp struct {
	stats *evaluator.Stats
	width int
}

// SetWidth makes long lines wrap inside the given width, so the rendered
// height (which the page uses for layout) matches what the terminal shows.
func (c *metricsCmp) SetWidth(w int) { c.width = w }

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
	lines := []string{row}
	if len(c.stats.Daily) > 0 {
		var evals, judgeCalls, judgeTokens int64
		counts := make([]int64, 0, len(c.stats.Daily))
		for _, d := range c.stats.Daily {
			evals += d.Evaluations
			judgeCalls += d.JudgeCalls
			judgeTokens += d.JudgePromptTokens + d.JudgeCompletionTokens
			counts = append(counts, d.Evaluations)
		}
		lines = append(lines, labelStyle.Render(fmt.Sprintf("Last %dd:", len(c.stats.Daily)))+" "+
			valueStyle.Render(sparkline(counts))+" "+
			valueStyle.Render(fmt.Sprintf("%d evals", evals))+sep+
			labelStyle.Render("Judge:")+" "+valueStyle.Render(fmt.Sprintf("%d calls, %d tokens", judgeCalls, judgeTokens)))
	}
	if c.stats.Problem != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(t.Warning()).Render("! "+c.stats.Problem))
	}
	style := lipgloss.NewStyle().Padding(0, 1).Foreground(t.Text())
	if c.width > 0 {
		style = style.Width(c.width)
	}
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// sparkline renders values as a row of block characters scaled to the maximum.
func sparkline(values []int64) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	var max int64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	out := make([]rune, 0, len(values))
	for _, v := range values {
		if max == 0 || v == 0 {
			out = append(out, blocks[0])
			continue
		}
		idx := int(v * int64(len(blocks)-1) / max)
		out = append(out, blocks[idx])
	}
	return string(out)
}

// NewMetricsCmp creates a new metrics header component.
func NewMetricsCmp(stats *evaluator.Stats) MetricsComponent {
	return &metricsCmp{stats: stats}
}
