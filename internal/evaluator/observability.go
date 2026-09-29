package evaluator

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/db"
)

// SessionVariant is a prompt variant served to an evaluated session.
type SessionVariant struct {
	Section string `json:"section"`
	Variant string `json:"variant"`
}

// SessionSkillRef is a learned skill injected into an evaluated session.
type SessionSkillRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// SessionDetail is one evaluated session with everything the observability
// surfaces show: the reward decomposition, the corrections it counted, the
// variants and skills it ran with, and the judge output.
type SessionDetail struct {
	SessionID       string  `json:"session_id"`
	Title           string  `json:"title"`
	Reward          float64 `json:"reward"`
	SuccessScore    float64 `json:"success_score"`
	EfficiencyScore float64 `json:"efficiency_score"`
	MessageCount    int64   `json:"message_count"`
	UserCorrections int64   `json:"user_corrections"`
	EvaluatedAt     int64   `json:"evaluated_at"`
	// Breakdown is the parsed components JSON (zero for legacy rows).
	Breakdown Breakdown `json:"breakdown"`
	// Components is the persisted components JSON as stored.
	Components json.RawMessage `json:"components"`
	// JudgeAnalysis is the stored judge output, JSON null when the judge did not run.
	JudgeAnalysis         json.RawMessage   `json:"judge_analysis"`
	JudgeModel            string            `json:"judge_model,omitempty"`
	JudgePromptTokens     int64             `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int64             `json:"judge_completion_tokens"`
	Variants              []SessionVariant  `json:"variants"`
	Skills                []SessionSkillRef `json:"skills"`
}

// TopComponents returns up to n "name value" pairs of the session's reward
// components, highest weight first (ties by name).
func (d SessionDetail) TopComponents(n int) []ComponentScore {
	out := make([]ComponentScore, 0, len(d.Breakdown.Components))
	for name, v := range d.Breakdown.Components {
		out = append(out, ComponentScore{Name: name, Score: v, Weight: d.Breakdown.Weights[name]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].Name < out[j].Name
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// ComponentScore is one reward component of a session.
type ComponentScore struct {
	Name   string
	Score  float64
	Weight float64
}

// RecentSessionDetails lists the most recently evaluated sessions with their
// components, variants and skills.
func RecentSessionDetails(ctx context.Context, q db.Querier, limit int) ([]SessionDetail, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := q.ListSessionScoresWithTitle(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	variants := map[string][]SessionVariant{}
	if vr, err := q.ListRecentSessionVariants(ctx, int64(limit)); err == nil {
		for _, v := range vr {
			name := v.VariantID
			if i := strings.LastIndex(name, "#"); i >= 0 {
				name = name[i+1:]
			}
			variants[v.SessionID] = append(variants[v.SessionID], SessionVariant{Section: v.Section, Variant: name})
		}
	}
	skills := map[string][]SessionSkillRef{}
	if sr, err := q.ListRecentSessionSkills(ctx, int64(limit)); err == nil {
		for _, s := range sr {
			skills[s.SessionID] = append(skills[s.SessionID], SessionSkillRef{ID: s.SkillID, Title: s.Title})
		}
	}

	out := make([]SessionDetail, 0, len(rows))
	for _, r := range rows {
		d := SessionDetail{
			SessionID: r.SessionID, Title: r.Title, Reward: r.Reward,
			SuccessScore: r.SuccessScore, EfficiencyScore: r.EfficiencyScore,
			MessageCount: r.MessageCount, UserCorrections: r.UserCorrections,
			EvaluatedAt: r.EvaluatedAt, Components: componentsRaw(r.Components),
			JudgeAnalysis: judgeRaw(r.JudgeAnalysis), JudgeModel: r.JudgeModel.String,
			JudgePromptTokens: r.JudgePromptTokens, JudgeCompletionTokens: r.JudgeCompletionTokens,
			Variants: variants[r.SessionID], Skills: skills[r.SessionID],
		}
		if d.Variants == nil {
			d.Variants = []SessionVariant{}
		}
		if d.Skills == nil {
			d.Skills = []SessionSkillRef{}
		}
		_ = json.Unmarshal(d.Components, &d.Breakdown)
		out = append(out, d)
	}
	return out, nil
}

func componentsRaw(s string) json.RawMessage {
	if s == "" || !json.Valid([]byte(s)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}

func judgeRaw(s sql.NullString) json.RawMessage {
	if !s.Valid || s.String == "" || !json.Valid([]byte(s.String)) {
		return json.RawMessage("null")
	}
	return json.RawMessage(s.String)
}

// DailyMetric is one local day of evaluator activity.
type DailyMetric struct {
	Day                   string  `json:"day"`
	Evaluations           int64   `json:"evaluations"`
	AvgReward             float64 `json:"avg_reward"`
	JudgeCalls            int64   `json:"judge_calls"`
	JudgePromptTokens     int64   `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int64   `json:"judge_completion_tokens"`
}

// DailyMetrics returns one entry per local day for the last days days (today
// included), zero-filled so a chart has a continuous axis. Task type is not
// stored with a score (only the judge output carries one), so the mean reward
// is per day, not per task type.
func DailyMetrics(ctx context.Context, q db.Querier, days int, now time.Time) ([]DailyMetric, error) {
	if days <= 0 {
		days = 14
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	rows, err := q.ListDailyEvaluationMetrics(ctx, start.Unix())
	if err != nil {
		return nil, err
	}
	byDay := make(map[string]db.ListDailyEvaluationMetricsRow, len(rows))
	for _, r := range rows {
		byDay[r.Day] = r
	}
	out := make([]DailyMetric, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		m := DailyMetric{Day: day}
		if r, ok := byDay[day]; ok {
			m = DailyMetric{Day: day, Evaluations: r.Evaluations, AvgReward: r.AvgReward,
				JudgeCalls: r.JudgeCalls, JudgePromptTokens: r.JudgePromptTokens, JudgeCompletionTokens: r.JudgeCompletionTokens}
		}
		out = append(out, m)
	}
	return out, nil
}

// Diagnose runs the doctor for this service (its config, DB and background state).
func (s *EvaluatorService) Diagnose(ctx context.Context) (*Report, error) {
	return Diagnose(ctx, DiagnoseOptions{Config: s.cfg, DB: s.db, Service: s, WorkDir: s.workDir()})
}
