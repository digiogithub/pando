package page

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/evaluator"
	evaluatorcomp "github.com/digiogithub/pando/internal/tui/components/evaluator"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// EvaluatorPageModel is the public interface for the self-improvement page.
type EvaluatorPageModel interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type evaluatorLoadedMsg struct{ stats *evaluator.Stats }
type evaluatorErrMsg struct{ err error }

type evaluatorPage struct {
	evaluatorSvc evaluator.Service
	stats        *evaluator.Stats
	table        evaluatorcomp.TableComponent
	skills       evaluatorcomp.SkillsComponent
	metrics      evaluatorcomp.MetricsComponent
	activePanel  int // 0=table, 1=skills
	loading      bool
	width        int
	height       int
}

func (p *evaluatorPage) Init() tea.Cmd {
	if p.evaluatorSvc == nil {
		return nil
	}
	return p.load()
}

func (p *evaluatorPage) load() tea.Cmd {
	svc := p.evaluatorSvc
	return func() tea.Msg {
		stats, err := svc.GetStats(context.Background())
		if err != nil {
			return evaluatorErrMsg{err: err}
		}
		return evaluatorLoadedMsg{stats: stats}
	}
}

func (p *evaluatorPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			if p.evaluatorSvc != nil {
				p.loading = true
				return p, p.load()
			}
		case "tab", "shift+tab":
			p.activePanel = (p.activePanel + 1) % 2
		case "up", "k", "down", "j":
			if p.table != nil && p.skills != nil {
				if p.activePanel == 0 {
					m, cmd := p.table.Update(msg)
					p.table = m.(evaluatorcomp.TableComponent)
					return p, cmd
				}
				m, cmd := p.skills.Update(msg)
				p.skills = m.(evaluatorcomp.SkillsComponent)
				return p, cmd
			}
		}

	case evaluatorLoadedMsg:
		p.loading = false
		p.stats = msg.stats
		p.table = evaluatorcomp.NewTableCmp(msg.stats.Templates)
		p.skills = evaluatorcomp.NewSkillsCmp(msg.stats.TopSkills)
		p.metrics = evaluatorcomp.NewMetricsCmp(msg.stats)
		if err := p.resizeComponents(); err != nil {
			_ = err
		}

	case evaluatorErrMsg:
		p.loading = false
	}

	return p, nil
}

func (p *evaluatorPage) resizeComponents() error {
	if p.table != nil {
		halfW := p.width / 2
		panelH := p.panelHeight()
		if panelH < 1 {
			panelH = 1
		}
		_ = p.table.SetSize(halfW, panelH)
		_ = p.skills.SetSize(p.width-halfW, panelH)
	}
	return nil
}

func (p *evaluatorPage) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle().Width(p.width).Height(p.height)

	if p.evaluatorSvc == nil || !p.evaluatorSvc.IsEnabled() {
		return baseStyle.Render(renderEvaluatorDisabled())
	}

	if p.loading {
		loadingStyle := lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Italic(true).
			Padding(1, 2)
		return baseStyle.Render(loadingStyle.Render("Loading self-improvement stats..."))
	}

	if p.stats == nil {
		return baseStyle.Render("")
	}

	metricsView := ""
	if p.metrics != nil {
		metricsView = p.metrics.View()
	}

	tableView := ""
	skillsView := ""
	if p.table != nil && p.skills != nil {
		tView := p.table.View()
		sView := p.skills.View()

		// Highlight active panel border
		focusedBorder := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocused())
		normalBorder := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderNormal())

		halfW := p.width / 2
		panelH := p.panelHeight()
		if panelH < 1 {
			panelH = 1
		}

		if p.activePanel == 0 {
			tableView = focusedBorder.Width(halfW - 2).Height(panelH).Render(tView)
			skillsView = normalBorder.Width(p.width - halfW - 2).Height(panelH).Render(sView)
		} else {
			tableView = normalBorder.Width(halfW - 2).Height(panelH).Render(tView)
			skillsView = focusedBorder.Width(p.width - halfW - 2).Height(panelH).Render(sView)
		}
	}

	panels := lipgloss.JoinHorizontal(lipgloss.Top, tableView, skillsView)
	helpBar := renderEvaluatorHelp()

	content := lipgloss.JoinVertical(lipgloss.Left, metricsView, panels, helpBar)
	return baseStyle.Render(content)
}

func (p *evaluatorPage) GetSize() (int, int) {
	return p.width, p.height
}

func (p *evaluatorPage) SetSize(width int, height int) tea.Cmd {
	p.width = width
	p.height = height
	_ = p.resizeComponents()
	return nil
}

func (p *evaluatorPage) panelHeight() int {
	metricsHeight := 0
	if p.metrics != nil {
		metricsHeight = lipgloss.Height(p.metrics.View())
	}
	helpHeight := lipgloss.Height(renderEvaluatorHelp())
	available := p.height - metricsHeight - helpHeight
	if metricsHeight > 0 {
		available--
	}
	if helpHeight > 0 {
		available--
	}
	available -= 2
	if available < 1 {
		return 1
	}
	return available
}

func (p *evaluatorPage) BindingKeys() []key.Binding {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch panel")),
		key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
		key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
	}
	if p.activePanel == 0 && p.table != nil {
		bindings = append(bindings, p.table.BindingKeys()...)
	} else if p.skills != nil {
		bindings = append(bindings, p.skills.BindingKeys()...)
	}
	return bindings
}

func renderEvaluatorDisabled() string {
	return `Self-Improvement is disabled.

To enable it, add to your config (.pando.toml or ~/.config/pando/config.toml):

  [evaluator]
  enabled = true
  model   = "claude-haiku-4-5-20251001"
  provider = "anthropic"

Restart Pando. The system will begin evaluating sessions automatically.`
}

func renderEvaluatorHelp() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Render("[r] Refresh  [tab] Switch panel  [↑↓] Navigate  [esc/q] Back")
}

// NewEvaluatorPage creates and returns a new self-improvement evaluator page.
func NewEvaluatorPage(svc evaluator.Service) EvaluatorPageModel {
	p := &evaluatorPage{
		evaluatorSvc: svc,
	}
	if svc != nil && svc.IsEnabled() {
		p.loading = true
	}
	return p
}
