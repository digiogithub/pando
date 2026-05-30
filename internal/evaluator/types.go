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

// ContextProfileKey is used to store a ContextProfile in the request context.
const ContextProfileKey contextKey = "context_profile"

// ContextProfile is produced by the ContextTrimmer at session start.
// It guides the PromptBuilder and agent tool assembly to include only
// what is relevant for the current task.
type ContextProfile struct {
	// TaskType is the classified task type (e.g. "code", "debug", "refactor").
	TaskType string
	// RelevantToolNames lists the tool names to keep for this task.
	// An empty slice means keep all tools (no filtering).
	RelevantToolNames []string
	// SkipSections lists prompt section names that are not needed for this task.
	SkipSections []string
	// Confidence is a 0.0–1.0 measure of the trimmer's certainty.
	// Profiles with Confidence < 0.5 should be ignored and defaults used.
	Confidence float64
}
