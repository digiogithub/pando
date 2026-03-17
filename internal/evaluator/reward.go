package evaluator

import (
	"math"
	"regexp"
)

// compilePatterns pre-compiles correction detection patterns.
func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		result = append(result, re)
	}
	return result, nil
}

// calculateReward computes R = alpha*S_success + beta*S_tokens for a session.
// It receives the list of messages for the session.
func calculateReward(
	messages []messageInfo,
	patterns []*regexp.Regexp,
	baseline float64,
	alphaWeight, betaWeight float64,
) RewardResult {
	corrections := 0
	var promptTokens, completionTokens int64
	msgCount := int64(len(messages))

	for _, msg := range messages {
		if msg.isUser && len(patterns) > 0 {
			for _, re := range patterns {
				if re.MatchString(msg.text) {
					corrections++
					break
				}
			}
		}
		promptTokens += msg.promptTokens
		completionTokens += msg.completionTokens
	}

	// S_success: 1.0 if no corrections, degrades by 0.3 per correction
	sSuccess := 1.0
	if corrections > 0 {
		sSuccess = math.Max(0, 1.0-float64(corrections)*0.3)
	}

	// S_tokens: normalized efficiency vs rolling baseline
	sTokens := 0.5 // neutral if no baseline
	totalTokens := float64(promptTokens + completionTokens)
	if baseline > 0 && totalTokens > 0 {
		sTokens = math.Max(0, math.Min(1, 1.0-(totalTokens-baseline)/baseline))
	}

	total := alphaWeight*sSuccess + betaWeight*sTokens

	return RewardResult{
		Total:            total,
		SuccessScore:     sSuccess,
		EfficiencyScore:  sTokens,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		MessageCount:     msgCount,
		UserCorrections:  corrections,
	}
}

// messageInfo is a simplified message representation for reward calculation.
type messageInfo struct {
	isUser           bool
	text             string
	promptTokens     int64
	completionTokens int64
}
