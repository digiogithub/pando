package evaluator

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/db"
)

// Skill represents a learned optimization rule from the Skill Library.
type Skill struct {
	ID          string
	Title       string
	Content     string
	TaskType    string
	SuccessRate float64
	UsageCount  int
}

// TemplateStats holds the UCB statistics of one prompt variant (for UI display).
type TemplateStats struct {
	// VariantID is "<section>#<variant>"; "<section>#default" is the embedded template.
	VariantID string
	Section   string
	Variant   string
	TimesUsed int
	AvgReward float64
	UCBScore  float64
	Rank      int
}

// VariantStatsFromRows converts persisted per-variant stats into ranked
// TemplateStats: UCB1 within each section, sections in alphabetical order.
func VariantStatsFromRows(rows []db.PromptVariantStat, explorationC float64) []TemplateStats {
	totals := make(map[string]int)
	for _, r := range rows {
		totals[r.Section] += int(r.TimesUsed)
	}
	out := make([]TemplateStats, 0, len(rows))
	for _, r := range rows {
		variant := r.VariantID
		if i := strings.LastIndex(variant, "#"); i >= 0 {
			variant = variant[i+1:]
		}
		out = append(out, TemplateStats{
			VariantID: r.VariantID,
			Section:   r.Section,
			Variant:   variant,
			TimesUsed: int(r.TimesUsed),
			AvgReward: r.AvgReward,
			UCBScore:  UCBScore(r.AvgReward, totals[r.Section], int(r.TimesUsed), explorationC),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		return out[i].UCBScore > out[j].UCBScore
	})
	rank := 0
	prev := ""
	for i := range out {
		if out[i].Section != prev {
			rank, prev = 0, out[i].Section
		}
		rank++
		out[i].Rank = rank
	}
	return out
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
	// Breakdown is the explainable decomposition persisted with the score.
	Breakdown Breakdown
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

// EvaluateOptions tunes a single evaluation.
type EvaluateOptions struct {
	// Force bypasses the completion guards (minimum user turns, subagent
	// sessions). Idempotency is never bypassed.
	Force bool
	// SkipJudge disables the LLM judge for this evaluation.
	SkipJudge bool
	// Rescore replaces an existing session score in place instead of skipping
	// the session as already evaluated (used after explicit feedback). The
	// judge never runs on a re-score.
	Rescore bool
}

// Result is the outcome of one evaluation.
type Result struct {
	SessionID string
	// Skipped is non-empty when no score was written; it holds the reason.
	Skipped string
	// Reward is the decomposition of the persisted score (zero when Skipped).
	Reward RewardResult
	// Judged reports whether the LLM judge ran.
	Judged bool
}

// Summary renders the result as human-readable text (reward decomposition).
func (r *Result) Summary() string {
	if r == nil {
		return "no result"
	}
	if r.Skipped != "" {
		return fmt.Sprintf("session %s not evaluated: %s", r.SessionID, r.Skipped)
	}
	judge := "off"
	if r.Judged {
		judge = "on"
	}
	rw := r.Reward
	return fmt.Sprintf(
		"session %s evaluated: reward %.3f (success %.2f, efficiency %.2f), corrections %d, messages %d, tokens %d prompt / %d completion, judge %s%s",
		r.SessionID, rw.Total, rw.SuccessScore, rw.EfficiencyScore, rw.UserCorrections, rw.MessageCount, rw.PromptTokens, rw.CompletionTokens, judge, rw.Breakdown.componentSummary(),
	)
}
