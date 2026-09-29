package evaluator

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/message"
)

// Reward components. Every component is a score in [0,1] where 1 is best.
const (
	ComponentSuccess    = "success"
	ComponentTokens     = "tokens"
	ComponentToolErrors = "toolErrors"
	ComponentCancels    = "cancels"
	ComponentRepetition = "repetition"
	ComponentTurns      = "turns"
	ComponentEndState   = "endState"
	// ComponentFeedback is informational: explicit feedback overrides the total.
	ComponentFeedback = "feedback"
)

// Correction penalties. The first user turn is never a correction; a correction
// that directly follows an assistant turn is a stronger signal than one typed
// while the user was still queueing messages.
const (
	correctionPenaltyAfterAssistant = 0.35
	correctionPenaltyOther          = 0.2
	// maxCorrectionPenalty caps the total success penalty so a long session
	// with many pushbacks still keeps a floor of 1-maxCorrectionPenalty.
	maxCorrectionPenalty = 0.8
	maxSnippetLen        = 80
)

// Explicit feedback bounds: bad feedback forces total < 0.3, good > 0.8.
const (
	FeedbackGood = "good"
	FeedbackBad  = "bad"

	feedbackBadCeiling = 0.25
	feedbackGoodFloor  = 0.85
)

// Turns component: sessions finished in up to turnsFree user turns are not
// penalised; the score then decays linearly to 0 over turnsDecay more turns.
const (
	turnsFree  = 4
	turnsDecay = 20
)

// compilePatterns pre-compiles correction detection patterns. Legacy bare
// negation patterns from older configs are dropped (see isLegacyBareNegation).
func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if isLegacyBareNegation(p) {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		result = append(result, re)
	}
	return result, nil
}

// isLegacyBareNegation reports whether p is the old default that flagged every
// "no" as a correction. Existing configs still carry it, and it makes
// "no problem" / "no, that's fine" count against the assistant.
func isLegacyBareNegation(p string) bool {
	switch strings.TrimSpace(p) {
	case `(?i)\bno[,.]?\b`, `(?i)\bno\b`:
		return true
	}
	return false
}

// PatternHit records one user turn flagged as a correction.
type PatternHit struct {
	// Index is the position of the message in the session (0-based).
	Index   int     `json:"index"`
	Pattern string  `json:"pattern"`
	Weight  float64 `json:"weight"`
	// AfterAssistant is true when the turn directly follows an assistant turn.
	AfterAssistant bool   `json:"afterAssistant"`
	Snippet        string `json:"snippet"`
}

// Breakdown is the persisted, explainable decomposition of a reward. It is
// stored as JSON in session_scores.components.
type Breakdown struct {
	// Components holds the score of every component that was available.
	Components map[string]float64 `json:"components"`
	// Weights holds the weight applied to each available component.
	Weights map[string]float64 `json:"weights"`
	// PatternHits are the user turns flagged as corrections.
	PatternHits []PatternHit `json:"patternHits,omitempty"`
	// Feedback is the explicit user rating ("good"/"bad") when present.
	Feedback     string `json:"feedback,omitempty"`
	FeedbackNote string `json:"feedbackNote,omitempty"`
	// Raw counters behind the components.
	UserTurns  int `json:"userTurns"`
	ToolCalls  int `json:"toolCalls"`
	ToolErrors int `json:"toolErrors"`
	Cancels    int `json:"cancels"`
	Repeats    int `json:"repeats"`
	// Baseline is the mean token count of recent sessions (0 when unknown).
	Baseline float64 `json:"baseline,omitempty"`
	// WeightedTotal is the total before any explicit-feedback override.
	WeightedTotal float64 `json:"weightedTotal"`
}

// JSON renders the breakdown for persistence.
func (b Breakdown) JSON() string {
	out, err := json.Marshal(b)
	if err != nil {
		return "{}"
	}
	return string(out)
}

// Feedback is an explicit user rating of a session.
type Feedback struct {
	Rating string
	Note   string
}

// rewardInput bundles everything calculateReward needs.
type rewardInput struct {
	Messages         []messageInfo
	Patterns         []*regexp.Regexp
	Baseline         float64
	PromptTokens     int64
	CompletionTokens int64
	Weights          config.EvaluatorWeights
	Feedback         *Feedback
}

// calculateReward computes the reward of a session as the weighted mean of the
// components available for it, then applies explicit feedback when present.
// It never calls an LLM and only uses data persisted with the session.
func calculateReward(in rewardInput) RewardResult {
	bd := Breakdown{
		Components: map[string]float64{},
		Weights:    map[string]float64{},
		Baseline:   in.Baseline,
	}

	// --- success: corrections in user turns after the first ---
	seenFirstUser := false
	penalty := 0.0
	for i, msg := range in.Messages {
		if !msg.isUser {
			continue
		}
		bd.UserTurns++
		if !seenFirstUser {
			seenFirstUser = true
			continue
		}
		re := firstMatch(in.Patterns, msg.text)
		if re == nil {
			continue
		}
		after := i > 0 && in.Messages[i-1].role == message.Assistant
		w := correctionPenaltyOther
		if after {
			w = correctionPenaltyAfterAssistant
		}
		penalty += w
		bd.PatternHits = append(bd.PatternHits, PatternHit{
			Index: i, Pattern: re.String(), Weight: w, AfterAssistant: after, Snippet: snippet(msg.text),
		})
	}
	success := 1.0 - math.Min(penalty, maxCorrectionPenalty)
	bd.Components[ComponentSuccess] = success

	// --- tokens: efficiency vs the recent-sessions baseline ---
	totalTokens := float64(in.PromptTokens + in.CompletionTokens)
	tokensAvailable := in.Baseline > 0 && totalTokens > 0
	tokens := 0.5 // neutral without a baseline; kept visible but not weighted
	if tokensAvailable {
		tokens = clamp01(1.0 - (totalTokens-in.Baseline)/in.Baseline)
	}
	bd.Components[ComponentTokens] = tokens

	// --- persisted-signal components ---
	toolCalls := map[string]int{}
	assistantTurns := 0
	for _, msg := range in.Messages {
		for _, tc := range msg.toolCalls {
			bd.ToolCalls++
			toolCalls[tc.name+"\x00"+tc.input]++
		}
		bd.ToolErrors += msg.toolErrors
		if msg.role == message.Assistant {
			assistantTurns++
			if msg.finish == message.FinishReasonCanceled {
				bd.Cancels++
			}
		}
	}
	for _, n := range toolCalls {
		if n > 1 {
			bd.Repeats += n - 1
		}
	}
	avail := map[string]bool{ComponentSuccess: true, ComponentTokens: tokensAvailable}
	if bd.ToolCalls > 0 {
		bd.Components[ComponentToolErrors] = clamp01(1.0 - float64(bd.ToolErrors)/float64(bd.ToolCalls))
		avail[ComponentToolErrors] = true
	}
	if assistantTurns > 0 {
		// Two or more cancelled runs give the worst score.
		bd.Components[ComponentCancels] = clamp01(1.0 - float64(bd.Cancels)/2.0)
		avail[ComponentCancels] = true
		bd.Components[ComponentEndState] = 1.0
		if endedOnError(in.Messages) {
			bd.Components[ComponentEndState] = 0
		}
		avail[ComponentEndState] = true
	}
	if bd.ToolCalls >= 2 {
		// A quarter of the calls being exact repeats already gives the worst score.
		bd.Components[ComponentRepetition] = clamp01(1.0 - 4.0*float64(bd.Repeats)/float64(bd.ToolCalls))
		avail[ComponentRepetition] = true
	}
	if bd.UserTurns > 0 {
		over := float64(bd.UserTurns - turnsFree)
		bd.Components[ComponentTurns] = clamp01(1.0 - math.Max(0, over)/turnsDecay)
		avail[ComponentTurns] = true
	}

	weights := map[string]float64{
		ComponentSuccess:    in.Weights.Success,
		ComponentTokens:     in.Weights.Tokens,
		ComponentToolErrors: in.Weights.ToolErrors,
		ComponentCancels:    in.Weights.Cancels,
		ComponentRepetition: in.Weights.Repetition,
		ComponentTurns:      in.Weights.Turns,
		ComponentEndState:   in.Weights.EndState,
	}
	var sum, wsum float64
	for name, w := range weights {
		if w <= 0 || !avail[name] {
			continue
		}
		bd.Weights[name] = w
		sum += w * bd.Components[name]
		wsum += w
	}
	// Drop unavailable components from the persisted view (except the neutral
	// tokens value, kept so the UI can show it was not measured).
	for name := range bd.Components {
		if !avail[name] && name != ComponentTokens {
			delete(bd.Components, name)
		}
	}
	total := success
	if wsum > 0 {
		total = sum / wsum
	}
	bd.WeightedTotal = total

	// --- explicit feedback dominates ---
	if in.Feedback != nil {
		switch in.Feedback.Rating {
		case FeedbackBad:
			total = math.Min(total, feedbackBadCeiling)
			bd.Components[ComponentFeedback] = 0
			bd.Feedback = FeedbackBad
		case FeedbackGood:
			total = math.Max(total, feedbackGoodFloor)
			bd.Components[ComponentFeedback] = 1
			bd.Feedback = FeedbackGood
		}
		bd.FeedbackNote = in.Feedback.Note
	}

	return RewardResult{
		Total:            clamp01(total),
		SuccessScore:     success,
		EfficiencyScore:  tokens,
		PromptTokens:     in.PromptTokens,
		CompletionTokens: in.CompletionTokens,
		MessageCount:     int64(len(in.Messages)),
		UserCorrections:  len(bd.PatternHits),
		Breakdown:        bd,
	}
}

// endedOnError reports whether the last assistant/tool activity of the session
// was an error: a failed tool result, or a run that finished with error.
func endedOnError(msgs []messageInfo) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		switch m.role {
		case message.Tool:
			return m.toolErrors > 0
		case message.Assistant:
			return m.finish == message.FinishReasonError
		}
	}
	return false
}

func firstMatch(patterns []*regexp.Regexp, text string) *regexp.Regexp {
	for _, re := range patterns {
		if re.MatchString(text) {
			return re
		}
	}
	return nil
}

func snippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) > maxSnippetLen {
		return string(r[:maxSnippetLen]) + "..."
	}
	return text
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// toolCallInfo is the part of a tool call needed to detect repeats.
type toolCallInfo struct {
	name  string
	input string
}

// messageInfo is a simplified message representation for reward calculation.
type messageInfo struct {
	role       message.MessageRole
	isUser     bool
	text       string
	toolCalls  []toolCallInfo
	toolErrors int
	finish     message.FinishReason
}

// componentSummary renders the measured components for human-readable output
// (", components: toolErrors 0.80, ..."), or "" when there are none.
func (b Breakdown) componentSummary() string {
	order := []string{ComponentToolErrors, ComponentCancels, ComponentRepetition, ComponentTurns, ComponentEndState, ComponentFeedback}
	var parts []string
	for _, name := range order {
		if v, ok := b.Components[name]; ok {
			parts = append(parts, fmt.Sprintf("%s %.2f", name, v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return ", components: " + strings.Join(parts, ", ")
}
