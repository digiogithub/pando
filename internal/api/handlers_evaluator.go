package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
)

// EvaluatorMetrics is the JSON representation of aggregated evaluator statistics.
type EvaluatorMetrics struct {
	TotalSessions  int64   `json:"total_sessions"`
	TotalTemplates int64   `json:"total_templates"`
	AvgReward      float64 `json:"avg_reward"`
	ActiveSkills   int64   `json:"active_skills"`
	IsEnabled      bool    `json:"is_enabled"`
}

// TemplateResponse is the JSON representation of a prompt template with UCB stats.
type TemplateResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Section   string  `json:"section"`
	UCBScore  float64 `json:"ucb_score"`
	WinRate   float64 `json:"win_rate"`
	Uses      int64   `json:"uses"`
	IsDefault bool    `json:"is_default"`
}

// SkillResponse is the JSON representation of a skill library entry.
type SkillResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	TaskType    string  `json:"task_type"`
	Confidence  float64 `json:"confidence"`
	Uses        int64   `json:"uses"`
}

// EvaluatorSessionResponse is the JSON representation of a evaluated session score.
type EvaluatorSessionResponse struct {
	ID              string  `json:"id"`
	SessionID       string  `json:"session_id"`
	TemplateID      string  `json:"template_id,omitempty"`
	Reward          float64 `json:"reward"`
	SuccessScore    float64 `json:"success_score"`
	EfficiencyScore float64 `json:"efficiency_score"`
	MessageCount    int64   `json:"message_count"`
	EvaluatedAt     int64   `json:"evaluated_at"`
	// Components is the persisted reward decomposition (per-signal scores,
	// weights, correction pattern hits, explicit feedback).
	Components json.RawMessage `json:"components"`
	// JudgeAnalysis is the stored LLM judge output (reasoning, key points,
	// skill proposal); null when the judge did not run for this session.
	JudgeAnalysis         json.RawMessage `json:"judge_analysis"`
	JudgeModel            string          `json:"judge_model,omitempty"`
	JudgePromptTokens     int64           `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int64           `json:"judge_completion_tokens"`
}

// handleGetEvaluatorMetrics handles GET /api/v1/evaluator/metrics.
func (s *Server) handleGetEvaluatorMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	isEnabled := s.app.Evaluator != nil && s.app.Evaluator.IsEnabled()

	if s.config.DB == nil {
		writeJSON(w, http.StatusOK, EvaluatorMetrics{IsEnabled: isEnabled})
		return
	}

	q := db.New(s.config.DB)

	stats, err := q.GetEvaluatorStats(r.Context())
	if err != nil {
		// DB might not have data yet; return safe empty metrics.
		writeJSON(w, http.StatusOK, EvaluatorMetrics{IsEnabled: isEnabled})
		return
	}

	templateCount, err := q.CountPromptTemplates(r.Context())
	if err != nil {
		templateCount = 0
	}

	writeJSON(w, http.StatusOK, EvaluatorMetrics{
		TotalSessions:  stats.TotalEvaluations,
		TotalTemplates: templateCount,
		AvgReward:      stats.AvgReward,
		ActiveSkills:   stats.ActiveSkills,
		IsEnabled:      isEnabled,
	})
}

// handleGetEvaluatorTemplates handles GET /api/v1/evaluator/templates.
func (s *Server) handleGetEvaluatorTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.config.DB == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"templates": []interface{}{}})
		return
	}

	q := db.New(s.config.DB)

	rows, err := q.ListUCBRanking(r.Context())
	if err != nil {
		// TODO: implement when DB queries are ready
		writeJSON(w, http.StatusOK, map[string]interface{}{"templates": []interface{}{}})
		return
	}

	templates := make([]TemplateResponse, 0, len(rows))
	for _, row := range rows {
		templates = append(templates, TemplateResponse{
			ID:        row.ID,
			Name:      row.Name,
			Section:   row.Section,
			UCBScore:  row.UcbScore,
			WinRate:   row.AvgReward,
			Uses:      row.TimesUsed,
			IsDefault: row.IsDefault == 1,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": templates})
}

// handleGetEvaluatorSkills handles GET /api/v1/evaluator/skills.
func (s *Server) handleGetEvaluatorSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.config.DB == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"skills": []interface{}{}})
		return
	}

	q := db.New(s.config.DB)

	rows, err := q.ListAllActiveSkills(r.Context())
	if err != nil {
		// TODO: implement when DB queries are ready
		writeJSON(w, http.StatusOK, map[string]interface{}{"skills": []interface{}{}})
		return
	}

	skills := make([]SkillResponse, 0, len(rows))
	for _, row := range rows {
		skills = append(skills, SkillResponse{
			ID:          row.ID,
			Name:        row.Title,
			Description: row.Content,
			TaskType:    row.TaskType,
			Confidence:  row.SuccessRate,
			Uses:        row.UsageCount,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"skills": skills})
}

// handleGetEvaluatorSessions handles GET /api/v1/evaluator/sessions.
func (s *Server) handleGetEvaluatorSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.config.DB == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": []interface{}{}})
		return
	}

	q := db.New(s.config.DB)

	rows, err := q.ListSessionScores(r.Context(), 50)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": []interface{}{}})
		return
	}

	sessions := make([]EvaluatorSessionResponse, 0, len(rows))
	for _, row := range rows {
		templateID := ""
		if row.TemplateID.Valid {
			templateID = row.TemplateID.String
		}
		sessions = append(sessions, EvaluatorSessionResponse{
			ID:              row.ID,
			SessionID:       row.SessionID,
			TemplateID:      templateID,
			Reward:          row.Reward,
			SuccessScore:    row.SuccessScore,
			EfficiencyScore: row.EfficiencyScore,
			MessageCount:    row.MessageCount,
			EvaluatedAt:     row.EvaluatedAt,
			Components:      componentsJSON(row.Components),
			JudgeAnalysis:   judgeAnalysisJSON(row.JudgeAnalysis),
			JudgeModel:      row.JudgeModel.String,

			JudgePromptTokens:     row.JudgePromptTokens,
			JudgeCompletionTokens: row.JudgeCompletionTokens,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

// EvaluateSessionResponse is the JSON result of a manual session evaluation.
type EvaluateSessionResponse struct {
	SessionID       string  `json:"session_id"`
	Skipped         string  `json:"skipped,omitempty"`
	Reward          float64 `json:"reward"`
	SuccessScore    float64 `json:"success_score"`
	EfficiencyScore float64 `json:"efficiency_score"`
	UserCorrections int     `json:"user_corrections"`
	MessageCount    int64   `json:"message_count"`
	Judged          bool    `json:"judged"`
}

// handleEvaluateSession handles POST /api/v1/evaluator/sessions/{id}/evaluate.
// It evaluates the session synchronously, bypassing the completion guards.
func (s *Server) handleEvaluateSession(w http.ResponseWriter, r *http.Request) {
	if s.app.Evaluator == nil || !s.app.Evaluator.IsEnabled() {
		writeError(w, http.StatusConflict, "evaluator is not enabled")
		return
	}
	res, err := s.app.Evaluator.EvaluateNow(r.Context(), r.PathValue("id"), evaluator.EvaluateOptions{Force: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, EvaluateSessionResponse{
		SessionID:       res.SessionID,
		Skipped:         res.Skipped,
		Reward:          res.Reward.Total,
		SuccessScore:    res.Reward.SuccessScore,
		EfficiencyScore: res.Reward.EfficiencyScore,
		UserCorrections: res.Reward.UserCorrections,
		MessageCount:    res.Reward.MessageCount,
		Judged:          res.Judged,
	})
}

// judgeAnalysisJSON returns the stored judge output as raw JSON, or JSON null
// when the judge did not run or the stored value is invalid.
func judgeAnalysisJSON(raw sql.NullString) json.RawMessage {
	if !raw.Valid || raw.String == "" || !json.Valid([]byte(raw.String)) {
		return json.RawMessage("null")
	}
	return json.RawMessage(raw.String)
}

// componentsJSON returns the stored decomposition as raw JSON, falling back to
// an empty object for legacy rows or invalid content.
func componentsJSON(raw string) json.RawMessage {
	if raw == "" || !json.Valid([]byte(raw)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(raw)
}

// SessionFeedbackRequest is the body of POST /api/v1/evaluator/sessions/{id}/feedback.
type SessionFeedbackRequest struct {
	Rating string `json:"rating"` // "good" | "bad"
	Note   string `json:"note,omitempty"`
}

// handleSessionFeedback records explicit feedback for a session and re-scores it.
func (s *Server) handleSessionFeedback(w http.ResponseWriter, r *http.Request) {
	if s.app.Evaluator == nil || !s.app.Evaluator.IsEnabled() {
		writeError(w, http.StatusConflict, "evaluator is not enabled")
		return
	}
	var req SessionFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := evaluator.ParseFeedbackRating(req.Rating); !ok {
		writeError(w, http.StatusBadRequest, "rating must be good or bad")
		return
	}
	res, err := s.app.Evaluator.RecordFeedback(r.Context(), r.PathValue("id"), req.Rating, req.Note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, EvaluateSessionResponse{
		SessionID:       res.SessionID,
		Skipped:         res.Skipped,
		Reward:          res.Reward.Total,
		SuccessScore:    res.Reward.SuccessScore,
		EfficiencyScore: res.Reward.EfficiencyScore,
		UserCorrections: res.Reward.UserCorrections,
		MessageCount:    res.Reward.MessageCount,
	})
}
