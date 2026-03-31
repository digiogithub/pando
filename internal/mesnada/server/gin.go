package server

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	uiassets "github.com/digiogithub/pando/internal/mesnada/ui"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func (s *Server) newGinEngine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/ui")
	})

	r.GET("/ui", func(c *gin.Context) {
		b, err := fs.ReadFile(uiassets.FS, "index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "failed to load UI")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", b)
	})
	r.GET("/ui/", func(c *gin.Context) {
		b, err := fs.ReadFile(uiassets.FS, "index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "failed to load UI")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", b)
	})

	r.GET("/ui/partials/tasks", gin.WrapF(s.handleUITasks))
	r.GET("/ui/partials/panel", gin.WrapF(s.handleUIPanel))
	r.GET("/ui/partials/log", gin.WrapF(s.handleUILog))
	r.POST("/ui/purge", gin.WrapF(s.handleUIPurge))

	r.GET("/ui/assets/*filepath", func(c *gin.Context) {
		path := "assets" + c.Param("filepath")
		b, err := fs.ReadFile(uiassets.FS, path)
		if err != nil {
			c.String(http.StatusNotFound, "asset not found")
			return
		}

		contentType := "application/octet-stream"
		if strings.HasSuffix(path, ".png") {
			contentType = "image/png"
		} else if strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") {
			contentType = "image/jpeg"
		} else if strings.HasSuffix(path, ".ico") {
			contentType = "image/x-icon"
		}
		c.Data(http.StatusOK, contentType, b)
	})

	api := r.Group("/api")
	{
		api.GET("/version", s.handleAPIVersion)
		api.GET("/tasks", s.handleAPITasksList)
		api.GET("/tasks/:id/log", s.handleAPITaskLog)
		api.POST("/tasks/:id/pause", s.handleAPITaskPause)
		api.POST("/tasks/:id/resume", s.handleAPITaskResume)
		api.POST("/tasks/:id/retry", s.handleAPITaskRetry)
		api.DELETE("/tasks/:id", s.handleAPITaskDelete)
		api.DELETE("/tasks/:id/purge", s.handleAPITaskPurge)
		api.POST("/tasks/:id/acp/prompt", s.handleAPIACPFollowUp)
		api.POST("/tasks/:id/acp/mode", s.handleAPIACPSetMode)
		api.GET("/tasks/:id/acp/status", s.handleAPIACPStatus)
		api.GET("/tasks/:id/acp/permissions", s.handleAPIACPListPermissions)
		api.POST("/tasks/:id/acp/permissions/:req_id", s.handleAPIACPResolvePermission)
	}

	return r
}

func (s *Server) handleAPITasksList(c *gin.Context) {
	statuses, err := parseStatusQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tasks, err := s.orchestrator.ListTasks(models.ListRequest{Status: statuses})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type taskItem struct {
		ID            string            `json:"id"`
		Status        models.TaskStatus `json:"status"`
		PromptExcerpt string            `json:"prompt_excerpt"`
		LogFile       string            `json:"log_file"`
		CreatedAt     string            `json:"created_at"`
	}

	items := make([]taskItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskItem{
			ID:            task.ID,
			Status:        task.Status,
			PromptExcerpt: promptExcerpt(task.Prompt, 80),
			LogFile:       task.LogFile,
			CreatedAt:     task.CreatedAt.Format(time.RFC3339Nano),
		})
	}

	c.JSON(http.StatusOK, gin.H{"tasks": items})
}

func (s *Server) handleAPITaskLog(c *gin.Context) {
	id := c.Param("id")
	task, err := s.findTaskByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if task.LogFile == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "log not available"})
		return
	}

	offset := int64(0)
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
			return
		}
		offset = v
	}

	data, nextOffset, truncated, err := readLogChunk(task.LogFile, offset, 64*1024)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"content":     string(data),
		"next_offset": nextOffset,
		"truncated":   truncated,
	})
}

func (s *Server) handleAPITaskPause(c *gin.Context) {
	id := c.Param("id")
	task, err := s.orchestrator.Pause(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

func (s *Server) handleAPITaskResume(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Prompt     string    `json:"prompt"`
		Model      string    `json:"model"`
		Background *bool     `json:"background"`
		Timeout    string    `json:"timeout"`
		Tags       *[]string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	background := true
	if req.Background != nil {
		background = *req.Background
	}

	task, err := s.orchestrator.Resume(c.Request.Context(), id, orchestrator.ResumeOptions{
		Prompt:     req.Prompt,
		Model:      req.Model,
		Background: background,
		Timeout:    req.Timeout,
		Tags:       req.Tags,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

func (s *Server) handleAPITaskRetry(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Background *bool `json:"background"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	background := true
	if req.Background != nil {
		background = *req.Background
	}

	task, err := s.orchestrator.Retry(c.Request.Context(), id, orchestrator.RetryOptions{
		Background: background,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

func (s *Server) handleAPITaskDelete(c *gin.Context) {
	id := c.Param("id")
	if err := s.orchestrator.Delete(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleAPITaskPurge(c *gin.Context) {
	id := c.Param("id")
	if err := s.orchestrator.Purge(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleAPIVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": s.version,
		"commit":  s.commit,
	})
}

func (s *Server) findTaskByID(id string) (*models.Task, error) {
	tasks, err := s.orchestrator.ListTasks(models.ListRequest{})
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return nil, nil
}

func parseStatusQuery(c *gin.Context) ([]models.TaskStatus, error) {
	raw := c.QueryArray("status")
	if len(raw) == 0 {
		if v := strings.TrimSpace(c.Query("status")); v != "" {
			raw = strings.Split(v, ",")
		}
	}

	var statuses []models.TaskStatus
	for _, part := range raw {
		st := models.TaskStatus(strings.TrimSpace(part))
		if st == "" {
			continue
		}
		switch st {
		case models.TaskStatusPending, models.TaskStatusRunning, models.TaskStatusPaused, models.TaskStatusCompleted, models.TaskStatusFailed, models.TaskStatusCancelled:
			statuses = append(statuses, st)
		default:
			return nil, &apiError{msg: "invalid status"}
		}
	}

	return statuses, nil
}

type apiError struct{ msg string }

func (e *apiError) Error() string { return e.msg }

func promptExcerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func (s *Server) handleAPIACPFollowUp(c *gin.Context) {
	taskID := c.Param("id")

	var req struct {
		Message string `json:"message"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	result, err := s.orchestrator.ACPSessionControl(taskID, "follow_up", req.Message, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleAPIACPSetMode(c *gin.Context) {
	taskID := c.Param("id")

	var req struct {
		Mode string `json:"mode"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Mode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode is required"})
		return
	}

	validModes := map[string]bool{"code": true, "ask": true, "architect": true}
	if !validModes[req.Mode] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode (valid: code, ask, architect)"})
		return
	}

	result, err := s.orchestrator.ACPSessionControl(taskID, "set_mode", "", req.Mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleAPIACPStatus(c *gin.Context) {
	taskID := c.Param("id")

	result, err := s.orchestrator.ACPSessionControl(taskID, "status", "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleAPIACPListPermissions(c *gin.Context) {
	taskID := c.Param("id")

	result, err := s.orchestrator.ACPSessionControl(taskID, "list_permissions", "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleAPIACPResolvePermission(c *gin.Context) {
	taskID := c.Param("id")
	reqID := c.Param("req_id")

	var req struct {
		Action   string `json:"action"`
		OptionID string `json:"option_id,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Action != "approve" && req.Action != "deny" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action must be 'approve' or 'deny'"})
		return
	}

	data := fmt.Sprintf(`{"request_id":"%s","action":"%s","option_id":"%s"}`, reqID, req.Action, req.OptionID)
	result, err := s.orchestrator.ACPSessionControl(taskID, "resolve_permission", data, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func readLogChunk(path string, offset, max int64) ([]byte, int64, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, false, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, offset, false, err
	}

	size := st.Size()
	start := offset
	truncated := false

	if start < 0 {
		start = 0
	}
	if start > size {
		start = size
	}
	if start == 0 && size > max {
		start = size - max
		truncated = true
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, start, false, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, start, false, err
	}

	if max > 0 && int64(len(data)) > max {
		data = data[:max]
		truncated = true
	}

	nextOffset := start + int64(len(data))
	return data, nextOffset, truncated, nil
}
