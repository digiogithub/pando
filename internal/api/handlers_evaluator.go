package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/llm/prompt"
)

// EvaluatorMetrics is the JSON representation of aggregated evaluator statistics.
type EvaluatorMetrics struct {
	TotalSessions  int64   `json:"total_sessions"`
	TotalTemplates int64   `json:"total_templates"`
	AvgReward      float64 `json:"avg_reward"`
	ActiveSkills   int64   `json:"active_skills"`
	IsEnabled      bool    `json:"is_enabled"`
}

// TemplateVariantResponse is one prompt template variant of a section with its
// UCB statistics. The embedded template is the variant "default" (no path).
type TemplateVariantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	IsDefault bool   `json:"is_default"`
	// Missing marks a variant that has statistics but no file any more.
	Missing   bool    `json:"missing,omitempty"`
	TimesUsed int64   `json:"times_used"`
	AvgReward float64 `json:"avg_reward"`
	UCBScore  float64 `json:"ucb_score"`
}

// TemplateSectionResponse groups the variants of one prompt section.
type TemplateSectionResponse struct {
	Section  string                    `json:"section"`
	Variants []TemplateVariantResponse `json:"variants"`
}

// SkillResponse is the JSON representation of a skill library entry.
type SkillResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TaskType    string `json:"task_type"`
	// Confidence is the judge's confidence when it proposed the skill.
	Confidence float64 `json:"confidence"`
	Uses       int64   `json:"uses"`
	// Status is the review state: pending, approved or rejected. Only approved
	// skills are injected into prompts.
	Status string `json:"status"`
	// SuccessRate is the mean reward of the EvalCount evaluated sessions the
	// skill was injected in.
	SuccessRate   float64 `json:"success_rate"`
	EvalCount     int64   `json:"eval_count"`
	JudgeModel    string  `json:"judge_model,omitempty"`
	SourceSession string  `json:"source_session,omitempty"`
	Created       int64   `json:"created,omitempty"`
}

func skillResponseFrom(sk evaluator.Skill) SkillResponse {
	r := SkillResponse{
		ID: sk.ID, Name: sk.Title, Description: sk.Content, TaskType: sk.TaskType,
		Confidence: sk.Confidence, Uses: int64(sk.UsageCount), Status: sk.Status,
		SuccessRate: sk.SuccessRate, EvalCount: int64(sk.EvalCount),
		JudgeModel: sk.JudgeModel, SourceSession: sk.SourceSession,
	}
	if !sk.Created.IsZero() {
		r.Created = sk.Created.Unix()
	}
	return r
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

	var templateCount int64
	if variantStats, err := q.ListAllVariantStats(r.Context()); err == nil {
		templateCount = int64(len(variantStats))
	}

	writeJSON(w, http.StatusOK, EvaluatorMetrics{
		TotalSessions:  stats.TotalEvaluations,
		TotalTemplates: templateCount,
		AvgReward:      stats.AvgReward,
		ActiveSkills:   stats.ActiveSkills,
		IsEnabled:      isEnabled,
	})
}

// handleGetEvaluatorTemplates handles GET /api/v1/evaluator/templates. It lists,
// per prompt section that has variant files or statistics, the default
// (embedded) variant and the variant files with times used, average reward and
// UCB score.
func (s *Server) handleGetEvaluatorTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var stats []db.PromptVariantStat
	if s.config.DB != nil {
		if rows, err := db.New(s.config.DB).ListAllVariantStats(r.Context()); err == nil {
			stats = rows
		}
	}
	explorationC := 1.41
	if cfg := config.Get(); cfg != nil && cfg.Evaluator.ExplorationC > 0 {
		explorationC = cfg.Evaluator.ExplorationC
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sections": buildTemplateSections(prompt.DiscoverAllVariants(prompt.VariantRoots(config.WorkingDirectory())), stats, explorationC),
	})
}

// buildTemplateSections merges the variant files on disk with the persisted
// statistics into per-section rankings.
func buildTemplateSections(files []prompt.VariantFile, stats []db.PromptVariantStat, explorationC float64) []TemplateSectionResponse {
	type entry struct {
		resp TemplateVariantResponse
	}
	sections := make(map[string]map[string]*entry)
	add := func(section, id, name, path string, isDefault, missing bool) {
		if sections[section] == nil {
			sections[section] = make(map[string]*entry)
		}
		if _, ok := sections[section][id]; !ok {
			sections[section][id] = &entry{resp: TemplateVariantResponse{ID: id, Name: name, Path: path, IsDefault: isDefault, Missing: missing}}
		}
	}
	for _, f := range files {
		add(f.Section, prompt.VariantID(f.Section, prompt.DefaultVariant), prompt.DefaultVariant, "", true, false)
		add(f.Section, f.ID, f.Name, f.Path, false, false)
	}
	for _, st := range stats {
		name := st.VariantID
		if i := strings.LastIndex(name, "#"); i >= 0 {
			name = name[i+1:]
		}
		add(st.Section, st.VariantID, name, "", name == prompt.DefaultVariant, name != prompt.DefaultVariant)
	}

	statByID := make(map[string]db.PromptVariantStat, len(stats))
	totals := make(map[string]int64)
	for _, st := range stats {
		statByID[st.VariantID] = st
		totals[st.Section] += st.TimesUsed
	}

	out := make([]TemplateSectionResponse, 0, len(sections))
	for section, variants := range sections {
		sec := TemplateSectionResponse{Section: section, Variants: make([]TemplateVariantResponse, 0, len(variants))}
		for id, e := range variants {
			st := statByID[id]
			e.resp.TimesUsed = st.TimesUsed
			e.resp.AvgReward = st.AvgReward
			e.resp.UCBScore = evaluator.UCBScore(st.AvgReward, int(totals[section]), int(st.TimesUsed), explorationC)
			if e.resp.TimesUsed == 0 {
				e.resp.UCBScore = 0 // not-yet-tried is "infinite" in UCB; the UI shows a dash
			}
			sec.Variants = append(sec.Variants, e.resp)
		}
		sort.Slice(sec.Variants, func(i, j int) bool {
			if sec.Variants[i].IsDefault != sec.Variants[j].IsDefault {
				return sec.Variants[i].IsDefault
			}
			return sec.Variants[i].Name < sec.Variants[j].Name
		})
		out = append(out, sec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Section < out[j].Section })
	return out
}

// handleGetEvaluatorSkills handles GET /api/v1/evaluator/skills[?status=pending|approved|rejected].
// It lists the learned skills (reviewable files plus statistics); without a
// status filter every status is returned.
func (s *Server) handleGetEvaluatorSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !evaluator.ValidSkillStatus(status) {
		writeError(w, http.StatusBadRequest, "status must be pending, approved or rejected")
		return
	}

	skills := make([]SkillResponse, 0)
	if s.app != nil && s.app.Evaluator != nil && s.app.Evaluator.IsEnabled() {
		list, err := s.app.Evaluator.ListSkills(r.Context(), status, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, sk := range list {
			skills = append(skills, skillResponseFrom(sk))
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"skills": skills})
}

// handleReviewEvaluatorSkill handles POST /api/v1/evaluator/skills/{id}/approve
// and /reject. Approving takes effect on the next new session only.
func (s *Server) handleReviewEvaluatorSkill(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.app == nil || s.app.Evaluator == nil || !s.app.Evaluator.IsEnabled() {
			writeError(w, http.StatusConflict, "evaluator is not enabled")
			return
		}
		sk, err := s.app.Evaluator.ReviewSkill(r.Context(), r.PathValue("id"), status)
		if err != nil {
			code := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "ambiguous") {
				code = http.StatusNotFound
			}
			writeError(w, code, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, skillResponseFrom(*sk))
	}
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
