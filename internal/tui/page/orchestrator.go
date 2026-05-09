package page

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/app"
	mesnadaOrchestrator "github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	mesnadaModels "github.com/digiogithub/pando/pkg/mesnada/models"
)

const (
	orchestratorMinPromptWidth         = 20
	orchestratorPromptMaxLen           = 80
	orchestratorRefreshIntervalIdle    = 4 * time.Second
	orchestratorRefreshIntervalRunning = 1 * time.Second
)

// spinnerFrames cycles on each tick for running-task animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type orchestratorPage struct {
	app    *app.App
	width  int
	height int

	tasks           []taskRow
	rawTasks        []*mesnadaModels.Task
	selectedIdx     int
	showDetail      bool
	pendingSelectID string
	filterTag       string // when non-empty, only tasks with this tag are shown

	spinnerFrame int // current animation frame for running tasks
	tableHeight  int // visible row height of the table (for mouse calculations)

	table          table.Model
	detailViewport viewport.Model

	showSpawnDialog bool
	spawnInput      textinput.Model
	spawnEngine     textinput.Model
	spawnACPAgent   textinput.Model
}

type taskRow struct {
	ID     string
	Status string
	Engine string
	Model  string
	Prompt string
	Output string
}

type orchestratorKeyMap struct {
	Enter      key.Binding
	Spawn      key.Binding
	Relaunch   key.Binding
	Cancel     key.Binding
	Delete     key.Binding
	Refresh    key.Binding
	Close      key.Binding
	FilterCron key.Binding
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
	Relaunch: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "relaunch task"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "cancel task"),
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
	FilterCron: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "filter cronjob tasks"),
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

type orchestratorTickMsg struct{}

func orchestratorTick(hasRunning bool) tea.Cmd {
	interval := orchestratorRefreshIntervalIdle
	if hasRunning {
		interval = orchestratorRefreshIntervalRunning
	}
	return tea.Tick(interval, func(time.Time) tea.Msg {
		return orchestratorTickMsg{}
	})
}

func NewOrchestratorPage(app *app.App) tea.Model {
	columns := []table.Column{
		{Title: "ID", Width: 12},
		{Title: "Status", Width: 14},
		{Title: "Engine", Width: 12},
		{Title: "Model", Width: 18},
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
	engineInput.Placeholder = "copilot | claude | gemini | opencode | mistral | acp | pando"
	engineInput.Prompt = "engine> "

	agentInput := textinput.New()
	agentInput.Placeholder = "ACP agent name. Used only when engine=acp"
	agentInput.Prompt = "acp_agent> "

	vp := viewport.New(80, 10)
	vp.MouseWheelEnabled = true

	return &orchestratorPage{
		app:            app,
		table:          tableModel,
		detailViewport: vp,
		spawnInput:     input,
		spawnEngine:    engineInput,
		spawnACPAgent:  agentInput,
	}
}

func (p *orchestratorPage) Init() tea.Cmd {
	if p.app == nil || p.app.MesnadaOrchestrator == nil {
		return nil
	}
	return tea.Batch(p.refreshCmd(), orchestratorTick(false))
}

func (p *orchestratorPage) hasRunningTasks() bool {
	for _, task := range p.rawTasks {
		if task.Status == mesnadaModels.TaskStatusRunning || task.Status == mesnadaModels.TaskStatusPending {
			return true
		}
	}
	return false
}

func (p *orchestratorPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseMsg:
		return p.handleMouse(msg)

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

	case orchestratorTickMsg:
		p.spinnerFrame = (p.spinnerFrame + 1) % len(spinnerFrames)
		// Re-render status column with updated spinner before fetching new data.
		p.refreshTableRows()
		return p, tea.Batch(p.refreshCmd(), orchestratorTick(p.hasRunningTasks()))

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
					return p, util.ReportWarn("acp_agent is required when engine=acp")
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
		case key.Matches(msg, orchestratorKeys.Relaunch):
			task := p.selectedTask()
			if task == nil {
				return p, util.ReportWarn("No task selected")
			}
			return p, p.relaunchTaskCmd(task.ID)
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
		case key.Matches(msg, orchestratorKeys.FilterCron):
			if p.filterTag == "cronjob" {
				p.filterTag = ""
			} else {
				p.filterTag = "cronjob"
			}
			p.setTasks(p.rawTasks, p.selectedTaskID())
			return p, nil
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

	headerText := fmt.Sprintf("Orchestrator Dashboard (%d tasks)", len(p.rawTasks))
	if p.filterTag != "" {
		headerText += fmt.Sprintf(" [filter: %s]", p.filterTag)
	}
	header := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(t.Background()).
		Bold(true).
		Width(p.width).
		Render(headerText)

	subtitle := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(t.Background()).
		Width(p.width).
		Render("ID | Status | Engine | Model | Prompt")

	tableHeight, detailHeight := p.paneHeights()
	tablePane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderNormal()).
		BorderBackground(t.Background()).
		Width(max(0, p.width-2)).
		Height(max(0, tableHeight-2)).
		MaxHeight(max(0, tableHeight-2)).
		Render(styles.ForceReplaceBackgroundWithLipgloss(p.table.View(), t.Background()))

	detailContent := p.buildDetailContent()
	p.detailViewport.SetContent(detailContent)
	detailPane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderNormal()).
		BorderBackground(t.Background()).
		Width(max(0, p.width-2)).
		Height(max(0, detailHeight-2)).
		MaxHeight(max(0, detailHeight-2)).
		Render(styles.ForceReplaceBackgroundWithLipgloss(p.detailViewport.View(), t.Background()))

	content := lipgloss.JoinVertical(lipgloss.Left, header, subtitle, tablePane, detailPane)
	view := baseStyle.Width(p.width).Height(p.height).MaxHeight(p.height).Render(content)

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

	tableHeight, detailHeight := p.paneHeights()
	contentWidth := max(10, width-4)
	p.tableHeight = max(3, tableHeight-4)
	p.table.SetWidth(contentWidth)
	p.table.SetHeight(p.tableHeight)
	p.detailViewport.Width = contentWidth
	p.detailViewport.Height = max(2, detailHeight-4)
	p.spawnInput.Width = max(20, min(contentWidth-4, 80))
	p.resizeColumns(contentWidth)
	return nil
}

func (p *orchestratorPage) GetSize() (int, int) {
	return p.width, p.height
}

func (p *orchestratorPage) BindingKeys() []key.Binding {
	if p.showSpawnDialog {
		return []key.Binding{
			orchestratorKeys.Enter,
			orchestratorKeys.Close,
		}
	}
	return append(layout.KeyMapToSlice(p.table.KeyMap), layout.KeyMapToSlice(orchestratorKeys)...)
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

func (p *orchestratorPage) relaunchTaskCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		_, err := p.app.MesnadaOrchestrator.Relaunch(
			context.Background(),
			taskID,
			mesnadaOrchestrator.RelaunchOptions{Background: true},
		)
		if err != nil {
			return orchestratorActionDoneMsg{err: err}
		}
		return orchestratorActionDoneMsg{
			info:         "Task relaunched: " + taskID,
			selectTaskID: taskID,
		}
	}
}

// refreshTableRows re-renders the status column in the existing rows using the
// current spinner frame without reloading data from the orchestrator.
func (p *orchestratorPage) refreshTableRows() {
	visible := p.filteredTasks()
	rows := make([]table.Row, 0, len(visible))
	for _, task := range visible {
		rows = append(rows, table.Row{
			task.ID,
			p.statusText(task.Status),
			fallbackString(string(task.Engine), "-"),
			fallbackString(task.Model, "-"),
			truncatePrompt(task.Prompt, orchestratorPromptMaxLen),
		})
	}
	p.table.SetRows(rows)
}

// SetFilterTag applies a tag filter. Call with empty string to clear the filter.
func (p *orchestratorPage) SetFilterTag(tag string) {
	p.filterTag = tag
	p.setTasks(p.rawTasks, p.selectedTaskID())
}

func (p *orchestratorPage) filteredTasks() []*mesnadaModels.Task {
	if p.filterTag == "" {
		return p.rawTasks
	}
	var filtered []*mesnadaModels.Task
	for _, task := range p.rawTasks {
		for _, tag := range task.Tags {
			if tag == p.filterTag {
				filtered = append(filtered, task)
				break
			}
		}
	}
	return filtered
}

func (p *orchestratorPage) setTasks(tasks []*mesnadaModels.Task, selectedID string) {
	p.rawTasks = tasks
	visible := p.filteredTasks()
	p.tasks = make([]taskRow, 0, len(visible))

	rows := make([]table.Row, 0, len(visible))
	for _, task := range visible {
		row := taskRow{
			ID:     task.ID,
			Status: string(task.Status),
			Engine: string(task.Engine),
			Model:  fallbackString(task.Model, "-"),
			Prompt: truncatePrompt(task.Prompt, orchestratorPromptMaxLen),
			Output: taskOutput(task),
		}
		p.tasks = append(p.tasks, row)
		rows = append(rows, table.Row{
			row.ID,
			p.statusText(task.Status),
			fallbackString(row.Engine, "-"),
			row.Model,
			row.Prompt,
		})
	}

	p.table.SetRows(rows)
	if len(visible) == 0 {
		p.selectedIdx = 0
		p.table.SetCursor(0)
		return
	}

	if !p.setSelectedByID(selectedID) {
		p.selectedIdx = min(p.selectedIdx, len(visible)-1)
		p.table.SetCursor(p.selectedIdx)
	}
}

func (p *orchestratorPage) setSelectedByID(taskID string) bool {
	if strings.TrimSpace(taskID) == "" {
		return false
	}

	for idx, task := range p.filteredTasks() {
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
	visible := p.filteredTasks()
	if len(visible) == 0 {
		return nil
	}

	p.selectedIdx = util.Clamp(p.table.Cursor(), 0, len(visible)-1)
	return visible[p.selectedIdx]
}

func (p *orchestratorPage) buildDetailContent() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle().
		Foreground(t.Text()).
		Width(max(0, p.width-4))

	task := p.selectedTask()
	if task == nil {
		return baseStyle.Foreground(t.TextMuted()).Render("No tasks available")
	}

	statusLabel := p.statusCell(task.Status)
	lines := []string{
		fmt.Sprintf("Task: %s  %s", task.ID, statusLabel),
		fmt.Sprintf("Engine: %s  Model: %s",
			fallbackString(string(task.Engine), "-"),
			fallbackString(task.Model, "-"),
		),
		fmt.Sprintf("Progress: %s", taskProgressLabel(task)),
	}

	if task.CurrentTool != "" {
		lines = append(lines, fmt.Sprintf("Current Tool: %s", task.CurrentTool))
	}

	lines = append(lines,
		fmt.Sprintf("WorkDir: %s", fallbackString(task.WorkDir, ".")),
		fmt.Sprintf("Created: %s", formatTime(task.CreatedAt)),
	)

	if task.StartedAt != nil {
		lines = append(lines, fmt.Sprintf("Started: %s", formatTime(*task.StartedAt)))
	}
	if task.CompletedAt != nil {
		lines = append(lines, fmt.Sprintf("Completed: %s", formatTime(*task.CompletedAt)))
		if task.StartedAt != nil {
			duration := task.CompletedAt.Sub(*task.StartedAt).Round(time.Second)
			lines = append(lines, fmt.Sprintf("Duration: %s", duration))
		}
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

	if len(task.ToolCalls) > 0 {
		lines = append(lines, "", "Tool Call History:")
		for _, tc := range task.ToolCalls {
			status := tc.Status
			if status == "" {
				status = "unknown"
			}
			title := tc.Title
			if title == "" {
				title = tc.Name
			}
			line := fmt.Sprintf("  [%s] %s", status, title)
			if len(tc.Locations) > 0 {
				line += fmt.Sprintf(" (%s)", strings.Join(tc.Locations, ", "))
			}
			lines = append(lines, line)

			// Show arguments and diffs if in detail mode
			if p.showDetail {
				if len(tc.Arguments) > 0 {
					for k, v := range tc.Arguments {
						lines = append(lines, fmt.Sprintf("    %s: %v", k, v))
					}
				}
				if len(tc.Diffs) > 0 {
					for path, diff := range tc.Diffs {
						lines = append(lines, fmt.Sprintf("    Diff for %s: %s", path, diff))
					}
				}
			}
		}
	}

	prompt := taskPrompt(task)
	output := taskOutput(task)

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

	bg := t.Background()

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(bg).
		Bold(true).
		Width(maxWidth).
		Render("Spawn Orchestrator Task")

	description := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(bg).
		Width(maxWidth).
		Render("Enter a prompt for the new task and press Enter to spawn it in the background.")

	input := lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(bg).
		Width(maxWidth).
		Render(p.spawnInput.View())

	engine := lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(bg).
		Width(maxWidth).
		Render(p.spawnEngine.View())

	acpAgent := lipgloss.NewStyle().
		Foreground(t.Text()).
		Background(bg).
		Width(maxWidth).
		Render(p.spawnACPAgent.View())

	footer := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Background(bg).
		Width(maxWidth).
		Render("engine=pando spawns Pando as CLI subprocess (default); engine=acp + acp_agent=<name> for ACP • Enter confirm • Esc cancel")

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
	if len(columns) != 5 {
		return
	}

	columns[0].Width = min(18, max(10, width/7))
	columns[1].Width = 14 // Status: icon + space + longest word = "cancelled" (9) + 2 = 11 + margin
	columns[2].Width = 12
	columns[3].Width = min(20, max(14, width/6))

	remaining := width - columns[0].Width - columns[1].Width - columns[2].Width - columns[3].Width - 10
	columns[4].Width = max(orchestratorMinPromptWidth, remaining)
	p.table.SetColumns(columns)
}

// statusText returns a plain-text (no ANSI codes) status label for use in table
// cells. The bubbles table truncates cell values with runewidth.Truncate which
// counts ANSI escape sequences as visible characters, so styled strings must
// not be placed directly in table rows.
func (p *orchestratorPage) statusText(status mesnadaModels.TaskStatus) string {
	var icon string
	switch status {
	case mesnadaModels.TaskStatusRunning:
		icon = spinnerFrames[p.spinnerFrame%len(spinnerFrames)]
	case mesnadaModels.TaskStatusPending:
		icon = "◌"
	case mesnadaModels.TaskStatusCompleted:
		icon = "✓"
	case mesnadaModels.TaskStatusFailed:
		icon = "✗"
	case mesnadaModels.TaskStatusCancelled:
		icon = "○"
	case mesnadaModels.TaskStatusPaused:
		icon = "⏸"
	default:
		icon = "?"
	}
	return icon + " " + string(status)
}

// handleMouse processes mouse events for the orchestrator page.
// Wheel events scroll the table (upper pane) or the detail viewport (lower
// pane). Left-click events inside the table data area select the clicked row.
func (p *orchestratorPage) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	tablePane, _ := p.paneHeights()
	// Table data rows start after: header(1) + subtitle(1) + border-top(1) + column-headers(1) = Y=4
	const tableDataStartY = 4
	tableDataEndY := tableDataStartY + p.tableHeight

	inTable := msg.Y >= tableDataStartY && msg.Y < tableDataEndY
	inDetail := msg.Y >= tablePane+2 // detail pane starts below the table pane box

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if inDetail {
			p.detailViewport.ScrollUp(3)
		} else {
			p.table.MoveUp(3)
			p.selectedIdx = p.table.Cursor()
		}
		return p, nil
	case tea.MouseButtonWheelDown:
		if inDetail {
			p.detailViewport.ScrollDown(3)
		} else {
			p.table.MoveDown(3)
			p.selectedIdx = p.table.Cursor()
		}
		return p, nil
	}

	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && inTable {
		visibleStart := max(0, p.table.Cursor()-p.tableHeight)
		rowIdx := visibleStart + (msg.Y - tableDataStartY)
		visible := p.filteredTasks()
		if rowIdx >= 0 && rowIdx < len(visible) {
			p.table.SetCursor(rowIdx)
			p.selectedIdx = rowIdx
			// Reset detail viewport scroll when selection changes.
			p.detailViewport.GotoTop()
		}
		return p, nil
	}

	// Forward unhandled mouse events to the table (e.g. future bubbles updates).
	updatedTable, cmd := p.table.Update(msg)
	p.table = updatedTable
	return p, cmd
}

func (p *orchestratorPage) statusCell(status mesnadaModels.TaskStatus) string {
	t := theme.CurrentTheme()

	var icon string
	var color lipgloss.TerminalColor
	bold := false

	switch status {
	case mesnadaModels.TaskStatusRunning:
		icon = spinnerFrames[p.spinnerFrame%len(spinnerFrames)]
		color = t.Success()
		bold = true
	case mesnadaModels.TaskStatusPending:
		icon = "◌"
		color = t.Warning()
	case mesnadaModels.TaskStatusCompleted:
		icon = "✓"
		color = t.Info()
	case mesnadaModels.TaskStatusFailed:
		icon = "✗"
		color = t.Error()
		bold = true
	case mesnadaModels.TaskStatusCancelled:
		icon = "○"
		color = t.TextMuted()
	case mesnadaModels.TaskStatusPaused:
		icon = "⏸"
		color = t.Accent()
	default:
		icon = "?"
		color = t.TextMuted()
	}

	return lipgloss.NewStyle().
		Foreground(color).
		Bold(bold).
		Render(icon + " " + string(status))
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

func taskPrompt(task *mesnadaModels.Task) string {
	if task == nil {
		return "-"
	}
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return "-"
	}
	return prompt
}

func taskOutput(task *mesnadaModels.Task) string {
	if task == nil {
		return "-"
	}
	output := strings.TrimSpace(task.Output)
	if output == "" {
		output = strings.TrimSpace(task.OutputTail)
	}
	if output == "" {
		return "-"
	}
	return output
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
