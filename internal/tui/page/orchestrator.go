package page

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	mesnadaModels "github.com/digiogithub/pando/pkg/mesnada/models"
)

const (
	orchestratorMinPromptWidth = 20
	orchestratorPromptMaxLen   = 80
)

type orchestratorPage struct {
	app    *app.App
	width  int
	height int

	tasks           []taskRow
	rawTasks        []*mesnadaModels.Task
	selectedIdx     int
	showDetail      bool
	pendingSelectID string

	table table.Model

	showSpawnDialog bool
	spawnInput      textinput.Model
	spawnEngine     textinput.Model
	spawnACPAgent   textinput.Model
}

type taskRow struct {
	ID       string
	Status   string
	Engine   string
	Model    string
	Progress string
	Prompt   string
}

type orchestratorKeyMap struct {
	Enter   key.Binding
	Spawn   key.Binding
	Cancel  key.Binding
	Delete  key.Binding
	Refresh key.Binding
	Close   key.Binding
}

var orchestratorKeys = orchestratorKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "toggle detail"),
	),
	Spawn: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "spawn task"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "cancel task"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete task"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Close: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close dialog"),
	),
}

type orchestratorTasksLoadedMsg struct {
	tasks []*mesnadaModels.Task
	err   error
}

type orchestratorActionDoneMsg struct {
	info         string
	err          error
	selectTaskID string
}

func NewOrchestratorPage(app *app.App) tea.Model {
	columns := []table.Column{
		{Title: "ID", Width: 12},
		{Title: "Status", Width: 12},
		{Title: "Engine", Width: 12},
		{Title: "Model", Width: 18},
		{Title: "Progress", Width: 14},
		{Title: "Prompt", Width: orchestratorPromptMaxLen},
	}

	tableModel := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
	)
	tableModel.Focus()

	input := textinput.New()
	input.Placeholder = "Describe the task to spawn..."
	input.Prompt = "prompt> "

	engineInput := textinput.New()
	engineInput.Placeholder = "copilot | claude | gemini | opencode | ollama-claude | ollama-opencode | mistral | acp"
	engineInput.Prompt = "engine> "

	agentInput := textinput.New()
	agentInput.Placeholder = "ACP agent name (e.g. pando). Used only when engine=acp"
	agentInput.Prompt = "acp_agent> "

	return &orchestratorPage{
		app:          app,
		table:        tableModel,
		spawnInput:   input,
		spawnEngine:  engineInput,
		spawnACPAgent: agentInput,
	}
}

func (p *orchestratorPage) Init() tea.Cmd {
	if p.app == nil || p.app.MesnadaOrchestrator == nil {
		return nil
	}
	return p.refreshCmd()
}

func (p *orchestratorPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.SetSize(msg.Width, msg.Height)

	case orchestratorTasksLoadedMsg:
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}

		selectedID := p.selectedTaskID()
		if p.pendingSelectID != "" {
			selectedID = p.pendingSelectID
			p.pendingSelectID = ""
		}
		p.setTasks(msg.tasks, selectedID)
		return p, nil

	case orchestratorActionDoneMsg:
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}

		if msg.selectTaskID != "" {
			p.pendingSelectID = msg.selectTaskID
		}

		cmds = append(cmds, p.refreshCmd())
		if strings.TrimSpace(msg.info) != "" {
			cmds = append(cmds, util.ReportInfo(msg.info))
		}
		return p, tea.Batch(cmds...)

	case tea.KeyMsg:
		if p.showSpawnDialog {
			switch {
			case key.Matches(msg, orchestratorKeys.Close):
				p.showSpawnDialog = false
				p.spawnInput.Blur()
				p.spawnEngine.Blur()
				p.spawnACPAgent.Blur()
				return p, nil
			case key.Matches(msg, orchestratorKeys.Enter):
				prompt := strings.TrimSpace(p.spawnInput.Value())
				if prompt == "" {
					return p, util.ReportWarn("Task prompt cannot be empty")
				}
				engine := strings.TrimSpace(p.spawnEngine.Value())
				acpAgent := strings.TrimSpace(p.spawnACPAgent.Value())
				if strings.EqualFold(engine, "acp") && acpAgent == "" {
					return p, util.ReportWarn("acp_agent is required when engine is acp")
				}
				p.showSpawnDialog = false
				p.spawnInput.Blur()
				p.spawnEngine.Blur()
				p.spawnACPAgent.Blur()
				return p, p.spawnTaskCmd(prompt, engine, acpAgent)
			}

			var cmd tea.Cmd
			p.spawnInput, cmd = p.spawnInput.Update(msg)
			p.spawnEngine, _ = p.spawnEngine.Update(msg)
			p.spawnACPAgent, _ = p.spawnACPAgent.Update(msg)
			return p, cmd
		}

		switch {
		case key.Matches(msg, orchestratorKeys.Enter):
			if len(p.rawTasks) == 0 {
				return p, nil
			}
			p.showDetail = !p.showDetail
			return p, nil
		case key.Matches(msg, orchestratorKeys.Spawn):
			if p.app == nil || p.app.MesnadaOrchestrator == nil {
				return p, util.ReportWarn("Mesnada not enabled")
			}
			p.showSpawnDialog = true
			p.spawnInput.SetValue("")
			p.spawnEngine.SetValue("")
			p.spawnACPAgent.SetValue("")
			p.spawnInput.Focus()
			return p, textinput.Blink
		case key.Matches(msg, orchestratorKeys.Cancel):
			task := p.selectedTask()
			if task == nil {
				return p, util.ReportWarn("No task selected")
			}
			return p, p.cancelTaskCmd(task.ID)
		case key.Matches(msg, orchestratorKeys.Delete):
			task := p.selectedTask()
			if task == nil {
				return p, util.ReportWarn("No task selected")
			}
			return p, p.deleteTaskCmd(task.ID)
		case key.Matches(msg, orchestratorKeys.Refresh):
			return p, p.refreshCmd()
		}
	}

	previousCursor := p.table.Cursor()
	updatedTable, cmd := p.table.Update(msg)
	p.table = updatedTable
	p.selectedIdx = p.table.Cursor()
	if previousCursor != p.selectedIdx && !p.showDetail {
		p.showDetail = false
	}
	return p, cmd
}

func (p *orchestratorPage) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if p.width <= 0 || p.height <= 0 {
		return ""
	}

	if p.app == nil || p.app.MesnadaOrchestrator == nil {
		message := lipgloss.NewStyle().
			Foreground(t.Warning()).
			Bold(true).
			Width(max(0, p.width-4)).
			Align(lipgloss.Center).
			Render("Mesnada not enabled. Enable in Settings > Mesnada")

		return baseStyle.Width(p.width).Height(p.height).Render(
			lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center, message),
		)
	}

	header := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Width(p.width).
		Render(fmt.Sprintf("Orchestrator Dashboard (%d tasks)", len(p.rawTasks)))

	subtitle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(p.width).
		Render("ID | Status | Engine | Model | Progress | Prompt")

	tableHeight, detailHeight := p.paneHeights()
	tablePane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderNormal()).
		BorderBackground(t.Background()).
		Width(max(0, p.width-2)).
		Height(max(0, tableHeight-2)).
		Render(styles.ForceReplaceBackgroundWithLipgloss(p.table.View(), t.Background()))

	detailPane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderNormal()).
		BorderBackground(t.Background()).
		Width(max(0, p.width-2)).
		Height(max(0, detailHeight-2)).
		Render(p.renderDetail())

	content := lipgloss.JoinVertical(lipgloss.Left, header, subtitle, tablePane, detailPane)
	view := baseStyle.Width(p.width).Height(p.height).Render(content)

	if p.showSpawnDialog {
		overlay := p.renderSpawnDialog()
		row := max(0, (lipgloss.Height(view)-lipgloss.Height(overlay))/2)
		col := max(0, (lipgloss.Width(view)-lipgloss.Width(overlay))/2)
		view = layout.PlaceOverlay(col, row, overlay, view, true)
	}

	return view
}

func (p *orchestratorPage) SetSize(width, height int) tea.Cmd {
	p.width = width
	p.height = height

	tableHeight, _ := p.paneHeights()
	contentWidth := max(10, width-4)
	p.table.SetWidth(contentWidth)
	p.table.SetHeight(max(3, tableHeight-4))
	p.spawnInput.Width = max(20, min(contentWidth-4, 80))
	p.resizeColumns(contentWidth)
	return nil
}

func (p *orchestratorPage) GetSize() (int, int) {
	return p.width, p.height
}

func (p *orchestratorPage) BindingKeys() []key.Binding {
	bindings := append(layout.KeyMapToSlice(p.table.KeyMap), layout.KeyMapToSlice(orchestratorKeys)...)
	if !p.showSpawnDialog {
		return bindings
	}

	return []key.Binding{
		orchestratorKeys.Enter,
		orchestratorKeys.Close,
	}
}

func (p *orchestratorPage) paneHeights() (int, int) {
	available := max(6, p.height-2)
	tableHeight := max(6, (available*3)/5)
	detailHeight := max(4, available-tableHeight)
	return tableHeight, detailHeight
}

func (p *orchestratorPage) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		if p.app == nil || p.app.MesnadaOrchestrator == nil {
			return orchestratorTasksLoadedMsg{}
		}

		tasks, err := p.app.MesnadaOrchestrator.ListTasks(mesnadaModels.ListRequest{})
		return orchestratorTasksLoadedMsg{tasks: tasks, err: err}
	}
}

func (p *orchestratorPage) spawnTaskCmd(prompt, engine, acpAgent string) tea.Cmd {
	return func() tea.Msg {
		spawnReq := mesnadaModels.SpawnRequest{
			Prompt:     prompt,
			WorkDir:    ".",
			Background: true,
		}
		if engine != "" {
			spawnReq.Engine = mesnadaModels.Engine(engine)
		}
		if acpAgent != "" {
			spawnReq.ACPAgent = acpAgent
		}
		task, err := p.app.MesnadaOrchestrator.Spawn(context.Background(), spawnReq)
		if err != nil {
			return orchestratorActionDoneMsg{err: err}
		}
		return orchestratorActionDoneMsg{
			info:         "Task spawned: " + task.ID,
			selectTaskID: task.ID,
		}
	}
}

func (p *orchestratorPage) cancelTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if err := p.app.MesnadaOrchestrator.Cancel(taskID); err != nil {
			return orchestratorActionDoneMsg{err: err}
		}
		return orchestratorActionDoneMsg{
			info:         "Task cancelled: " + taskID,
			selectTaskID: taskID,
		}
	}
}

func (p *orchestratorPage) deleteTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		if err := p.app.MesnadaOrchestrator.Delete(taskID); err != nil {
			return orchestratorActionDoneMsg{err: err}
		}
		return orchestratorActionDoneMsg{
			info: "Task deleted: " + taskID,
		}
	}
}

func (p *orchestratorPage) setTasks(tasks []*mesnadaModels.Task, selectedID string) {
	p.rawTasks = tasks
	p.tasks = make([]taskRow, 0, len(tasks))

	rows := make([]table.Row, 0, len(tasks))
	for _, task := range tasks {
		row := taskRow{
			ID:       task.ID,
			Status:   string(task.Status),
			Engine:   string(task.Engine),
			Model:    fallbackString(task.Model, "-"),
			Progress: taskProgressLabel(task),
			Prompt:   truncatePrompt(task.Prompt, orchestratorPromptMaxLen),
		}
		p.tasks = append(p.tasks, row)
		rows = append(rows, table.Row{
			row.ID,
			p.statusCell(task.Status),
			fallbackString(row.Engine, "-"),
			row.Model,
			row.Progress,
			row.Prompt,
		})
	}

	p.table.SetRows(rows)
	if len(tasks) == 0 {
		p.selectedIdx = 0
		p.table.SetCursor(0)
		return
	}

	if !p.setSelectedByID(selectedID) {
		p.selectedIdx = min(p.selectedIdx, len(tasks)-1)
		p.table.SetCursor(p.selectedIdx)
	}
}

func (p *orchestratorPage) setSelectedByID(taskID string) bool {
	if strings.TrimSpace(taskID) == "" {
		return false
	}

	for idx, task := range p.rawTasks {
		if task.ID == taskID {
			p.selectedIdx = idx
			p.table.SetCursor(idx)
			return true
		}
	}

	return false
}

func (p *orchestratorPage) selectedTaskID() string {
	task := p.selectedTask()
	if task == nil {
		return ""
	}
	return task.ID
}

func (p *orchestratorPage) selectedTask() *mesnadaModels.Task {
	if len(p.rawTasks) == 0 {
		return nil
	}

	p.selectedIdx = util.Clamp(p.table.Cursor(), 0, len(p.rawTasks)-1)
	return p.rawTasks[p.selectedIdx]
}

func (p *orchestratorPage) renderDetail() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle().
		Foreground(t.Text()).
		Width(max(0, p.width-4))

	task := p.selectedTask()
	if task == nil {
		return baseStyle.Foreground(t.TextMuted()).Render("No tasks available")
	}

	lines := []string{
		fmt.Sprintf("Selected Task: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
		fmt.Sprintf("Engine: %s", fallbackString(string(task.Engine), "-")),
		fmt.Sprintf("Model: %s", fallbackString(task.Model, "-")),
		fmt.Sprintf("Progress: %s", taskProgressLabel(task)),
		fmt.Sprintf("WorkDir: %s", fallbackString(task.WorkDir, ".")),
		fmt.Sprintf("Created: %s", formatTime(task.CreatedAt)),
	}

	if task.StartedAt != nil {
		lines = append(lines, fmt.Sprintf("Started: %s", formatTime(*task.StartedAt)))
	}
	if task.CompletedAt != nil {
		lines = append(lines, fmt.Sprintf("Completed: %s", formatTime(*task.CompletedAt)))
	}
	if len(task.Dependencies) > 0 {
		lines = append(lines, "Dependencies: "+strings.Join(task.Dependencies, ", "))
	}
	if len(task.Tags) > 0 {
		lines = append(lines, "Tags: "+strings.Join(task.Tags, ", "))
	}
	if task.Error != "" {
		lines = append(lines, "Error: "+task.Error)
	}

	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		prompt = "-"
	}
	output := strings.TrimSpace(task.OutputTail)
	if output == "" {
		output = strings.TrimSpace(task.Output)
	}
	if output == "" {
		output = "-"
	}

	if p.showDetail {
		lines = append(lines, "", "Prompt:", prompt, "", "Output:", output)
	} else {
		lines = append(lines,
			"",
			"Prompt:",
			truncateMultiline(prompt, 4, 240),
			"",
			"Output:",
			truncateMultiline(output, 4, 240),
		)
	}

	return baseStyle.Render(strings.Join(lines, "\n"))
}

func (p *orchestratorPage) renderSpawnDialog() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	maxWidth := max(30, min(70, max(30, p.width-8)))

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Render("Spawn Orchestrator Task")

	description := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(maxWidth).
		Render("Enter a prompt for the new task and press Enter to spawn it in the background.")

	input := lipgloss.NewStyle().
		Foreground(t.Text()).
		Width(maxWidth).
		Render(p.spawnInput.View())

	engine := lipgloss.NewStyle().
		Foreground(t.Text()).
		Width(maxWidth).
		Render(p.spawnEngine.View())

	acpAgent := lipgloss.NewStyle().
		Foreground(t.Text()).
		Width(maxWidth).
		Render(p.spawnACPAgent.View())

	footer := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Width(maxWidth).
		Render("Use engine=acp + acp_agent=pando for Pando subagent via ACP • Enter confirm • Esc cancel")

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", description, "", input, "", engine, "", acpAgent, "", footer)
	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		BorderBackground(t.Background()).
		Width(maxWidth + 4).
		Render(content)
}

func (p *orchestratorPage) resizeColumns(width int) {
	columns := p.table.Columns()
	if len(columns) != 6 {
		return
	}

	columns[0].Width = min(18, max(10, width/7))
	columns[1].Width = 12
	columns[2].Width = 12
	columns[3].Width = min(20, max(14, width/6))
	columns[4].Width = 16

	remaining := width - columns[0].Width - columns[1].Width - columns[2].Width - columns[3].Width - columns[4].Width - 10
	columns[5].Width = max(orchestratorMinPromptWidth, remaining)
	p.table.SetColumns(columns)
}

func (p *orchestratorPage) statusCell(status mesnadaModels.TaskStatus) string {
	t := theme.CurrentTheme()

	color := t.TextMuted()
	switch status {
	case mesnadaModels.TaskStatusRunning:
		color = t.Success()
	case mesnadaModels.TaskStatusPending:
		color = t.Warning()
	case mesnadaModels.TaskStatusCompleted:
		color = t.Info()
	case mesnadaModels.TaskStatusFailed:
		color = t.Error()
	case mesnadaModels.TaskStatusCancelled:
		color = t.TextMuted()
	case mesnadaModels.TaskStatusPaused:
		color = t.Accent()
	}

	return lipgloss.NewStyle().
		Foreground(color).
		Bold(status == mesnadaModels.TaskStatusRunning || status == mesnadaModels.TaskStatusFailed).
		Render(string(status))
}

func taskProgressLabel(task *mesnadaModels.Task) string {
	if task == nil || task.Progress == nil {
		return "-"
	}

	if desc := strings.TrimSpace(task.Progress.Description); desc != "" {
		return fmt.Sprintf("%d%% %s", task.Progress.Percentage, truncatePrompt(desc, 18))
	}
	return fmt.Sprintf("%d%%", task.Progress.Percentage)
}

func truncatePrompt(prompt string, maxLen int) string {
	prompt = strings.Join(strings.Fields(prompt), " ")
	if len(prompt) <= maxLen {
		return prompt
	}
	if maxLen <= 3 {
		return prompt[:maxLen]
	}
	return prompt[:maxLen-3] + "..."
}

func truncateMultiline(value string, maxLines, maxChars int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}

	lines := strings.Split(value, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[len(lines)-1] = lines[len(lines)-1] + " ..."
	}

	result := strings.Join(lines, "\n")
	if len(result) <= maxChars {
		return result
	}
	return result[:maxChars-3] + "..."
}

func formatTime(t time.Time) string {
	return t.Local().Format(time.RFC3339)
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var _ layout.Sizeable = (*orchestratorPage)(nil)
var _ layout.Bindings = (*orchestratorPage)(nil)
