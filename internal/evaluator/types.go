package evaluator

import "time"

// PromptTemplate represents a versioned prompt template variant.
type PromptTemplate struct {
	ID        string
	Name      string
	Section   string
	Content   string
	Version   int
	IsDefault bool
}

// Skill represents a learned optimization rule from the Skill Library.
type Skill struct {
	ID          string
	Title       string
	Content     string
	TaskType    string
	SuccessRate float64
	UsageCount  int
}

// TemplateStats holds UCB statistics for a template (for TUI display).
type TemplateStats struct {
	Template  PromptTemplate
	TimesUsed int
	AvgReward float64
	UCBScore  float64
	Rank      int
}

// Stats is the overall self-improvement system statistics.
type Stats struct {
	TotalEvaluations int
	Templates        []TemplateStats
	SkillCount       int
	TopSkills        []Skill
	AvgReward        float64
	LastEvaluation   time.Time
	IsEnabled        bool
}

// RewardResult holds the decomposed reward calculation for a session.
type RewardResult struct {
	Total            float64
	SuccessScore     float64
	EfficiencyScore  float64
	PromptTokens     int64
	CompletionTokens int64
	MessageCount     int64
	UserCorrections  int
}

// JudgeOutput is the structured response from the LLM judge model.
type JudgeOutput struct {
	Reasoning  string   `json:"reasoning"`
	KeyPoints  []string `json:"key_points"`
	NewSkill   string   `json:"new_skill"`
	TaskType   string   `json:"task_type"`
	Confidence float64  `json:"confidence"`
}

// contextKey is used for context values.
type contextKey string

// SelectedTemplateKey is used to store the selected template ID in context.
const SelectedTemplateKey contextKey = "selected_template_id"
