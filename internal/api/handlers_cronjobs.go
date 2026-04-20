package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// CronJobResponse is the JSON representation of a cronjob returned by the API.
type CronJobResponse struct {
	Name     string    `json:"name"`
	Schedule string    `json:"schedule"`
	Enabled  bool      `json:"enabled"`
	Prompt   string    `json:"prompt,omitempty"`
	Engine   string    `json:"engine,omitempty"`
	Model    string    `json:"model,omitempty"`
	WorkDir  string    `json:"workDir,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
	Timeout  string    `json:"timeout,omitempty"`
	NextRun  time.Time `json:"nextRun,omitempty"`
}

// CreateCronJobRequest is the body for POST /api/v1/cronjobs.
type CreateCronJobRequest struct {
	Name     string   `json:"name"`
	Schedule string   `json:"schedule"`
	Prompt   string   `json:"prompt"`
	Enabled  bool     `json:"enabled"`
	Engine   string   `json:"engine,omitempty"`
	Model    string   `json:"model,omitempty"`
	WorkDir  string   `json:"workDir,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Timeout  string   `json:"timeout,omitempty"`
}

// UpdateCronJobRequest is the body for PUT /api/v1/cronjobs/{name}.
type UpdateCronJobRequest struct {
	Enabled  *bool    `json:"enabled,omitempty"`
	Prompt   *string  `json:"prompt,omitempty"`
	Schedule *string  `json:"schedule,omitempty"`
	Engine   *string  `json:"engine,omitempty"`
	Model    *string  `json:"model,omitempty"`
	WorkDir  *string  `json:"workDir,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Timeout  *string  `json:"timeout,omitempty"`
}

// handleListCronJobs handles GET /api/v1/cronjobs.
func (s *Server) handleListCronJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.CronService == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"jobs":    []CronJobResponse{},
			"enabled": false,
		})
		return
	}

	jobs := s.app.CronService.ListJobs()
	responses := make([]CronJobResponse, 0, len(jobs))
	for _, j := range jobs {
		tags := j.Tags
		if tags == nil {
			tags = []string{}
		}
		responses = append(responses, CronJobResponse{
			Name:     j.Name,
			Schedule: j.Schedule,
			Enabled:  j.Enabled,
			Prompt:   j.Prompt,
			Engine:   j.Engine,
			Model:    j.Model,
			WorkDir:  j.WorkDir,
			Tags:     tags,
			Timeout:  j.Timeout,
			NextRun:  j.NextRun,
		})
	}

	cfg := config.Get()
	enabled := cfg != nil && cfg.CronJobs.Enabled

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":    responses,
		"enabled": enabled,
	})
}

// handleCreateCronJob handles POST /api/v1/cronjobs.
func (s *Server) handleCreateCronJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req CreateCronJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Schedule) == "" {
		writeError(w, http.StatusBadRequest, "schedule is required")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	// Check for duplicate names.
	for _, j := range cfg.CronJobs.Jobs {
		if strings.EqualFold(j.Name, req.Name) {
			writeError(w, http.StatusConflict, "a cronjob with that name already exists")
			return
		}
	}

	newJob := config.CronJob{
		Name:     req.Name,
		Schedule: req.Schedule,
		Prompt:   req.Prompt,
		Enabled:  req.Enabled,
		Engine:   req.Engine,
		Model:    req.Model,
		WorkDir:  req.WorkDir,
		Tags:     req.Tags,
		Timeout:  req.Timeout,
	}

	updated := config.CronJobsConfig{
		Enabled: cfg.CronJobs.Enabled,
		Jobs:    append(append([]config.CronJob(nil), cfg.CronJobs.Jobs...), newJob),
	}

	if err := config.UpdateCronJobs(updated); err != nil {
		writeError(w, http.StatusBadRequest, "failed to save cronjob: "+err.Error())
		return
	}

	// Hot-reload the service if running.
	if s.app.CronService != nil {
		cfg2 := config.Get()
		if cfg2 != nil {
			_ = s.app.CronService.Reload(cfg2.CronJobs)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]string{"name": newJob.Name})
}

// handleCronJobByName dispatches PUT and DELETE for /api/v1/cronjobs/{name}.
func (s *Server) handleCronJobByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name path parameter required")
		return
	}

	switch r.Method {
	case http.MethodPut:
		s.handleUpdateCronJob(w, r, name)
	case http.MethodDelete:
		s.handleDeleteCronJob(w, r, name)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleUpdateCronJob handles PUT /api/v1/cronjobs/{name}.
func (s *Server) handleUpdateCronJob(w http.ResponseWriter, r *http.Request, name string) {
	var req UpdateCronJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	jobs := append([]config.CronJob(nil), cfg.CronJobs.Jobs...)
	found := false
	for i, j := range jobs {
		if strings.EqualFold(j.Name, name) {
			if req.Enabled != nil {
				jobs[i].Enabled = *req.Enabled
			}
			if req.Prompt != nil {
				jobs[i].Prompt = *req.Prompt
			}
			if req.Schedule != nil {
				jobs[i].Schedule = *req.Schedule
			}
			if req.Engine != nil {
				jobs[i].Engine = *req.Engine
			}
			if req.Model != nil {
				jobs[i].Model = *req.Model
			}
			if req.WorkDir != nil {
				jobs[i].WorkDir = *req.WorkDir
			}
			if req.Tags != nil {
				jobs[i].Tags = req.Tags
			}
			if req.Timeout != nil {
				jobs[i].Timeout = *req.Timeout
			}
			found = true
			break
		}
	}

	if !found {
		writeError(w, http.StatusNotFound, "cronjob not found")
		return
	}

	updated := config.CronJobsConfig{
		Enabled: cfg.CronJobs.Enabled,
		Jobs:    jobs,
	}

	if err := config.UpdateCronJobs(updated); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update cronjob: "+err.Error())
		return
	}

	if s.app.CronService != nil {
		cfg2 := config.Get()
		if cfg2 != nil {
			_ = s.app.CronService.Reload(cfg2.CronJobs)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// handleDeleteCronJob handles DELETE /api/v1/cronjobs/{name}.
func (s *Server) handleDeleteCronJob(w http.ResponseWriter, r *http.Request, name string) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	jobs := append([]config.CronJob(nil), cfg.CronJobs.Jobs...)
	found := false
	filtered := make([]config.CronJob, 0, len(jobs))
	for _, j := range jobs {
		if strings.EqualFold(j.Name, name) {
			found = true
			continue
		}
		filtered = append(filtered, j)
	}

	if !found {
		writeError(w, http.StatusNotFound, "cronjob not found")
		return
	}

	updated := config.CronJobsConfig{
		Enabled: cfg.CronJobs.Enabled,
		Jobs:    filtered,
	}

	if err := config.UpdateCronJobs(updated); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete cronjob: "+err.Error())
		return
	}

	if s.app.CronService != nil {
		cfg2 := config.Get()
		if cfg2 != nil {
			_ = s.app.CronService.Reload(cfg2.CronJobs)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRunCronJobNow handles POST /api/v1/cronjobs/{name}/run.
func (s *Server) handleRunCronJobNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.CronService == nil {
		writeError(w, http.StatusServiceUnavailable, "cronjob service is not enabled")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name path parameter required")
		return
	}

	task, err := s.app.CronService.RunNow(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to run cronjob: "+err.Error())
		return
	}

	taskID := ""
	if task != nil {
		taskID = task.ID
	}

	writeJSON(w, http.StatusOK, map[string]string{"taskId": taskID})
}
