package client

import (
	"context"
	"net/url"
)

// Evaluator (self-improvement) DTOs, mirroring
// web-ui/packages/pando-client/src/types (Evaluator*, Template*, Skill).

// EvaluatorDaily is one day of GET /evaluator/metrics.
type EvaluatorDaily struct {
	Day                  string  `json:"day"`
	Evaluations          int     `json:"evaluations"`
	AvgReward            float64 `json:"avg_reward"`
	JudgeCalls           int     `json:"judge_calls"`
	JudgePromptTokens    int     `json:"judge_prompt_tokens"`
	JudgeCompletionToken int     `json:"judge_completion_tokens"`
}

// EvaluatorMetrics is GET /api/v1/evaluator/metrics.
type EvaluatorMetrics struct {
	TotalSessions         int              `json:"total_sessions"`
	TotalTemplates        int              `json:"total_templates"`
	AvgReward             float64          `json:"avg_reward"`
	ActiveSkills          int              `json:"active_skills"`
	IsEnabled             bool             `json:"is_enabled"`
	Daily                 []EvaluatorDaily `json:"daily"`
	JudgeCalls            int              `json:"judge_calls"`
	JudgePromptTokens     int              `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int              `json:"judge_completion_tokens"`
	Problem               string           `json:"problem"`
}

// TemplateVariant is a prompt variant with its bandit statistics.
type TemplateVariant struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	IsDefault bool    `json:"is_default"`
	Missing   bool    `json:"missing"`
	TimesUsed int     `json:"times_used"`
	AvgReward float64 `json:"avg_reward"`
	UCBScore  float64 `json:"ucb_score"`
}

// TemplateSection groups the variants of a prompt section.
type TemplateSection struct {
	Section  string            `json:"section"`
	Variants []TemplateVariant `json:"variants"`
}

// Skill is a learned skill.
type Skill struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	TaskType    string  `json:"task_type"`
	Confidence  float64 `json:"confidence"`
	Uses        int     `json:"uses"`
	// Status is pending, approved or rejected.
	Status      string  `json:"status"`
	SuccessRate float64 `json:"success_rate"`
	EvalCount   int     `json:"eval_count"`
}

// EvaluatorPatternHit is a correction pattern that matched.
type EvaluatorPatternHit struct {
	Index   int     `json:"index"`
	Pattern string  `json:"pattern"`
	Weight  float64 `json:"weight"`
	Snippet string  `json:"snippet"`
}

// EvaluatorComponents is the reward decomposition of a session.
type EvaluatorComponents struct {
	Components map[string]float64 `json:"components"`
	Weights    map[string]float64 `json:"weights"`
}

// EvaluatorJudge is the LLM judge output.
type EvaluatorJudge struct {
	Reasoning  string   `json:"reasoning"`
	KeyPoints  []string `json:"key_points"`
	NewSkill   string   `json:"new_skill"`
	TaskType   string   `json:"task_type"`
	Confidence float64  `json:"confidence"`
}

// EvaluatorSessionScore is one row of GET /evaluator/sessions.
type EvaluatorSessionScore struct {
	ID           string                `json:"id"`
	SessionID    string                `json:"session_id"`
	Title        string                `json:"title"`
	Corrections  int                   `json:"corrections"`
	PatternHits  []EvaluatorPatternHit `json:"pattern_hits"`
	Feedback     string                `json:"feedback"`
	FeedbackNote string                `json:"feedback_note"`
	Variants     []struct {
		Section string `json:"section"`
		Variant string `json:"variant"`
	} `json:"variants"`
	Skills []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"skills"`
	Reward                float64             `json:"reward"`
	MessageCount          int                 `json:"message_count"`
	EvaluatedAt           int64               `json:"evaluated_at"`
	Components            EvaluatorComponents `json:"components"`
	JudgeAnalysis         *EvaluatorJudge     `json:"judge_analysis"`
	JudgeModel            string              `json:"judge_model"`
	JudgePromptTokens     int                 `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int                 `json:"judge_completion_tokens"`
}

// EvaluatorDoctor is GET /evaluator/doctor.
type EvaluatorDoctor struct {
	Problem string `json:"problem"`
	Text    string `json:"text"`
	Report  struct {
		Enabled         bool     `json:"enabled"`
		DisabledReasons []string `json:"disabled_reasons"`
		Warnings        []string `json:"warnings"`
	} `json:"report"`
}

// EvaluatorMetrics fetches the headline metrics and the daily series.
func (c *Client) EvaluatorMetrics(ctx context.Context) (EvaluatorMetrics, error) {
	var out EvaluatorMetrics
	err := c.Get(ctx, "/api/v1/evaluator/metrics", &out)
	return out, err
}

// EvaluatorTemplates fetches the prompt sections with UCB statistics.
func (c *Client) EvaluatorTemplates(ctx context.Context) ([]TemplateSection, error) {
	var out struct {
		Sections []TemplateSection `json:"sections"`
	}
	err := c.Get(ctx, "/api/v1/evaluator/templates", &out)
	return out.Sections, err
}

// EvaluatorSkills fetches the learned skills.
func (c *Client) EvaluatorSkills(ctx context.Context) ([]Skill, error) {
	var out struct {
		Skills []Skill `json:"skills"`
	}
	err := c.Get(ctx, "/api/v1/evaluator/skills", &out)
	return out.Skills, err
}

// EvaluatorSessions fetches the recent evaluated sessions.
func (c *Client) EvaluatorSessions(ctx context.Context) ([]EvaluatorSessionScore, error) {
	var out struct {
		Sessions []EvaluatorSessionScore `json:"sessions"`
	}
	err := c.Get(ctx, "/api/v1/evaluator/sessions", &out)
	return out.Sessions, err
}

// EvaluatorDoctor fetches the diagnosis report.
func (c *Client) EvaluatorDoctor(ctx context.Context) (EvaluatorDoctor, error) {
	var out EvaluatorDoctor
	err := c.Get(ctx, "/api/v1/evaluator/doctor", &out)
	return out, err
}

// ReviewSkill approves or rejects a learned skill (decision "approve" or "reject").
func (c *Client) ReviewSkill(ctx context.Context, id, decision string) error {
	return c.Post(ctx, "/api/v1/evaluator/skills/"+url.PathEscape(id)+"/"+decision, map[string]any{}, nil)
}
