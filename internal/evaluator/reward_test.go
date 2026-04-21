package evaluator

import (
	"math"
	"regexp"
	"testing"
)

func TestCalculateReward_NoCorrections(t *testing.T) {
	msgs := []messageInfo{
		{isUser: true, text: "implement the feature"},
		{isUser: false, text: "Done!"},
	}
	result := calculateReward(msgs, nil, 0, 0.8, 0.2)

	if result.SuccessScore != 1.0 {
		t.Errorf("expected SuccessScore=1.0, got %f", result.SuccessScore)
	}
	if result.UserCorrections != 0 {
		t.Errorf("expected 0 corrections, got %d", result.UserCorrections)
	}
	if result.MessageCount != 2 {
		t.Errorf("expected MessageCount=2, got %d", result.MessageCount)
	}
}

func TestCalculateReward_WithCorrections(t *testing.T) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bwrong\b`),
		regexp.MustCompile(`(?i)\bno[,.]?\b`),
	}
	msgs := []messageInfo{
		{isUser: true, text: "implement feature"},
		{isUser: false, text: "Here it is"},
		{isUser: true, text: "no, that's wrong"},
	}
	result := calculateReward(msgs, patterns, 0, 0.8, 0.2)

	if result.UserCorrections == 0 {
		t.Error("expected corrections to be detected")
	}
	if result.SuccessScore >= 1.0 {
		t.Errorf("expected SuccessScore < 1.0 with corrections, got %f", result.SuccessScore)
	}
}

func TestCalculateReward_TokenEfficiency(t *testing.T) {
	msgs := []messageInfo{
		{isUser: false, promptTokens: 500, completionTokens: 500},
	}
	// baseline 2000: using 1000 tokens is better than baseline -> sTokens > 0.5
	result := calculateReward(msgs, nil, 2000, 0.8, 0.2)

	if result.EfficiencyScore <= 0.5 {
		t.Errorf("expected efficiency > 0.5 when under baseline, got %f", result.EfficiencyScore)
	}
	if result.PromptTokens != 500 || result.CompletionTokens != 500 {
		t.Errorf("unexpected token counts: prompt=%d completion=%d", result.PromptTokens, result.CompletionTokens)
	}
}

func TestCalculateReward_NoBaseline(t *testing.T) {
	msgs := []messageInfo{
		{isUser: false, promptTokens: 1000, completionTokens: 500},
	}
	result := calculateReward(msgs, nil, 0, 0.8, 0.2)

	// With no baseline, sTokens should be 0.5 (neutral)
	if math.Abs(result.EfficiencyScore-0.5) > 1e-9 {
		t.Errorf("expected EfficiencyScore=0.5 with no baseline, got %f", result.EfficiencyScore)
	}
}

func TestUCBScore_NeverUsed(t *testing.T) {
	score := UCBScore(0, 10, 0, 1.41)
	if score != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 for unused template, got %f", score)
	}
}

func TestUCBScore_Formula(t *testing.T) {
	// UCB1 = avgReward + c * sqrt(ln(N) / n)
	// avgReward=0.7, N=10, n=2, c=1.41
	avgReward := 0.7
	score := UCBScore(avgReward, 10, 2, 1.41)
	exploration := 1.41 * math.Sqrt(math.Log(10)/2)
	expected := avgReward + exploration
	if math.Abs(score-expected) > 1e-9 {
		t.Errorf("UCBScore mismatch: expected %f, got %f", expected, score)
	}
}

func TestUCBScore_ZeroTotal(t *testing.T) {
	score := UCBScore(0.5, 0, 3, 1.41)
	// With totalSessions=0, should return avgReward
	if score != 0.5 {
		t.Errorf("expected avgReward=0.5 when totalSessions=0, got %f", score)
	}
}
