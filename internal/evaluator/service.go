package evaluator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/skills"
)

// Service defines the evaluator interface used by other packages.
type Service interface {
	// EvaluateSession triggers evaluation of a session on explicit request
	// (async if configured). It bypasses the completion guards of MarkCompleted.
	EvaluateSession(ctx context.Context, sessionID string) error

	// MarkCompleted is the entry point for session-completion triggers (session
	// switch, idle sweep, shutdown). It applies the evaluation guards (minimum
	// user turns, subagent sessions, idempotency, in-flight dedupe) and
	// evaluates asynchronously when configured. reason is only used for logging.
	MarkCompleted(ctx context.Context, sessionID, reason string) error

	// SelectVariant returns the prompt variant id chosen for a section of a
	// session among candidates (candidates[0] is the default variant). The choice
	// is made once per (session, section), persisted and frozen; it is the
	// default when the feature is off or there is nothing to choose from.
	SelectVariant(ctx context.Context, sessionID, section string, candidates []string) (string, error)

	// GetActiveSkills returns the approved skills for a task type.
	GetActiveSkills(ctx context.Context, taskType string) ([]Skill, error)

	// ListSkills returns the learned skills (files + statistics), optionally
	// filtered by status (pending|approved|rejected) and task type.
	ListSkills(ctx context.Context, status, taskType string) ([]Skill, error)

	// ReviewSkill approves or rejects a learned skill by id.
	ReviewSkill(ctx context.Context, id, status string) (*Skill, error)

	// GetStats returns current UCB rankings and skill library summary.
	GetStats(ctx context.Context) (*Stats, error)

	// IsEnabled returns whether the evaluator is active.
	IsEnabled() bool

	// ClassifyTask returns a task type label from the user's first message.
	// Returns "general" if no pattern matches.
	ClassifyTask(text string) string
}

// EvaluatorService is the concrete implementation of Service.
type EvaluatorService struct {
	cfg          config.EvaluatorConfig
	db           db.Querier
	msgs         message.Service
	judge        *Judge
	patterns     []*regexp.Regexp
	taskPatterns []compiledPattern
	mu           sync.Mutex
	// inflight holds the session IDs being evaluated right now (dedupe).
	inflight map[string]struct{}
	// failed holds sessions the background sweeps must not retry in this process.
	failed sync.Map
	// wg tracks async evaluations so Flush can wait for them on shutdown.
	wg sync.WaitGroup
	// selMu guards selections: sessionID -> section -> frozen variant id, an
	// in-memory copy of session_template_selections so a prompt build does not
	// query the DB for every section on every turn.
	selMu      sync.Mutex
	selections map[string]map[string]string
	// skillFileMu serialises access to the learned-skill files and their mirror.
	skillFileMu sync.Mutex
	// skillSelMu guards skillSel and serialises the first-build selection so a
	// session's usage counts are incremented once. skillSel caches the frozen
	// per-session skill set (mirror of session_skill_injections).
	skillSelMu sync.Mutex
	skillSel   map[string][]Skill
	// workDirOverride, when set, replaces the configured working directory.
	workDirOverride string
	// judgeMu serialises judge calls so the daily budget check and the call
	// that spends it cannot interleave.
	judgeMu sync.Mutex
	// budgetWarnedDay is the local date ("2006-01-02") for which the
	// budget-exhausted warning was already logged.
	budgetWarnedDay string
	// lastJudgeErr is the most recent judge failure (init or call).
	lastJudgeErr *JudgeError
	// lastEvalErr is the most recent evaluation failure since the process started.
	lastEvalErr *JudgeError
	// bg is the background (idle sweeper / backfill) state, see BackgroundState.
	bg BackgroundState
	// onFirstPass, when set, runs once after the first background pass on the
	// primary instance (the startup diagnostic).
	onFirstPass func(ctx context.Context)
}

// JudgeError is the most recent judge failure, kept in memory for diagnostics.
type JudgeError struct {
	Message string
	At      time.Time
}

// LastJudgeError returns the most recent judge failure (initialisation or
// call) since the process started, or nil when there was none.
func (s *EvaluatorService) LastJudgeError() *JudgeError {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastJudgeErr == nil {
		return nil
	}
	e := *s.lastJudgeErr
	return &e
}

// LastEvaluationError returns the most recent evaluation failure since the
// process started, or nil when there was none.
func (s *EvaluatorService) LastEvaluationError() *JudgeError {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastEvalErr == nil {
		return nil
	}
	e := *s.lastEvalErr
	return &e
}

func (s *EvaluatorService) recordEvalError(err error) {
	s.mu.Lock()
	s.lastEvalErr = &JudgeError{Message: err.Error(), At: time.Now()}
	s.mu.Unlock()
}

func (s *EvaluatorService) recordJudgeError(err error) {
	s.mu.Lock()
	s.lastJudgeErr = &JudgeError{Message: err.Error(), At: time.Now()}
	s.mu.Unlock()
}

// New creates a new EvaluatorService. Returns nil if disabled.
func New(cfg config.EvaluatorConfig, q db.Querier, msgs message.Service) (*EvaluatorService, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	slog.Info("evaluator: initializing self-improvement system",
		"async", cfg.Async,
		"model", cfg.Model,
		"min_sessions_for_ucb", cfg.MinSessionsForUCB,
		"max_skills", cfg.MaxSkills,
	)

	patterns, err := compilePatterns(cfg.CorrectionsPatterns)
	if err != nil {
		return nil, fmt.Errorf("evaluator: compile correction patterns: %w", err)
	}

	svc := &EvaluatorService{
		cfg:          cfg,
		db:           q,
		msgs:         msgs,
		inflight:     make(map[string]struct{}),
		selections:   make(map[string]map[string]string),
		skillSel:     make(map[string][]Skill),
		patterns:     patterns,
		taskPatterns: compileTaskPatterns(cfg.TaskPatterns),
	}

	// Create judge (soft failure: log warning, continue without judge).
	if cfg.Model != "" {
		j, err := newJudge(cfg)
		if err != nil {
			slog.Warn("evaluator: judge init failed, continuing without LLM judge", "err", err)
			svc.recordJudgeError(err)
		} else {
			svc.judge = j
			slog.Info("evaluator: judge initialized", "model", cfg.Model)
		}
	}

	slog.Debug("evaluator: self-improvement system ready", "judge_enabled", svc.judge != nil, "correction_patterns", len(svc.patterns))

	return svc, nil
}

// IsEnabled returns whether the evaluator is active.
func (s *EvaluatorService) IsEnabled() bool {
	return s != nil && s.cfg.Enabled
}

// EvaluateSession triggers evaluation of a session on explicit request. It does
// not apply the completion guards (minimum user turns, subagent sessions).
func (s *EvaluatorService) EvaluateSession(ctx context.Context, sessionID string) error {
	return s.dispatch(ctx, sessionID, "explicit", EvaluateOptions{Force: true})
}

// MarkCompleted evaluates a session that a surface considers completed. See
// Service.MarkCompleted.
func (s *EvaluatorService) MarkCompleted(ctx context.Context, sessionID, reason string) error {
	return s.dispatch(ctx, sessionID, reason, EvaluateOptions{})
}

// dispatch runs an evaluation async or sync according to cfg.Async, deduping
// concurrent triggers for the same session.
func (s *EvaluatorService) dispatch(ctx context.Context, sessionID, reason string, opts EvaluateOptions) error {
	if s == nil || !s.cfg.Enabled || sessionID == "" {
		return nil
	}
	if !s.begin(sessionID) {
		slog.Debug("evaluator: evaluation already in flight, skipping", "session_id", sessionID, "reason", reason)
		return nil
	}
	slog.Info("evaluator: starting session evaluation", "session_id", sessionID, "reason", reason, "async", s.cfg.Async)
	if s.cfg.Async {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.end(sessionID)
			bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if _, err := s.runEvaluation(bgCtx, sessionID, opts); err != nil {
				slog.Warn("evaluator: evaluation failed", "session_id", sessionID, "err", err)
			}
		}()
		return nil
	}
	defer s.end(sessionID)
	_, err := s.runEvaluation(ctx, sessionID, opts)
	return err
}

func (s *EvaluatorService) begin(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.inflight[sessionID]; busy {
		return false
	}
	s.inflight[sessionID] = struct{}{}
	return true
}

func (s *EvaluatorService) end(sessionID string) {
	s.mu.Lock()
	delete(s.inflight, sessionID)
	s.mu.Unlock()
}

// Flush waits for in-flight async evaluations until ctx is done. Call it on
// shutdown with a short deadline so pending evaluations are not lost.
func (s *EvaluatorService) Flush(ctx context.Context) error {
	if s == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EvaluateNow evaluates a session synchronously and returns the reward
// decomposition (or the reason it was skipped). Unlike EvaluateSession it
// ignores cfg.Async.
func (s *EvaluatorService) EvaluateNow(ctx context.Context, sessionID string, opts EvaluateOptions) (*Result, error) {
	if s == nil || !s.cfg.Enabled {
		return &Result{SessionID: sessionID, Skipped: "evaluator disabled"}, nil
	}
	if !s.begin(sessionID) {
		return &Result{SessionID: sessionID, Skipped: "evaluation already in progress"}, nil
	}
	defer s.end(sessionID)
	return s.runEvaluation(ctx, sessionID, opts)
}

// feedbackSubject is the events.subject under which explicit feedback of a
// session is stored.
func feedbackSubject(sessionID string) string { return "session_feedback:" + sessionID }

// feedbackMeta is the JSON payload stored in the event metadata.
type feedbackMeta struct {
	SessionID string `json:"session_id"`
	Rating    string `json:"rating"`
	Note      string `json:"note,omitempty"`
}

// loadFeedback returns the latest explicit feedback of a session, or nil.
func (s *EvaluatorService) loadFeedback(ctx context.Context, sessionID string) *Feedback {
	raw, err := s.db.GetLatestSessionFeedback(ctx, feedbackSubject(sessionID))
	if err != nil {
		return nil
	}
	var m feedbackMeta
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	if m.Rating != FeedbackGood && m.Rating != FeedbackBad {
		return nil
	}
	return &Feedback{Rating: m.Rating, Note: m.Note}
}

// ParseFeedbackRating normalises user input ("good", "bad", "+", "up", ...).
func ParseFeedbackRating(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "good", "+", "+1", "up", "thumbsup", "positive":
		return FeedbackGood, true
	case "bad", "-", "-1", "down", "thumbsdown", "negative":
		return FeedbackBad, true
	}
	return "", false
}

// RecordFeedback stores explicit feedback for a session as an event and
// re-scores the session so the feedback dominates: bad gives a total below
// 0.3, good above 0.8. An already scored session has its score row replaced
// in place; an unscored one is scored now. Later feedback supersedes earlier.
func (s *EvaluatorService) RecordFeedback(ctx context.Context, sessionID, rating, note string) (*Result, error) {
	if s == nil || !s.cfg.Enabled {
		return nil, fmt.Errorf("evaluator is not enabled")
	}
	rating, ok := ParseFeedbackRating(rating)
	if !ok {
		return nil, fmt.Errorf("rating must be good or bad")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("no session to rate")
	}
	if _, err := s.db.GetSessionByID(ctx, sessionID); err != nil {
		return nil, fmt.Errorf("session %s not found: %w", sessionID, err)
	}
	note = strings.TrimSpace(note)
	meta, _ := json.Marshal(feedbackMeta{SessionID: sessionID, Rating: rating, Note: note})
	content := "Session feedback: " + rating
	if note != "" {
		content += " - " + note
	}
	if _, err := s.db.InsertSessionFeedbackEvent(ctx, db.InsertSessionFeedbackEventParams{
		Subject:  feedbackSubject(sessionID),
		Content:  content,
		Metadata: string(meta),
	}); err != nil {
		return nil, fmt.Errorf("store feedback: %w", err)
	}
	return s.EvaluateNow(ctx, sessionID, EvaluateOptions{Force: true, Rescore: true, SkipJudge: true})
}

// runEvaluation performs the evaluation and remembers the last failure for the
// doctor report.
func (s *EvaluatorService) runEvaluation(ctx context.Context, sessionID string, opts EvaluateOptions) (*Result, error) {
	res, err := s.evaluate(ctx, sessionID, opts)
	if err != nil {
		s.recordEvalError(err)
	}
	return res, err
}

// evaluate performs the actual evaluation logic.
func (s *EvaluatorService) evaluate(ctx context.Context, sessionID string, opts EvaluateOptions) (*Result, error) {
	res := &Result{SessionID: sessionID}
	// Idempotency: skip if already evaluated, unless explicitly re-scoring.
	existing, scoreErr := s.db.GetSessionScore(ctx, sessionID)
	rescoring := scoreErr == nil && opts.Rescore
	if scoreErr == nil && !rescoring {
		slog.Debug("evaluator: session already evaluated, skipping", "session_id", sessionID)
		res.Skipped = "already evaluated"
		return res, nil
	}

	sess, sessErr := s.db.GetSessionByID(ctx, sessionID)
	if !opts.Force && !s.cfg.IncludeSubagents && sessErr == nil && sess.ParentSessionID.Valid {
		slog.Debug("evaluator: skipping subagent session", "session_id", sessionID)
		res.Skipped = "subagent session"
		return res, nil
	}

	// Load messages
	msgs, err := s.msgs.List(ctx, sessionID)
	if err != nil {
		return res, fmt.Errorf("evaluator: load messages: %w", err)
	}
	if !opts.Force && countUserMessages(msgs) < minUserTurns {
		slog.Debug("evaluator: skipping session with too few user turns", "session_id", sessionID)
		res.Skipped = "fewer than 2 user messages"
		return res, nil
	}

	// Convert to messageInfo for reward calculation (text, tool calls/results, finish reasons).
	msgInfos := messagesToInfo(msgs)

	// Session-level token totals (stored at session level, not per message).
	var promptTokens, completionTokens int64
	if sessErr == nil {
		promptTokens, completionTokens = sess.PromptTokens, sess.CompletionTokens
	}

	// Token baseline from the sessions table (works before any score exists).
	// It is global: the task classifier needs the first user message of each
	// past session, which would cost one message load per baseline session.
	baseline, _ := s.db.GetSessionsTokenBaseline(ctx, db.GetSessionsTokenBaselineParams{
		ID:    sessionID,
		Limit: int64(s.cfg.MaxTokensBaseline),
	})

	reward := calculateReward(rewardInput{
		Messages:         msgInfos,
		Patterns:         s.patterns,
		Baseline:         baseline,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Weights:          s.cfg.ResolvedWeights(),
		Feedback:         s.loadFeedback(ctx, sessionID),
	})
	res.Reward = reward

	if rescoring {
		// Replace the score in place and apply just the reward delta to the
		// variants the session was served (times_used stays untouched).
		_, err = s.db.UpdateSessionScore(ctx, db.UpdateSessionScoreParams{
			Reward:           reward.Total,
			SuccessScore:     reward.SuccessScore,
			EfficiencyScore:  reward.EfficiencyScore,
			PromptTokens:     reward.PromptTokens,
			CompletionTokens: reward.CompletionTokens,
			MessageCount:     reward.MessageCount,
			UserCorrections:  int64(reward.UserCorrections),
			Components:       reward.Breakdown.JSON(),
			SessionID:        sessionID,
		})
		if err != nil {
			return res, fmt.Errorf("evaluator: update session score: %w", err)
		}
		s.applyRewardDelta(ctx, sessionID, reward.Total-existing.Reward)
		slog.Info("evaluator: session re-scored",
			"session_id", sessionID,
			"previous_reward", existing.Reward,
			"reward", reward.Total,
			"feedback", reward.Breakdown.Feedback,
		)
		return res, nil
	}

	// Persist session score
	scoreID := uuid.New().String()
	_, err = s.db.InsertSessionScore(ctx, db.InsertSessionScoreParams{
		ID:              scoreID,
		SessionID:       sessionID,
		Reward:          reward.Total,
		SuccessScore:    reward.SuccessScore,
		EfficiencyScore: reward.EfficiencyScore,
		// Judge output is written by maybeJudge once (and if) the judge ran.
		JudgeAnalysis:    sql.NullString{},
		JudgeModel:       sql.NullString{},
		PromptTokens:     reward.PromptTokens,
		CompletionTokens: reward.CompletionTokens,
		MessageCount:     reward.MessageCount,
		UserCorrections:  int64(reward.UserCorrections),
		Components:       reward.Breakdown.JSON(),
	})
	if err != nil {
		return res, fmt.Errorf("evaluator: insert session score: %w", err)
	}

	s.applySessionReward(ctx, sessionID, reward.Total)

	slog.Info("evaluator: session evaluated",
		"session_id", sessionID,
		"reward", reward.Total,
		"success_score", reward.SuccessScore,
		"efficiency_score", reward.EfficiencyScore,
		"corrections", reward.UserCorrections,
	)

	s.maybeJudge(ctx, res, msgs, reward, opts)

	return res, nil
}

// judgeSettings returns the judge gating config with zero values replaced by
// the defaults. The daily budgets stay as configured (0 = unlimited).
func (s *EvaluatorService) judgeSettings() config.JudgeConfig {
	j := s.cfg.Judge
	if j.HighReward == 0 {
		j.HighReward = 0.8
	}
	if j.LowReward == 0 {
		j.LowReward = 0.3
	}
	if j.MinTurns == 0 {
		j.MinTurns = 4
	}
	if j.MaxTranscriptTokens == 0 {
		j.MaxTranscriptTokens = 6000
	}
	return j
}

// shouldJudge reports whether a session is decisive enough for an LLM judge
// call: a very high or very low reward and enough user turns.
func shouldJudge(j config.JudgeConfig, total float64, userTurns int) bool {
	if userTurns < j.MinTurns {
		return false
	}
	return total >= j.HighReward || total <= j.LowReward
}

// maybeJudge runs the LLM judge for a freshly scored session when it is
// decisive and the daily budget allows, and persists the judge output.
func (s *EvaluatorService) maybeJudge(ctx context.Context, res *Result, msgs []message.Message, reward RewardResult, opts EvaluateOptions) {
	if s.judge == nil || opts.SkipJudge {
		return
	}
	js := s.judgeSettings()
	if !shouldJudge(js, reward.Total, countUserMessages(msgs)) {
		slog.Debug("evaluator: judge skipped, session not decisive",
			"session_id", res.SessionID, "reward", reward.Total, "user_turns", countUserMessages(msgs))
		return
	}

	s.judgeMu.Lock()
	defer s.judgeMu.Unlock()

	if s.judgeBudgetExhausted(ctx, js) {
		return
	}

	meta := JudgeMeta{
		TemplateName:    "default",
		TemplateVersion: 1,
		Corrections:     reward.UserCorrections,
		Tokens:          reward.PromptTokens + reward.CompletionTokens,
	}
	meta.Transcript = buildCappedTranscript(msgs, s.cfg.JudgePromptTemplate, meta, js.MaxTranscriptTokens)

	jr, err := s.judge.EvaluateWithUsage(ctx, meta, s.cfg.JudgePromptTemplate)
	if err != nil {
		slog.Warn("evaluator: judge call failed", "session_id", res.SessionID, "err", err)
		s.recordJudgeError(err)
		return
	}
	if jr == nil || jr.Output == nil {
		return
	}
	out := jr.Output
	slog.Debug("evaluator: judge output", "session_id", res.SessionID, "confidence", out.Confidence, "task_type", out.TaskType)
	res.Judged = true

	model := jr.Model
	if model == "" {
		model = string(s.cfg.Model)
	}
	if model == "" {
		model = "unknown"
	}
	analysis, _ := json.Marshal(out)
	if err := s.db.UpdateSessionScoreJudge(ctx, db.UpdateSessionScoreJudgeParams{
		JudgeAnalysis:         sql.NullString{String: string(analysis), Valid: true},
		JudgeModel:            sql.NullString{String: model, Valid: true},
		JudgePromptTokens:     jr.PromptTokens,
		JudgeCompletionTokens: jr.CompletionTokens,
		SessionID:             res.SessionID,
	}); err != nil {
		slog.Warn("evaluator: persist judge output failed", "session_id", res.SessionID, "err", err)
	}
	if err := s.saveSkillFromJudge(ctx, out, res.SessionID, model); err != nil {
		slog.Warn("evaluator: save skill failed", "session_id", res.SessionID, "err", err)
	}
}

// startOfDay returns the unix time of local midnight for t.
func startOfDay(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}

// judgeBudgetExhausted reports whether today's judge calls or tokens, counted
// from persisted scores (so it survives restarts), reached the daily budget.
// It logs a single warning per day when the budget is exhausted.
func (s *EvaluatorService) judgeBudgetExhausted(ctx context.Context, js config.JudgeConfig) bool {
	if js.DailyCalls <= 0 && js.DailyTokens <= 0 {
		return false
	}
	now := time.Now()
	usage, err := s.db.GetJudgeUsageSince(ctx, startOfDay(now))
	if err != nil {
		slog.Debug("evaluator: judge budget lookup failed, allowing call", "err", err)
		return false
	}
	callsOut := js.DailyCalls > 0 && usage.Calls >= int64(js.DailyCalls)
	tokensOut := js.DailyTokens > 0 && usage.Tokens >= js.DailyTokens
	if !callsOut && !tokensOut {
		return false
	}
	day := now.Format("2006-01-02")
	s.mu.Lock()
	warn := s.budgetWarnedDay != day
	s.budgetWarnedDay = day
	s.mu.Unlock()
	if warn {
		slog.Warn("evaluator: daily judge budget exhausted, skipping judge calls until tomorrow",
			"calls", usage.Calls, "daily_calls", js.DailyCalls, "tokens", usage.Tokens, "daily_tokens", js.DailyTokens)
	}
	return true
}

// TemplatesEnabled reports whether prompt variant selection is active: the
// evaluator is enabled and evaluator.templates.enabled is not switched off.
func (s *EvaluatorService) TemplatesEnabled() bool {
	return s != nil && s.cfg.Enabled && s.cfg.Templates.Enabled && s.db != nil
}

// SelectVariant returns the variant id to use for a section of a session.
// candidates[0] is the default variant (the embedded template); the rest are
// variant files. The first call for a (session, section) chooses and persists
// the variant in session_template_selections; later calls, also from another
// process after a restart, return the persisted choice so the prompt bytes and
// the reward attribution stay stable for the whole session.
func (s *EvaluatorService) SelectVariant(ctx context.Context, sessionID, section string, candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", nil
	}
	def := candidates[0]
	if !s.TemplatesEnabled() || sessionID == "" || len(candidates) < 2 {
		return def, nil
	}
	usable := func(id string) string {
		for _, c := range candidates {
			if c == id {
				return id
			}
		}
		return def // the frozen variant no longer exists on disk
	}

	if id, ok := s.cachedSelection(sessionID, section); ok {
		return usable(id), nil
	}

	id, err := s.db.GetSessionTemplateSelection(ctx, db.GetSessionTemplateSelectionParams{SessionID: sessionID, Section: section})
	if err == nil {
		s.cacheSelection(sessionID, section, id)
		return usable(id), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return def, fmt.Errorf("evaluator: read template selection: %w", err)
	}

	chosen, err := s.pickVariant(ctx, section, candidates)
	if err != nil {
		return def, err
	}
	if err := s.db.InsertSessionTemplateSelection(ctx, db.InsertSessionTemplateSelectionParams{
		SessionID: sessionID, Section: section, VariantID: chosen,
	}); err != nil {
		return def, fmt.Errorf("evaluator: persist template selection: %w", err)
	}
	// Read back: another process may have won the insert race.
	if winner, err := s.db.GetSessionTemplateSelection(ctx, db.GetSessionTemplateSelectionParams{SessionID: sessionID, Section: section}); err == nil {
		chosen = winner
	}
	s.cacheSelection(sessionID, section, chosen)
	slog.Debug("evaluator: prompt variant selected", "session_id", sessionID, "section", section, "variant", chosen)
	return usable(chosen), nil
}

func (s *EvaluatorService) cachedSelection(sessionID, section string) (string, bool) {
	s.selMu.Lock()
	defer s.selMu.Unlock()
	id, ok := s.selections[sessionID][section]
	return id, ok
}

func (s *EvaluatorService) cacheSelection(sessionID, section, id string) {
	s.selMu.Lock()
	defer s.selMu.Unlock()
	if s.selections[sessionID] == nil {
		s.selections[sessionID] = make(map[string]string)
	}
	s.selections[sessionID][section] = id
}

func (s *EvaluatorService) forgetSelections(sessionID string) {
	s.selMu.Lock()
	delete(s.selections, sessionID)
	s.selMu.Unlock()
}

// pickVariant chooses a variant among candidates for a new session. Until the
// section has minSessionsForUCB evaluated sessions it balances exploration by
// picking the least selected variant (ties in candidate order); after that it
// uses UCB1 over the persisted per-variant statistics.
func (s *EvaluatorService) pickVariant(ctx context.Context, section string, candidates []string) (string, error) {
	stats, err := s.db.ListVariantStatsBySection(ctx, section)
	if err != nil {
		return "", fmt.Errorf("evaluator: list variant stats: %w", err)
	}
	byID := make(map[string]db.PromptVariantStat, len(stats))
	total := 0
	for _, st := range stats {
		byID[st.VariantID] = st
	}
	for _, c := range candidates {
		total += int(byID[c].TimesUsed)
	}

	if total < s.cfg.MinSessionsForUCB {
		counts, err := s.db.ListVariantSelectionCounts(ctx, section)
		if err != nil {
			return "", fmt.Errorf("evaluator: count variant selections: %w", err)
		}
		selected := make(map[string]int64, len(counts))
		for _, c := range counts {
			selected[c.VariantID] = c.Selections
		}
		best := candidates[0]
		for _, c := range candidates[1:] {
			if selected[c] < selected[best] {
				best = c
			}
		}
		return best, nil
	}

	best := candidates[0]
	bestScore := -1.0
	for _, c := range candidates {
		st := byID[c]
		score := UCBScore(st.AvgReward, total, int(st.TimesUsed), s.cfg.ExplorationC)
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best, nil
}

// applySessionReward counts a freshly evaluated session once for every variant
// it was served (per section) and drops the in-memory selections of the session.
func (s *EvaluatorService) applySessionReward(ctx context.Context, sessionID string, reward float64) {
	defer s.forgetSelections(sessionID)
	defer s.forgetSkills(sessionID)
	if err := s.db.ApplySessionRewardToSkillStats(ctx, db.ApplySessionRewardToSkillStatsParams{
		Reward: reward, Reward2: reward, SessionID: sessionID,
	}); err != nil {
		slog.Warn("evaluator: update skill stats failed", "session_id", sessionID, "err", err)
	}
	if err := s.db.ApplySessionRewardToVariantStats(ctx, db.ApplySessionRewardToVariantStatsParams{
		TotalReward: reward, AvgReward: reward, SessionID: sessionID,
	}); err != nil {
		slog.Warn("evaluator: update variant stats failed", "session_id", sessionID, "err", err)
	}
}

// applyRewardDelta applies a re-score's reward change to the session's variants.
func (s *EvaluatorService) applyRewardDelta(ctx context.Context, sessionID string, delta float64) {
	if delta == 0 {
		return
	}
	if err := s.db.ApplyRewardDeltaToSkillStats(ctx, db.ApplyRewardDeltaToSkillStatsParams{
		Delta: delta, Delta2: delta, SessionID: sessionID,
	}); err != nil {
		slog.Warn("evaluator: update skill stats delta failed", "session_id", sessionID, "err", err)
	}
	if err := s.db.ApplyRewardDeltaToVariantStats(ctx, db.ApplyRewardDeltaToVariantStatsParams{
		Delta: delta, Delta2: delta, SessionID: sessionID,
	}); err != nil {
		slog.Warn("evaluator: update variant stats delta failed", "session_id", sessionID, "err", err)
	}
}

// GetActiveSkills returns the approved skills for a task type, best ranked
// first (success_rate, then usage). It has no side effects: injection
// accounting happens once per session in SessionSkills.
func (s *EvaluatorService) GetActiveSkills(ctx context.Context, taskType string) ([]Skill, error) {
	if s == nil || !s.cfg.Enabled || s.db == nil {
		return nil, nil
	}

	if taskType == "" {
		taskType = "general"
	}

	rows, err := s.db.ListActiveSkillsByType(ctx, db.ListActiveSkillsByTypeParams{
		TaskType: taskType,
		Limit:    maxInjectedSkills,
	})
	if err != nil {
		return nil, fmt.Errorf("evaluator: list active skills: %w", err)
	}
	slog.Debug("evaluator: loaded active skills", "task_type", taskType, "count", len(rows))

	skills := make([]Skill, 0, len(rows))
	for _, r := range rows {
		skills = append(skills, skillFromRow(r))
	}
	return skills, nil
}

// maxCachedSkillSets bounds the in-memory frozen skill sets.
const maxCachedSkillSets = 512

// SessionSkills returns the learned skills injected into a session's system
// prompt. The set is chosen once, on the session's first prompt build, and
// persisted in session_skill_injections: later turns and later processes get
// the same skills in the same order, even when a skill is approved
// mid-session (it is picked up by the next session). usage_count is incremented
// once per skill when the set is persisted. Without a session id the current
// approved skills are returned unfrozen and without accounting.
func (s *EvaluatorService) SessionSkills(ctx context.Context, sessionID, taskType string) ([]Skill, error) {
	if s == nil || !s.cfg.Enabled || s.db == nil {
		return nil, nil
	}
	if sessionID == "" {
		return s.GetActiveSkills(ctx, taskType)
	}

	s.skillSelMu.Lock()
	defer s.skillSelMu.Unlock()
	if sk, ok := s.skillSel[sessionID]; ok {
		return sk, nil
	}

	n, err := s.db.CountSessionSkillInjections(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("evaluator: read skill injections: %w", err)
	}
	if n == 0 {
		if err := s.SyncLearnedSkills(ctx); err != nil {
			slog.Debug("evaluator: skill sync failed", "err", err)
		}
		chosen, err := s.GetActiveSkills(ctx, taskType)
		if err != nil {
			return nil, err
		}
		if len(chosen) == 0 {
			// Empty-set marker: a skill approved later must not appear mid-session.
			if err := s.db.InsertSessionSkillInjection(ctx, db.InsertSessionSkillInjectionParams{SessionID: sessionID}); err != nil {
				return nil, fmt.Errorf("evaluator: persist skill injections: %w", err)
			}
		}
		for i, sk := range chosen {
			if err := s.db.InsertSessionSkillInjection(ctx, db.InsertSessionSkillInjectionParams{
				SessionID: sessionID, SkillID: sk.ID, Position: int64(i),
			}); err != nil {
				return nil, fmt.Errorf("evaluator: persist skill injections: %w", err)
			}
		}
		for _, sk := range chosen {
			if err := s.db.IncrementSkillUsage(ctx, sk.ID); err != nil {
				slog.Debug("evaluator: increment skill usage failed", "id", sk.ID, "err", err)
			}
		}
		slog.Debug("evaluator: skills frozen for session", "session_id", sessionID, "count", len(chosen))
	}

	rows, err := s.db.ListSessionInjectedSkills(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("evaluator: load injected skills: %w", err)
	}
	skills := make([]Skill, 0, len(rows))
	for _, r := range rows {
		skills = append(skills, skillFromRow(r))
	}
	if len(s.skillSel) >= maxCachedSkillSets {
		s.skillSel = make(map[string][]Skill)
	}
	s.skillSel[sessionID] = skills
	return skills, nil
}

func (s *EvaluatorService) forgetSkills(sessionID string) {
	s.skillSelMu.Lock()
	delete(s.skillSel, sessionID)
	s.skillSelMu.Unlock()
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

	ranking, _ := s.db.ListAllVariantStats(ctx)
	allSkills, _ := s.ListSkills(ctx, "", "")

	templateStats := VariantStatsFromRows(ranking, s.cfg.ExplorationC)

	topSkills := allSkills

	var lastEval time.Time
	if aggr.LastEvaluation.Valid {
		lastEval = time.Unix(aggr.LastEvaluation.Int64, 0)
	}

	st := &Stats{
		TotalEvaluations: int(aggr.TotalEvaluations),
		Templates:        templateStats,
		SkillCount:       int(aggr.ActiveSkills),
		TopSkills:        topSkills,
		AvgReward:        aggr.AvgReward,
		LastEvaluation:   lastEval,
		IsEnabled:        true,
	}
	if recent, err := RecentSessionDetails(ctx, s.db, 20); err == nil {
		st.RecentSessions = recent
	}
	if daily, err := DailyMetrics(ctx, s.db, 14, time.Now()); err == nil {
		st.Daily = daily
	}
	if rep, err := s.Diagnose(ctx); err == nil {
		st.Problem = rep.ProblemLine()
	}
	return st, nil
}

// minUserTurns is the minimum number of user messages a session needs to be scored.
const minUserTurns = 2

func countUserMessages(msgs []message.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == message.User {
			n++
		}
	}
	return n
}

// messagesToInfo converts message.Message slice to evaluator messageInfo slice.
// Tokens are left at 0 here; session-level token counts are fetched separately.
func messagesToInfo(msgs []message.Message) []messageInfo {
	infos := make([]messageInfo, 0, len(msgs))
	for _, m := range msgs {
		info := messageInfo{
			role:   m.Role,
			isUser: m.Role == message.User,
			text:   m.Content().Text,
			finish: m.FinishReason(),
		}
		for _, tc := range m.ToolCalls() {
			info.toolCalls = append(info.toolCalls, toolCallInfo{name: tc.Name, input: tc.Input})
		}
		for _, tr := range m.ToolResults() {
			if tr.IsError {
				info.toolErrors++
			}
		}
		infos = append(infos, info)
	}
	return infos
}

// transcriptChunks formats each message as a readable transcript chunk for the
// judge. Tool results and any single chunk are truncated to avoid huge payloads.
func transcriptChunks(msgs []message.Message, maxChunkChars int) []string {
	const maxToolLen = 500
	chunks := make([]string, 0, len(msgs))
	for _, m := range msgs {
		var sb strings.Builder
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
		c := sb.String()
		if c == "" {
			continue
		}
		if maxChunkChars > 0 && len(c) > maxChunkChars {
			c = c[:maxChunkChars] + "...[truncated]\n"
		}
		chunks = append(chunks, c)
	}
	return chunks
}

// buildCappedTranscript builds the judge transcript so that the whole rendered
// prompt stays within maxTokens: the head and tail of the conversation are kept
// and the middle is replaced by an elision marker. maxTokens <= 0 disables the cap.
func buildCappedTranscript(msgs []message.Message, promptTemplate string, meta JudgeMeta, maxTokens int) string {
	if maxTokens <= 0 {
		return strings.Join(transcriptChunks(msgs, 0), "")
	}
	// Budget for the transcript = cap minus the prompt boilerplate.
	overhead := 0
	meta.Transcript = ""
	if p, err := renderJudgePrompt(promptTemplate, meta); err == nil {
		overhead = skills.EstimateTokens(p)
	}
	budget := maxTokens - overhead - 40 // room for the elision marker
	if min := maxTokens / 4; budget < min {
		budget = min
	}
	// No single message may take more than a quarter of the budget.
	chunks := transcriptChunks(msgs, budget)
	if len(chunks) == 0 {
		return ""
	}
	costs := make([]int, len(chunks))
	total := 0
	for i, c := range chunks {
		costs[i] = skills.EstimateTokens(c)
		total += costs[i]
	}
	if total <= budget {
		return strings.Join(chunks, "")
	}
	headBudget, tailBudget := budget*2/5, budget*3/5
	head, used := 0, 0
	for head < len(chunks) && used+costs[head] <= headBudget {
		used += costs[head]
		head++
	}
	tail, used := len(chunks), 0
	for tail > head && used+costs[tail-1] <= tailBudget {
		used += costs[tail-1]
		tail--
	}
	var sb strings.Builder
	sb.WriteString(strings.Join(chunks[:head], ""))
	fmt.Fprintf(&sb, "[... %d messages elided ...]\n", tail-head)
	sb.WriteString(strings.Join(chunks[tail:], ""))
	return sb.String()
}

// NewContextTrimmer creates a ContextTrimmer backed by this service's judge infrastructure.
// Returns nil if the evaluator has no judge configured, if the evaluator itself is
// nil, or unless evaluator.contextTrimmer.enabled is set (opt-in, default off).
func (s *EvaluatorService) NewContextTrimmer() *ContextTrimmer {
	if s == nil || s.judge == nil || !s.cfg.ContextTrimmer.Enabled {
		return nil
	}
	return NewContextTrimmer(s.cfg, s.judge)
}

// saveSkillFromJudge turns a confident judge proposal into a pending skill
// file for human review. Nothing is injected until the skill is approved.
// Proposals similar to any existing file (pending, approved or rejected) are
// dropped, so a rejected rule is not proposed again.
func (s *EvaluatorService) saveSkillFromJudge(ctx context.Context, out *JudgeOutput, sessionID, model string) error {
	if out == nil || strings.TrimSpace(out.NewSkill) == "" || out.Confidence < skillProposalMinConfidence {
		return nil
	}
	wd := s.workDir()
	if wd == "" {
		return errors.New("no project directory for learned skills")
	}

	s.skillFileMu.Lock()
	s.pruneUnderperformingLocked(ctx, wd)
	s.skillFileMu.Unlock()

	added, err := s.proposeSkill(ctx, wd, out, sessionID, model)
	if err != nil {
		return fmt.Errorf("evaluator: propose skill: %w", err)
	}
	if !added {
		slog.Debug("evaluator: skill deduplicated (similar rule already exists)", "task_type", out.TaskType)
	}
	return nil
}

// isDuplicateSkillText reports whether newContent is substantially similar to any existing rule.
// It uses a word-overlap ratio: if more than 70% of the significant words in newContent
// already appear in an existing rule, it is considered a duplicate.
func isDuplicateSkillText(newContent string, existing []string) bool {
	newWords := tokenizeSkill(newContent)
	if len(newWords) == 0 {
		return false
	}
	for _, content := range existing {
		if skillWordOverlap(newWords, tokenizeSkill(content)) > 0.70 {
			return true
		}
	}
	return false
}

// tokenizeSkill lowercases s and extracts significant words (length ≥ 4),
// filtering out stop words to focus on meaningful content.
func tokenizeSkill(s string) []string {
	lower := strings.ToLower(s)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := words[:0]
	for _, w := range words {
		if len(w) >= 4 && !isStopWord(w) {
			out = append(out, w)
		}
	}
	return out
}

// stopWords is a small set of common English words to exclude from skill similarity checks.
var stopWords = map[string]bool{
	"this": true, "that": true, "with": true, "from": true, "have": true,
	"will": true, "when": true, "your": true, "what": true, "should": true,
	"always": true, "never": true, "make": true, "sure": true, "using": true,
	"used": true, "only": true, "also": true, "then": true, "than": true,
	"more": true, "less": true, "each": true, "such": true, "been": true,
	"they": true, "them": true, "their": true, "there": true, "these": true,
	"those": true, "must": true, "need": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}

// skillWordOverlap returns the fraction of words in `a` that are present in `b`.
// Returns 0 if either slice is empty.
func skillWordOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setB := make(map[string]bool, len(b))
	for _, w := range b {
		setB[w] = true
	}
	matches := 0
	for _, w := range a {
		if setB[w] {
			matches++
		}
	}
	return float64(matches) / float64(len(a))
}
