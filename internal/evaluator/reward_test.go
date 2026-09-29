package evaluator

import (
	"math"
	"regexp"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/message"
)

func defaultTestPatterns(t *testing.T) []*regexp.Regexp {
	t.Helper()
	p, err := compilePatterns(config.DefaultCorrectionsPatterns())
	if err != nil {
		t.Fatalf("compile default patterns: %v", err)
	}
	return p
}

func user(text string) messageInfo {
	return messageInfo{role: message.User, isUser: true, text: text}
}

func assistant() messageInfo {
	return messageInfo{role: message.Assistant, finish: message.FinishReasonEndTurn, text: "ok"}
}

func legacyWeights() config.EvaluatorWeights {
	return config.EvaluatorWeights{Success: 0.8, Tokens: 0.2}
}

// TestCorrectionPatternsFixture documents which real-world (anonymised) user
// turns the default patterns flag as corrections. Bare negations must not match.
func TestCorrectionPatternsFixture(t *testing.T) {
	patterns := defaultTestPatterns(t)
	cases := []struct {
		text string
		hit  bool
	}{
		// Not corrections.
		{"no", false},
		{"No.", false},
		{"no problem, thanks", false},
		{"no, that's fine", false},
		{"no pasa nada, sigue con lo siguiente", false},
		{"no hace falta que lo documentes", false},
		{"vuelve a ejecutar los tests cuando termines", false},
		{"looks good, now add the same for the users endpoint", false},
		{"perfecto, gracias", false},
		{"can you explain how the cache works?", false},
		// Corrections.
		{"that's wrong, the field is called user_id", true},
		{"that's not what I asked, I wanted only the README", true},
		{"you missed the second file", true},
		{"undo that", true},
		{"revert the last change please", true},
		{"the build is still failing", true},
		{"it's still broken after your fix", true},
		{"eso no es lo que pedí", true},
		{"no era eso, quería el otro módulo", true},
		{"te equivocaste de fichero", true},
		{"esto está mal, faltan los tests", true},
		{"sigue fallando el login", true},
		{"deshazlo, por favor", true},
	}
	for _, c := range cases {
		got := firstMatch(patterns, c.text) != nil
		if got != c.hit {
			t.Errorf("text %q: hit=%v, want %v", c.text, got, c.hit)
		}
	}
}

func TestLegacyBareNegationPatternDropped(t *testing.T) {
	patterns, err := compilePatterns([]string{`(?i)\bno[,.]?\b`, `(?i)\bwrong\b`})
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected the legacy bare-negation pattern to be dropped, got %d patterns", len(patterns))
	}
	if firstMatch(patterns, "no problem") != nil {
		t.Error("bare negation must not match")
	}
}

func TestCalculateReward_FirstUserTurnNeverCorrection(t *testing.T) {
	msgs := []messageInfo{user("that's wrong, fix the parser"), assistant(), user("thanks")}
	r := calculateReward(rewardInput{Messages: msgs, Patterns: defaultTestPatterns(t), Weights: legacyWeights()})
	if r.UserCorrections != 0 || r.SuccessScore != 1.0 {
		t.Errorf("first turn must be ignored: corrections=%d success=%f", r.UserCorrections, r.SuccessScore)
	}
}

func TestCalculateReward_CorrectionWeightAndCap(t *testing.T) {
	p := defaultTestPatterns(t)
	// One correction right after an assistant turn.
	one := calculateReward(rewardInput{
		Messages: []messageInfo{user("do x"), assistant(), user("that's wrong")},
		Patterns: p, Weights: legacyWeights(),
	})
	if math.Abs(one.SuccessScore-(1-correctionPenaltyAfterAssistant)) > 1e-9 {
		t.Errorf("success = %f, want %f", one.SuccessScore, 1-correctionPenaltyAfterAssistant)
	}
	if len(one.Breakdown.PatternHits) != 1 || !one.Breakdown.PatternHits[0].AfterAssistant {
		t.Errorf("expected one after-assistant hit, got %+v", one.Breakdown.PatternHits)
	}
	// A correction not directly after an assistant turn weighs less.
	queued := calculateReward(rewardInput{
		Messages: []messageInfo{user("do x"), user("that's wrong")},
		Patterns: p, Weights: legacyWeights(),
	})
	if queued.SuccessScore <= one.SuccessScore {
		t.Errorf("queued correction should weigh less: %f vs %f", queued.SuccessScore, one.SuccessScore)
	}
	// Many corrections hit the cap.
	msgs := []messageInfo{user("start")}
	for i := 0; i < 10; i++ {
		msgs = append(msgs, assistant(), user("that's wrong"))
	}
	many := calculateReward(rewardInput{Messages: msgs, Patterns: p, Weights: legacyWeights()})
	if math.Abs(many.SuccessScore-(1-maxCorrectionPenalty)) > 1e-9 {
		t.Errorf("success = %f, want capped at %f", many.SuccessScore, 1-maxCorrectionPenalty)
	}
}

func TestCalculateReward_TokenEfficiency(t *testing.T) {
	msgs := []messageInfo{user("a"), assistant()}
	r := calculateReward(rewardInput{Messages: msgs, Baseline: 2000, PromptTokens: 500, CompletionTokens: 500, Weights: legacyWeights()})
	if r.EfficiencyScore <= 0.5 {
		t.Errorf("expected efficiency > 0.5 under baseline, got %f", r.EfficiencyScore)
	}
	if r.PromptTokens != 500 || r.CompletionTokens != 500 {
		t.Errorf("unexpected token counts: %d/%d", r.PromptTokens, r.CompletionTokens)
	}
}

func TestCalculateReward_NoBaselineNeutralAndUnweighted(t *testing.T) {
	msgs := []messageInfo{user("a"), assistant()}
	r := calculateReward(rewardInput{Messages: msgs, PromptTokens: 1000, CompletionTokens: 500, Weights: legacyWeights()})
	if math.Abs(r.EfficiencyScore-0.5) > 1e-9 {
		t.Errorf("expected neutral 0.5 without baseline, got %f", r.EfficiencyScore)
	}
	if _, weighted := r.Breakdown.Weights[ComponentTokens]; weighted {
		t.Error("tokens must not be weighted without a baseline")
	}
}

func TestCalculateReward_PersistedSignals(t *testing.T) {
	tc := func(name, in string) toolCallInfo { return toolCallInfo{name: name, input: in} }
	msgs := []messageInfo{
		user("run it"),
		{role: message.Assistant, toolCalls: []toolCallInfo{tc("bash", "ls"), tc("bash", "ls"), tc("view", "a.go")}, finish: message.FinishReasonToolUse},
		{role: message.Tool, toolErrors: 1},
		{role: message.Assistant, finish: message.FinishReasonCanceled},
	}
	w := config.DefaultEvaluatorWeights()
	r := calculateReward(rewardInput{Messages: msgs, Patterns: nil, Weights: w})
	bd := r.Breakdown
	if bd.ToolCalls != 3 || bd.ToolErrors != 1 || bd.Repeats != 1 || bd.Cancels != 1 {
		t.Errorf("unexpected counters: %+v", bd)
	}
	if got := bd.Components[ComponentToolErrors]; math.Abs(got-(1-1.0/3)) > 1e-9 {
		t.Errorf("toolErrors = %f", got)
	}
	if got := bd.Components[ComponentCancels]; math.Abs(got-0.5) > 1e-9 {
		t.Errorf("cancels = %f", got)
	}
	if bd.Components[ComponentRepetition] >= 1 {
		t.Errorf("repetition should be penalised: %f", bd.Components[ComponentRepetition])
	}
	if bd.Components[ComponentEndState] != 1 {
		t.Errorf("last activity was a cancel, not an error: endState=%f", bd.Components[ComponentEndState])
	}
	for name, v := range bd.Components {
		if v < 0 || v > 1 {
			t.Errorf("component %s out of [0,1]: %f", name, v)
		}
	}
	if r.Total < 0 || r.Total > 1 {
		t.Errorf("total out of [0,1]: %f", r.Total)
	}

	// Ending right after a failed tool call zeroes endState.
	endErr := []messageInfo{user("x"), {role: message.Assistant, toolCalls: []toolCallInfo{tc("bash", "x")}}, {role: message.Tool, toolErrors: 1}}
	r = calculateReward(rewardInput{Messages: endErr, Weights: w})
	if r.Breakdown.Components[ComponentEndState] != 0 {
		t.Errorf("endState = %f, want 0", r.Breakdown.Components[ComponentEndState])
	}
}

func TestCalculateReward_FeedbackDominates(t *testing.T) {
	good := []messageInfo{user("a"), assistant(), user("thanks")}
	bad := calculateReward(rewardInput{Messages: good, Weights: config.DefaultEvaluatorWeights(), Feedback: &Feedback{Rating: FeedbackBad, Note: "nope"}})
	if bad.Total >= 0.3 {
		t.Errorf("bad feedback total = %f, want < 0.3", bad.Total)
	}
	if bad.Breakdown.Feedback != FeedbackBad || bad.Breakdown.FeedbackNote != "nope" {
		t.Errorf("feedback not recorded: %+v", bad.Breakdown)
	}

	// A session full of corrections and tool errors still scores > 0.8 with good feedback.
	msgs := []messageInfo{user("a")}
	for i := 0; i < 5; i++ {
		msgs = append(msgs, assistant(), user("that's wrong"))
	}
	g := calculateReward(rewardInput{Messages: msgs, Patterns: defaultTestPatterns(t), Weights: config.DefaultEvaluatorWeights(), Feedback: &Feedback{Rating: FeedbackGood}})
	if g.Total <= 0.8 {
		t.Errorf("good feedback total = %f, want > 0.8", g.Total)
	}
}

func TestBreakdownJSONRoundTrip(t *testing.T) {
	r := calculateReward(rewardInput{Messages: []messageInfo{user("a"), assistant(), user("that's wrong")}, Patterns: defaultTestPatterns(t), Weights: legacyWeights()})
	js := r.Breakdown.JSON()
	if js == "" || js == "{}" {
		t.Fatalf("empty JSON: %q", js)
	}
}

func TestUCBScore_NeverUsed(t *testing.T) {
	score := UCBScore(0, 10, 0, 1.41)
	if score != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 for unused template, got %f", score)
	}
}

func TestUCBScore_Formula(t *testing.T) {
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
	if score != 0.5 {
		t.Errorf("expected avgReward=0.5 when totalSessions=0, got %f", score)
	}
}
