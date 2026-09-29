package db

import (
	"context"
	"database/sql"
)

// Hand-written queries for reviewable learned skills (see
// sql/self_improvement.sql, "Reviewable learned skills").

const skillColumns = `id, title, content, source_session_id, source_template_id, task_type, usage_count, success_rate, is_active, created_at, updated_at, status, confidence, judge_model, eval_count, reward_total`

func scanSkill(row interface{ Scan(...interface{}) error }) (SkillLibrary, error) {
	var i SkillLibrary
	err := row.Scan(
		&i.ID, &i.Title, &i.Content, &i.SourceSessionID, &i.SourceTemplateID,
		&i.TaskType, &i.UsageCount, &i.SuccessRate, &i.IsActive,
		&i.CreatedAt, &i.UpdatedAt, &i.Status, &i.Confidence, &i.JudgeModel,
		&i.EvalCount, &i.RewardTotal,
	)
	return i, err
}

func (q *Queries) querySkills(ctx context.Context, query string, args ...interface{}) ([]SkillLibrary, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SkillLibrary{}
	for rows.Next() {
		i, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return items, rows.Err()
}

const upsertSkillMirror = `INSERT INTO skill_library (
    id, title, content, source_session_id, task_type, is_active,
    status, confidence, judge_model, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'))
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    content = excluded.content,
    source_session_id = excluded.source_session_id,
    task_type = excluded.task_type,
    is_active = excluded.is_active,
    status = excluded.status,
    confidence = excluded.confidence,
    judge_model = excluded.judge_model,
    updated_at = strftime('%s', 'now')`

type UpsertSkillMirrorParams struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	Content         string         `json:"content"`
	SourceSessionID sql.NullString `json:"source_session_id"`
	TaskType        string         `json:"task_type"`
	IsActive        int64          `json:"is_active"`
	Status          string         `json:"status"`
	Confidence      float64        `json:"confidence"`
	JudgeModel      string         `json:"judge_model"`
	CreatedAt       int64          `json:"created_at"`
}

// UpsertSkillMirror mirrors a skill file into skill_library without touching
// its statistics.
func (q *Queries) UpsertSkillMirror(ctx context.Context, arg UpsertSkillMirrorParams) error {
	_, err := q.db.ExecContext(ctx, upsertSkillMirror,
		arg.ID, arg.Title, arg.Content, arg.SourceSessionID, arg.TaskType, arg.IsActive,
		arg.Status, arg.Confidence, arg.JudgeModel, arg.CreatedAt)
	return err
}

type SetSkillMirrorStateParams struct {
	Status   string `json:"status"`
	IsActive int64  `json:"is_active"`
	ID       string `json:"id"`
}

func (q *Queries) SetSkillMirrorState(ctx context.Context, arg SetSkillMirrorStateParams) error {
	_, err := q.db.ExecContext(ctx,
		`UPDATE skill_library SET status = ?, is_active = ?, updated_at = strftime('%s', 'now') WHERE id = ?`,
		arg.Status, arg.IsActive, arg.ID)
	return err
}

func (q *Queries) ListAllSkills(ctx context.Context) ([]SkillLibrary, error) {
	return q.querySkills(ctx, `SELECT `+skillColumns+` FROM skill_library ORDER BY created_at DESC, id`)
}

func (q *Queries) GetSkill(ctx context.Context, id string) (SkillLibrary, error) {
	return scanSkill(q.db.QueryRowContext(ctx, `SELECT `+skillColumns+` FROM skill_library WHERE id = ?`, id))
}

type InsertSessionSkillInjectionParams struct {
	SessionID string `json:"session_id"`
	SkillID   string `json:"skill_id"`
	Position  int64  `json:"position"`
}

// InsertSessionSkillInjection records a skill of the session's frozen set.
// First writer wins.
func (q *Queries) InsertSessionSkillInjection(ctx context.Context, arg InsertSessionSkillInjectionParams) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO session_skill_injections (session_id, skill_id, position, injected_at)
VALUES (?, ?, ?, strftime('%s', 'now'))`, arg.SessionID, arg.SkillID, arg.Position)
	return err
}

// CountSessionSkillInjections counts the rows of the session's frozen set,
// including the empty-set marker.
func (q *Queries) CountSessionSkillInjections(ctx context.Context, sessionID string) (int64, error) {
	var n int64
	err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_skill_injections WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}

// ListSessionInjectedSkills returns the skills frozen into the session's
// prompt, in injection order.
func (q *Queries) ListSessionInjectedSkills(ctx context.Context, sessionID string) ([]SkillLibrary, error) {
	return q.querySkills(ctx, `SELECT s.id, s.title, s.content, s.source_session_id, s.source_template_id, s.task_type, s.usage_count, s.success_rate, s.is_active, s.created_at, s.updated_at, s.status, s.confidence, s.judge_model, s.eval_count, s.reward_total
FROM session_skill_injections i
JOIN skill_library s ON s.id = i.skill_id
WHERE i.session_id = ?
ORDER BY i.position, s.id`, sessionID)
}

type ApplySessionRewardToSkillStatsParams struct {
	Reward    float64 `json:"reward"`
	Reward2   float64 `json:"reward2"`
	SessionID string  `json:"session_id"`
}

// ApplySessionRewardToSkillStats adds an evaluated session's reward to every
// skill injected into it; success_rate becomes the mean reward.
func (q *Queries) ApplySessionRewardToSkillStats(ctx context.Context, arg ApplySessionRewardToSkillStatsParams) error {
	_, err := q.db.ExecContext(ctx,
		`UPDATE skill_library
SET eval_count = eval_count + 1,
    reward_total = reward_total + ?,
    success_rate = (reward_total + ?) / (eval_count + 1),
    updated_at = strftime('%s', 'now')
WHERE id IN (SELECT skill_id FROM session_skill_injections WHERE session_id = ?)`,
		arg.Reward, arg.Reward2, arg.SessionID)
	return err
}

type ApplyRewardDeltaToSkillStatsParams struct {
	Delta     float64 `json:"delta"`
	Delta2    float64 `json:"delta2"`
	SessionID string  `json:"session_id"`
}

// ApplyRewardDeltaToSkillStats applies a re-score's reward change without
// touching eval_count.
func (q *Queries) ApplyRewardDeltaToSkillStats(ctx context.Context, arg ApplyRewardDeltaToSkillStatsParams) error {
	_, err := q.db.ExecContext(ctx,
		`UPDATE skill_library
SET reward_total = reward_total + ?,
    success_rate = (reward_total + ?) / MAX(eval_count, 1),
    updated_at = strftime('%s', 'now')
WHERE id IN (SELECT skill_id FROM session_skill_injections WHERE session_id = ?)`,
		arg.Delta, arg.Delta2, arg.SessionID)
	return err
}
