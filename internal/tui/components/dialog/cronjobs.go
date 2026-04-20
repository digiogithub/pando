package dialog

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/cronjob"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

const cronjobsDialogMinWidth = 56

// OpenCronJobsDialogMsg triggers the cronjobs dialog to open.
type OpenCronJobsDialogMsg struct{}

// CloseCronJobsDialogMsg closes the cronjobs dialog.
type CloseCronJobsDialogMsg struct{}

// CronJobRunNowMsg asks the app to run a cronjob immediately.
type CronJobRunNowMsg struct {
	Name string
}

// CronJobViewTasksMsg asks the app to navigate to the orchestrator filtered by this job.
type CronJobViewTasksMsg struct {
	Name string
}

// CronJobsDialog is the interface for the cronjobs management dialog.
type CronJobsDialog interface {
	tea.Model
	layout.Bindings
	SetJobs(jobs []cronjob.JobStatus)
}

type cronjobsKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Run    key.Binding
	Escape key.Binding
}

var cronjobsKeys = cronjobsKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "previous"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "next"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "view tasks"),
	),
	Run: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "run now"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
}

type cronjobsDialogCmp struct {
	jobs        []cronjob.JobStatus
	selectedIdx int
	width       int
	height      int
}

func (d *cronjobsDialogCmp) Init() tea.Cmd { return nil }

func (d *cronjobsDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, cronjobsKeys.Up):
			if d.selectedIdx > 0 {
				d.selectedIdx--
			}
		case key.Matches(msg, cronjobsKeys.Down):
			if d.selectedIdx < len(d.jobs)-1 {
				d.selectedIdx++
			}
		case key.Matches(msg, cronjobsKeys.Enter):
			if len(d.jobs) == 0 {
				return d, nil
			}
			name := d.jobs[d.selectedIdx].Name
			return d, util.CmdHandler(CronJobViewTasksMsg{Name: name})
		case key.Matches(msg, cronjobsKeys.Run):
			if len(d.jobs) == 0 {
				return d, nil
			}
			name := d.jobs[d.selectedIdx].Name
			return d, util.CmdHandler(CronJobRunNowMsg{Name: name})
		case key.Matches(msg, cronjobsKeys.Escape):
			return d, util.CmdHandler(CloseCronJobsDialogMsg{})
		}
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
	}
	return d, nil
}

func (d *cronjobsDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	dialogWidth := d.contentWidth()

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render("CronJobs")

	var lines []string
	if len(d.jobs) == 0 {
		lines = append(lines, baseStyle.
			Width(dialogWidth).
			Padding(0, 1).
			Foreground(t.TextMuted()).
			Render("No cronjobs configured"))
	} else {
		for i, job := range d.jobs {
			lines = append(lines, d.renderJob(job, i == d.selectedIdx, dialogWidth, t, baseStyle))
		}
	}

	footer := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("[↑↓/jk] Nav  [enter] View tasks  [r] Run now  [esc] Close")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(dialogWidth).Render(""),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		baseStyle.Width(dialogWidth).Render(""),
		footer,
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (d *cronjobsDialogCmp) renderJob(job cronjob.JobStatus, selected bool, width int, t theme.Theme, baseStyle lipgloss.Style) string {
	// Enabled indicator
	indicator := "○"
	indicatorColor := t.TextMuted()
	if job.Enabled {
		indicator = "●"
		indicatorColor = t.Success()
	}
	indicatorStyle := baseStyle.Foreground(indicatorColor)
	if selected {
		indicatorStyle = indicatorStyle.Background(t.SelectionBackground())
	}

	// Name
	nameStyle := baseStyle.Foreground(t.Text())
	if selected {
		nameStyle = nameStyle.
			Background(t.SelectionBackground()).
			Foreground(t.SelectionForeground()).
			Bold(true)
	}

	// Schedule + next run
	meta := job.Schedule
	if !job.NextRun.IsZero() {
		until := time.Until(job.NextRun)
		if until < 0 {
			meta += " (overdue)"
		} else if until < time.Minute {
			meta += fmt.Sprintf(" (in %ds)", int(until.Seconds()))
		} else if until < time.Hour {
			meta += fmt.Sprintf(" (in %dm)", int(until.Minutes()))
		} else {
			meta += fmt.Sprintf(" (in %dh%dm)", int(until.Hours()), int(until.Minutes())%60)
		}
	}
	metaStyle := baseStyle.Foreground(t.TextMuted())
	if selected {
		metaStyle = metaStyle.Background(t.SelectionBackground())
	}

	line := indicatorStyle.Render(indicator) + " " + nameStyle.Render(job.Name) + "  " + metaStyle.Render(meta)
	itemStyle := baseStyle.Width(width).Padding(0, 1)
	if selected {
		itemStyle = itemStyle.Background(t.SelectionBackground())
	}
	return itemStyle.Render(line)
}

func (d *cronjobsDialogCmp) contentWidth() int {
	w := cronjobsDialogMinWidth
	for _, job := range d.jobs {
		needed := 4 + len(job.Name) + 2 + len(job.Schedule) + 16
		if needed > w {
			w = needed
		}
	}
	if d.width > 0 {
		w = max(cronjobsDialogMinWidth, min(w, d.width-10))
	}
	return w
}

func (d *cronjobsDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(cronjobsKeys)
}

func (d *cronjobsDialogCmp) SetJobs(jobs []cronjob.JobStatus) {
	d.jobs = jobs
	if d.selectedIdx >= len(jobs) {
		d.selectedIdx = max(0, len(jobs)-1)
	}
}

// NewCronJobsDialogCmp creates a new cronjobs dialog.
func NewCronJobsDialogCmp() CronJobsDialog {
	return &cronjobsDialogCmp{}
}
