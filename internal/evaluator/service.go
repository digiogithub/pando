package evaluator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
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
	db       *sql.DB
	patterns []*regexp.Regexp
	mu       sync.Mutex
	// sessionTemplates maps sessionID -> templateID for the current session
	sessionTemplates sync.Map
}

// New creates a new EvaluatorService. Returns nil if disabled.
func New(cfg config.EvaluatorConfig, db *sql.DB) (*EvaluatorService, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	patterns, err := compilePatterns(cfg.CorrectionsPatterns)
	if err != nil {
		return nil, fmt.Errorf("evaluator: compile correction patterns: %w", err)
	}

	return &EvaluatorService{
		cfg:      cfg,
		db:       db,
		patterns: patterns,
	}, nil
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
	// TODO: implement full evaluation pipeline in Phase 4/5 integration
	// Steps:
	// 1. Load messages for sessionID from DB
	// 2. calculateReward(messages, s.patterns, baseline, s.cfg.AlphaWeight, s.cfg.BetaWeight)
	// 3. Get selected template from s.sessionTemplates
	// 4. Insert session_scores row via SQLC query
	// 5. If reward.Total > 0.5: call LLM judge, save skill if confidence > 0.7
	slog.Debug("evaluator: runEvaluation stub", "session_id", sessionID)
	return nil
}

// SelectTemplate returns the best template for a section using UCB.
func (s *EvaluatorService) SelectTemplate(ctx context.Context, sectionName string) (*PromptTemplate, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, nil
	}
	// TODO: implement UCB selection via SQLC queries in Phase 5
	// 1. Count total session_scores
	// 2. If count < MinSessionsForUCB, return nil (use default)
	// 3. List active templates by section with UCB stats
	// 4. Apply UCBScore() to each, return highest
	return nil, nil
}

// GetActiveSkills returns active skills for a task type.
func (s *EvaluatorService) GetActiveSkills(ctx context.Context, taskType string) ([]Skill, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, nil
	}
	// TODO: implement via SQLC queries in Phase 5
	return nil, nil
}

// GetStats returns system statistics for TUI display.
func (s *EvaluatorService) GetStats(ctx context.Context) (*Stats, error) {
	if s == nil {
		return &Stats{IsEnabled: false}, nil
	}
	// TODO: implement via SQLC queries in Phase 5
	return &Stats{
		IsEnabled: s.cfg.Enabled,
	}, nil
}
