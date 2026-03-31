package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	uiassets "github.com/digiogithub/pando/internal/mesnada/ui"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

type uiTaskRow struct {
	ID            string
	Status        models.TaskStatus
	StatusClass   string
	ProgressText  string
	WhenText      string
	WhenTitle     string
	Tags          []string
	Engine        string
	EngineClass   string
	Model         string
	PromptExcerpt string
	IsACP         bool
	ACPMode       string
}

type uiTasksVM struct {
	Tasks []uiTaskRow
}

type uiPanelVM struct {
	Task          *models.Task
	Engine        string
	EngineClass   string
	Model         string
	ProgressText  string
	WhenText      string
	WhenTitle     string
	FinishedText  string
	FinishedTitle string
	DurationText  string
	TagsText      string
	Prompt        string
	IsACP         bool
	ACPMode       string
	ACPAgentName  string
	ACPToolCalls  int
	ACPSessionID  string
	Permissions   []uiPermission
}

type uiPermission struct {
	RequestID string             `json:"request_id"`
	ToolCall  uiPermissionTool   `json:"tool_call"`
	Options   []uiPermissionItem `json:"options"`
}

type uiPermissionTool struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type uiPermissionItem struct {
	OptionID string `json:"option_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type uiLogVM struct {
	Log string
}

func (s *Server) getUITemplates() (*template.Template, error) {
	s.uiOnce.Do(func() {
		s.uiTpl, s.uiTplErr = template.ParseFS(fs.FS(uiassets.FS), "partials/*.html")
	})
	return s.uiTpl, s.uiTplErr
}

func (s *Server) handleUITasks(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	status := strings.TrimSpace(r.FormValue("status"))

	var statuses []models.TaskStatus
	if status != "" && status != "all" {
		statuses = []models.TaskStatus{models.TaskStatus(status)}
	}

	tasks, err := s.orchestrator.ListTasks(models.ListRequest{Status: statuses})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	vm := uiTasksVM{Tasks: make([]uiTaskRow, 0, len(tasks))}
	for _, task := range tasks {
		when := task.CreatedAt
		if task.StartedAt != nil {
			when = *task.StartedAt
		}

		progressText := "-"
		if task.Progress != nil {
			progressText = fmt.Sprintf("%d%%", task.Progress.Percentage)
		}

		engine := string(task.Engine)
		if engine == "" {
			engine = string(models.DefaultEngine())
		}

		isACP := isACPEngine(task.Engine)
		acpMode := ""
		if isACP && task.ACPMode != "" {
			acpMode = task.ACPMode
		}

		vm.Tasks = append(vm.Tasks, uiTaskRow{
			ID:            task.ID,
			Status:        task.Status,
			StatusClass:   statusClass(task.Status),
			ProgressText:  progressText,
			WhenText:      when.Format("2006-01-02 15:04:05"),
			WhenTitle:     when.Format(time.RFC3339),
			Tags:          task.Tags,
			Engine:        engine,
			EngineClass:   engineClass(task.Engine),
			Model:         task.Model,
			PromptExcerpt: truncate(stripTaskIDPrefix(task.Prompt), 100),
			IsACP:         isACP,
			ACPMode:       acpMode,
		})
	}

	tpl, err := s.getUITemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.ExecuteTemplate(w, "tasks.html", vm)
}

func (s *Server) handleUIPanel(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	taskID := strings.TrimSpace(r.FormValue("task_id"))
	if taskID == "" {
		http.Error(w, "missing task_id", http.StatusBadRequest)
		return
	}

	task, err := s.orchestrator.GetTask(taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	when := task.CreatedAt
	if task.StartedAt != nil {
		when = *task.StartedAt
	}

	finishedText := "-"
	finishedTitle := ""
	durationText := "-"
	if task.CompletedAt != nil {
		finished := *task.CompletedAt
		finishedText = finished.Format("2006-01-02 15:04:05")
		finishedTitle = finished.Format(time.RFC3339)

		startForDuration := task.CreatedAt
		if task.StartedAt != nil {
			startForDuration = *task.StartedAt
		}
		d := finished.Sub(startForDuration).Round(time.Second)
		if d < 0 {
			d = 0
		}
		durationText = d.String()
	}

	progressText := "-"
	if task.Progress != nil {
		progressText = fmt.Sprintf("%d%%", task.Progress.Percentage)
	}

	tagsText := "-"
	if len(task.Tags) > 0 {
		tagsText = strings.Join(task.Tags, ", ")
	}

	engine := string(task.Engine)
	if engine == "" {
		engine = string(models.DefaultEngine())
	}

	isACP := isACPEngine(task.Engine)
	acpMode := ""
	acpAgentName := ""
	acpToolCalls := 0
	acpSessionID := ""
	if isACP {
		acpMode = task.ACPMode
		acpSessionID = task.ACPSessionID
		if task.ACPStatus != nil {
			acpAgentName = task.ACPStatus.AgentName
			acpToolCalls = task.ACPStatus.ToolCalls
		}
	}

	permissions := make([]uiPermission, 0)
	if isACP && task.Status == models.TaskStatusRunning {
		permissions = s.getUIPermissions(task.ID)
	}

	vm := uiPanelVM{
		Task:          task,
		Engine:        engine,
		EngineClass:   engineClass(task.Engine),
		Model:         task.Model,
		ProgressText:  progressText,
		WhenText:      when.Format("2006-01-02 15:04:05"),
		WhenTitle:     when.Format(time.RFC3339),
		FinishedText:  finishedText,
		FinishedTitle: finishedTitle,
		DurationText:  durationText,
		TagsText:      tagsText,
		Prompt:        stripTaskIDPrefix(task.Prompt),
		IsACP:         isACP,
		ACPMode:       acpMode,
		ACPAgentName:  acpAgentName,
		ACPToolCalls:  acpToolCalls,
		ACPSessionID:  acpSessionID,
		Permissions:   permissions,
	}

	tpl, err := s.getUITemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.ExecuteTemplate(w, "panel.html", vm)
}

func (s *Server) handleUILog(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	taskID := strings.TrimSpace(r.FormValue("task_id"))
	if taskID == "" {
		http.Error(w, "missing task_id", http.StatusBadRequest)
		return
	}

	task, err := s.orchestrator.GetTask(taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	logText := ""
	if task.LogFile != "" {
		logText = readLastBytes(task.LogFile, 1024*1024)
	}
	if logText == "" {
		logText = task.Output
	}

	tpl, err := s.getUITemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.ExecuteTemplate(w, "log.html", uiLogVM{Log: logText})
}

func (s *Server) handleUIPurge(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	taskID := strings.TrimSpace(r.FormValue("task_id"))
	if taskID == "" {
		http.Error(w, "missing task_id", http.StatusBadRequest)
		return
	}

	if err := s.orchestrator.Purge(taskID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.handleUITasks(w, r)
}

func statusClass(st models.TaskStatus) string {
	switch st {
	case models.TaskStatusPending:
		return "st-pending"
	case models.TaskStatusRunning:
		return "st-running"
	case models.TaskStatusCompleted:
		return "st-completed"
	case models.TaskStatusFailed:
		return "st-failed"
	case models.TaskStatusCancelled:
		return "st-cancelled"
	case models.TaskStatusPaused:
		return "st-paused"
	default:
		return ""
	}
}

func engineClass(engine models.Engine) string {
	if engine == "" {
		engine = models.DefaultEngine()
	}
	switch engine {
	case models.EngineClaude:
		return "engine-claude"
	case models.EngineCopilot:
		return "engine-copilot"
	case models.EngineGemini:
		return "engine-gemini"
	case models.EngineOpenCode:
		return "engine-opencode"
	case models.EngineOllamaClaude:
		return "engine-ollama-claude"
	case models.EngineOllamaOpenCode:
		return "engine-ollama-opencode"
	case models.EngineMistral:
		return "engine-mistral"
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex, models.EngineACPCustom:
		return "engine-acp"
	default:
		return "engine-copilot"
	}
}

func isACPEngine(engine models.Engine) bool {
	switch engine {
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex, models.EngineACPCustom:
		return true
	default:
		return false
	}
}

func stripTaskIDPrefix(prompt string) string {
	p := strings.TrimSpace(prompt)
	if strings.HasPrefix(p, "You are the task_id:") {
		if idx := strings.Index(p, "\n"); idx >= 0 {
			p = strings.TrimSpace(p[idx+1:])
		}
	}
	return p
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func readLastBytes(path string, max int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return ""
	}

	size := st.Size()
	start := int64(0)
	if size > max {
		start = size - max
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *Server) getUIPermissions(taskID string) []uiPermission {
	result, err := s.orchestrator.ACPSessionControl(taskID, "list_permissions", "", "")
	if err != nil {
		return nil
	}

	b, err := json.Marshal(result)
	if err != nil {
		return nil
	}

	var payload struct {
		Permissions []uiPermission `json:"permissions"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil
	}

	return payload.Permissions
}
