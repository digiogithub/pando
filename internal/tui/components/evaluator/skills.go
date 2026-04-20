package evaluator

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/theme"
)

const maxContentWidth = 50

// SkillsComponent is the public interface for the skill library list.
type SkillsComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type skillsCmp struct {
	skills []evaluator.Skill
	cursor int
	width  int
	height int
}

func (c *skillsCmp) Init() tea.Cmd {
	return nil
}

func (c *skillsCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if c.cursor > 0 {
				c.cursor--
			}
		case "down", "j":
			if c.cursor < len(c.skills)-1 {
				c.cursor++
			}
		}
	}
	return c, nil
}

func (c *skillsCmp) View() string {
	t := theme.CurrentTheme()

	if len(c.skills) == 0 {
		return lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Italic(true).
			Render("No skills learned yet.")
	}

	tagStyle := lipgloss.NewStyle().
		Foreground(t.Secondary()).
		Bold(true)

	rateStyleGood := lipgloss.NewStyle().Foreground(t.Success())
	rateStyleMid := lipgloss.NewStyle().Foreground(t.Warning())
	rateStyleLow := lipgloss.NewStyle().Foreground(t.Error())

	mutedStyle := lipgloss.NewStyle().Foreground(t.TextMuted())
	selectedStyle := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true)

	var rows []string
	availableRows := c.height
	if availableRows <= 0 {
		availableRows = len(c.skills)
	}
	if availableRows < 1 {
		availableRows = 1
	}
	start := 0
	if c.cursor >= availableRows {
		start = c.cursor - availableRows + 1
	}
	end := min(len(c.skills), start+availableRows)
	for i := start; i < end; i++ {
		sk := c.skills[i]
		content := sk.Content
		if len(content) > maxContentWidth {
			content = content[:maxContentWidth-3] + "..."
		}

		rateStr := fmt.Sprintf("%.2f", sk.SuccessRate)
		var rateRendered string
		switch {
		case sk.SuccessRate >= 0.8:
			rateRendered = rateStyleGood.Render(rateStr)
		case sk.SuccessRate >= 0.6:
			rateRendered = rateStyleMid.Render(rateStr)
		default:
			rateRendered = rateStyleLow.Render(rateStr)
		}

		tag := tagStyle.Render(fmt.Sprintf("[%s]", sk.TaskType))
		usage := mutedStyle.Render(fmt.Sprintf("%d×", sk.UsageCount))

		line := fmt.Sprintf("%-12s %s  %-4s  %s", tag, rateRendered, usage, content)

		if i == c.cursor {
			line = selectedStyle.Render(line)
		}

		rows = append(rows, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (c *skillsCmp) GetSize() (int, int) {
	return c.width, c.height
}

func (c *skillsCmp) SetSize(width int, height int) tea.Cmd {
	c.width = width
	c.height = height
	return nil
}

func (c *skillsCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
		key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
	}
}

// NewSkillsCmp creates a new skills list component.
func NewSkillsCmp(skills []evaluator.Skill) SkillsComponent {
	return &skillsCmp{
		skills: skills,
	}
}
