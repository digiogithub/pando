package evaluator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/message"
)

// Service defines the evaluator interface used by other packages.
type Service interface {
	// EvaluateSession triggers evaluation of a completed session (async if configured).
	EvaluateSession(ctx context.Context, sessionID string) error

	// SelectTemplate returns the best prompt template for a section using UCB.
	// Returns nil if insufficient history or evaluator disabled.
	SelectTemplate(ctx context.Context, sectionName string) (*PromptTemplate, error)

	// GetActiveSkills returns skills to inject into prompts for a given task type.
	GetActiveSkills(ctx context.Context, taskType string) ([]Skill, error)

	// GetStats returns current UCB rankings and skill library summary.
	GetStats(ctx context.Context) (*Stats, error)

	// IsEnabled returns whether the evaluator is active.
	IsEnabled() bool

	// RecordTemplateSelection records which template was selected for a session.
	RecordTemplateSelection(ctx context.Context, sessionID, templateID string)
}

// EvaluatorService is the concrete implementation of Service.
type EvaluatorService struct {
	cfg      config.EvaluatorConfig
	db       db.Querier
	msgs     message.Service
	judge    *Judge
	patterns []*regexp.Regexp
	mu       sync.Mutex
	// sessionTemplates maps sessionID -> templateID for the current session
	sessionTemplates sync.Map
}

// New creates a new EvaluatorService. Returns nil if disabled.
func New(cfg config.EvaluatorConfig, q db.Querier, msgs message.Service) (*EvaluatorService, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	patterns, err := compilePatterns(cfg.CorrectionsPatterns)
	if err != nil {
		return nil, fmt.Errorf("evaluator: compile correction patterns: %w", err)
	}

	svc := &EvaluatorService{
		cfg:      cfg,
		db:       q,
		msgs:     msgs,
		patterns: patterns,
	}

	// Create judge (soft failure: log warning, continue without judge).
	if cfg.Model != "" {
		j, err := newJudge(cfg)
		if err != nil {
			slog.Warn("evaluator: judge init failed, continuing without LLM judge", "err", err)
		} else {
			svc.judge = j
		}
	}

	return svc, nil
}

// IsEnabled returns whether the evaluator is active.
func (s *EvaluatorService) IsEnabled() bool {
	return s != nil && s.cfg.Enabled
}

// RecordTemplateSelection stores the template used in this session for later evaluation.
func (s *EvaluatorService) RecordTemplateSelection(_ context.Context, sessionID, templateID string) {
	if s == nil {
		return
	}
	s.sessionTemplates.Store(sessionID, templateID)
}

// EvaluateSession triggers evaluation of a completed session.
func (s *EvaluatorService) EvaluateSession(ctx context.Context, sessionID string) error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	if s.cfg.Async {
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := s.runEvaluation(bgCtx, sessionID); err != nil {
				slog.Warn("evaluator: evaluation failed", "session_id", sessionID, "err", err)
			}
		}()
		return nil
	}
	return s.runEvaluation(ctx, sessionID)
}

// runEvaluation performs the actual evaluation logic.
func (s *EvaluatorService) runEvaluation(ctx context.Context, sessionID string) error {
	// Idempotency: skip if already evaluated
	if _, err := s.db.GetSessionScore(ctx, sessionID); err == nil {
		slog.Debug("evaluator: session already evaluated, skipping", "session_id", sessionID)
		return nil
	}

	// Load messages
	msgs, err := s.msgs.List(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("evaluator: load messages: %w", err)
	}

	// Convert to messageInfo for reward calculation (text + correction detection)
	msgInfos := messagesToInfo(msgs)

	// Get session-level token totals (stored at session level, not per-message)
	if sess, err := s.db.GetSessionByID(ctx, sessionID); err == nil {
		// Append a synthetic entry carrying the full token counts
		msgInfos = append(msgInfos, messageInfo{
			promptTokens:     sess.PromptTokens,
			completionTokens: sess.CompletionTokens,
		})
	}

	// Get rolling token baseline
	baseline, _ := s.db.GetTokenBaseline(ctx, int64(s.cfg.MaxTokensBaseline))

	// Calculate reward
	reward := calculateReward(msgInfos, s.patterns, baseline, s.cfg.AlphaWeight, s.cfg.BetaWeight)

	// Get template used for this session
	var templateID sql.NullString
	if tid, ok := s.sessionTemplates.Load(sessionID); ok {
		templateID = sql.NullString{String: tid.(string), Valid: true}
	}
	defer s.sessionTemplates.Delete(sessionID)

	// Persist session score
	scoreID := uuid.New().String()
	_, err = s.db.InsertSessionScore(ctx, db.InsertSessionScoreParams{
		ID:               scoreID,
		SessionID:        sessionID,
		TemplateID:       templateID,
		Reward:           reward.Total,
		SuccessScore:     reward.SuccessScore,
		EfficiencyScore:  reward.EfficiencyScore,
		JudgeAnalysis:    sql.NullString{},
		JudgeModel:       sql.NullString{},
		PromptTokens:     reward.PromptTokens,
		CompletionTokens: reward.CompletionTokens,
		MessageCount:     reward.MessageCount,
		UserCorrections:  int64(reward.UserCorrections),
	})
	if err != nil {
		return fmt.Errorf("evaluator: insert session score: %w", err)
	}

	slog.Info("evaluator: session evaluated",
		"session_id", sessionID,
		"reward", reward.Total,
		"success_score", reward.SuccessScore,
		"efficiency_score", reward.EfficiencyScore,
		"corrections", reward.UserCorrections,
	)

	// Call LLM judge for moderately successful sessions (avoid wasting tokens on failures).
	if s.judge != nil && (reward.Total > 0.5 || reward.SuccessScore == 1.0) {
		transcript := buildTranscript(msgs)
		templateName := "default"
		if templateID.Valid {
			templateName = templateID.String
		}
		meta := JudgeMeta{
			TemplateName:    templateName,
			TemplateVersion: 1,
			Corrections:     reward.UserCorrections,
			Tokens:          reward.PromptTokens + reward.CompletionTokens,
			Transcript:      transcript,
		}
		judgeOut, judgeErr := s.judge.Evaluate(ctx, meta, s.cfg.JudgePromptTemplate)
		if judgeErr != nil {
			slog.Warn("evaluator: judge call failed", "session_id", sessionID, "err", judgeErr)
		} else if judgeOut != nil {
			slog.Debug("evaluator: judge output", "session_id", sessionID, "confidence", judgeOut.Confidence, "task_type", judgeOut.TaskType)
			if err := s.saveSkillFromJudge(ctx, judgeOut, sessionID, templateID.String); err != nil {
				slog.Warn("evaluator: save skill failed", "session_id", sessionID, "err", err)
			}
		}
	}

	return nil
}

// SelectTemplate returns the best template for a section using UCB.
func (s *EvaluatorService) SelectTemplate(ctx context.Context, sectionName string) (*PromptTemplate, error) {
	if s == nil || !s.cfg.Enabled || s.db == nil {
		return nil, nil
	}

	total, err := s.db.CountSessionScores(ctx)
	if err != nil || int(total) < s.cfg.MinSessionsForUCB {
		return nil, nil // not enough history yet
	}

	templates, err := s.db.ListActiveTemplatesBySection(ctx, sectionName)
	if err != nil || len(templates) == 0 {
		return nil, nil
	}

	var best *db.ListActiveTemplatesBySectionRow
	bestScore := -1.0

	for i, t := range templates {
		score := UCBScore(t.AvgReward, int(total), int(t.TimesUsed), s.cfg.ExplorationC)
		if score > bestScore {
			bestScore = score
			tmp := templates[i]
			best = &tmp
		}
	}

	if best == nil {
		return nil, nil
	}

	return &PromptTemplate{
		ID:        best.ID,
		Name:      best.Name,
		Section:   best.Section,
		Content:   best.Content,
		Version:   int(best.Version),
		IsDefault: best.IsDefault == 1,
	}, nil
}

// GetActiveSkills returns active skills for a task type.
func (s *EvaluatorService) GetActiveSkills(ctx context.Context, taskType string) ([]Skill, error) {
	if s == nil || !s.cfg.Enabled || s.db == nil {
		return nil, nil
	}

	if taskType == "" {
		taskType = "general"
	}

	rows, err := s.db.ListActiveSkillsByType(ctx, db.ListActiveSkillsByTypeParams{
		TaskType: taskType,
		Limit:    10,
	})
	if err != nil {
		return nil, fmt.Errorf("evaluator: list active skills: %w", err)
	}

	skills := make([]Skill, 0, len(rows))
	for _, r := range rows {
		skills = append(skills, Skill{
			ID:          r.ID,
			Title:       r.Title,
			Content:     r.Content,
			TaskType:    r.TaskType,
			SuccessRate: r.SuccessRate,
			UsageCount:  int(r.UsageCount),
		})
	}

	// Increment usage for retrieved skills (fire-and-forget, non-blocking).
	go func(ids []string) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, id := range ids {
			_ = s.db.IncrementSkillUsage(bgCtx, id)
		}
	}(func() []string {
		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		return ids
	}())

	return skills, nil
}

// GetStats returns system statistics for TUI display.
func (s *EvaluatorService) GetStats(ctx context.Context) (*Stats, error) {
	if s == nil {
		return &Stats{IsEnabled: false}, nil
	}
	if !s.cfg.Enabled || s.db == nil {
		return &Stats{IsEnabled: s.cfg.Enabled}, nil
	}

	aggr, err := s.db.GetEvaluatorStats(ctx)
	if err != nil {
		return &Stats{IsEnabled: true}, fmt.Errorf("evaluator: get stats: %w", err)
	}

	ranking, _ := s.db.ListUCBRanking(ctx)
	allSkills, _ := s.db.ListAllActiveSkills(ctx)

	templateStats := make([]TemplateStats, 0, len(ranking))
	for i, r := range ranking {
		score := UCBScore(r.AvgReward, int(aggr.TotalEvaluations), int(r.TimesUsed), s.cfg.ExplorationC)
		templateStats = append(templateStats, TemplateStats{
			Template: PromptTemplate{
				ID:      r.ID,
				Name:    r.Name,
				Section: r.Section,
				Version: int(r.Version),
			},
			TimesUsed: int(r.TimesUsed),
			AvgReward: r.AvgReward,
			UCBScore:  score,
			Rank:      i + 1,
		})
	}

	topSkills := make([]Skill, 0, len(allSkills))
	for _, sk := range allSkills {
		topSkills = append(topSkills, Skill{
			ID:          sk.ID,
			Title:       sk.Title,
			Content:     sk.Content,
			TaskType:    sk.TaskType,
			SuccessRate: sk.SuccessRate,
			UsageCount:  int(sk.UsageCount),
		})
	}

	var lastEval time.Time
	if aggr.LastEvaluation.Valid {
		lastEval = time.Unix(aggr.LastEvaluation.Int64, 0)
	}

	return &Stats{
		TotalEvaluations: int(aggr.TotalEvaluations),
		Templates:        templateStats,
		SkillCount:       int(aggr.ActiveSkills),
		TopSkills:        topSkills,
		AvgReward:        aggr.AvgReward,
		LastEvaluation:   lastEval,
		IsEnabled:        true,
	}, nil
}

// messagesToInfo converts message.Message slice to evaluator messageInfo slice.
// Tokens are left at 0 here; session-level token counts are fetched separately.
func messagesToInfo(msgs []message.Message) []messageInfo {
	infos := make([]messageInfo, 0, len(msgs))
	for _, m := range msgs {
		info := messageInfo{
			isUser: m.Role == message.User,
			text:   m.Content().Text,
		}
		infos = append(infos, info)
	}
	return infos
}

// buildTranscript formats messages as a readable transcript for the judge.
// Tool results are truncated to avoid huge payloads.
func buildTranscript(msgs []message.Message) string {
	const maxToolLen = 500
	var sb strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case message.User:
			sb.WriteString("User: ")
			sb.WriteString(m.Content().Text)
			sb.WriteString("\n")
		case message.Assistant:
			sb.WriteString("Assistant: ")
			sb.WriteString(m.Content().Text)
			for _, tc := range m.ToolCalls() {
				fmt.Fprintf(&sb, "\n  [tool_call: %s]", tc.Name)
			}
			sb.WriteString("\n")
		}
		for _, tr := range m.ToolResults() {
			content := tr.Content
			if len(content) > maxToolLen {
				content = content[:maxToolLen] + "...[truncated]"
			}
			fmt.Fprintf(&sb, "  [tool_result: %s] %s\n", tr.Name, content)
		}
	}
	return sb.String()
}

// saveSkillFromJudge persists a new skill from judge output if confidence is high enough.
// Enforces MaxSkills by deactivating the lowest-performing skill before inserting.
func (s *EvaluatorService) saveSkillFromJudge(ctx context.Context, out *JudgeOutput, sessionID, templateID string) error {
	if out == nil || out.NewSkill == "" || out.Confidence < 0.7 {
		return nil
	}

	taskType := out.TaskType
	if taskType == "" {
		taskType = "general"
	}

	// Enforce MaxSkills limit.
	count, err := s.db.CountActiveSkills(ctx)
	if err == nil && int(count) >= s.cfg.MaxSkills {
		if err := s.db.DeactivateLowestSkill(ctx); err != nil {
			slog.Warn("evaluator: deactivate lowest skill failed", "err", err)
		}
	}

	srcSession := sql.NullString{String: sessionID, Valid: sessionID != ""}
	srcTemplate := sql.NullString{String: templateID, Valid: templateID != ""}

	_, err = s.db.InsertSkill(ctx, db.InsertSkillParams{
		ID:               uuid.New().String(),
		Title:            taskType + " skill",
		Content:          out.NewSkill,
		SourceSessionID:  srcSession,
		SourceTemplateID: srcTemplate,
		TaskType:         taskType,
	})
	if err != nil {
		return fmt.Errorf("evaluator: insert skill: %w", err)
	}

	slog.Info("evaluator: new skill saved", "task_type", taskType, "confidence", out.Confidence)
	return nil
}
