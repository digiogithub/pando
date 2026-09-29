package config

import (
	"regexp"
	"testing"
)

func TestResolvedWeights_MigratesLegacyAlphaBeta(t *testing.T) {
	w := EvaluatorConfig{AlphaWeight: 0.6, BetaWeight: 0.4}.ResolvedWeights()
	if w.Success != 0.6 || w.Tokens != 0.4 {
		t.Errorf("legacy keys not migrated: %+v", w)
	}
	if w.ToolErrors != DefaultEvaluatorWeights().ToolErrors {
		t.Errorf("new components must take defaults: %+v", w)
	}
	explicit := EvaluatorConfig{AlphaWeight: 0.6, Weights: EvaluatorWeights{Success: 1, Tokens: 1}}.ResolvedWeights()
	if explicit.Success != 1 || explicit.ToolErrors != 0 {
		t.Errorf("explicit weights must win untouched: %+v", explicit)
	}
	if got := (EvaluatorConfig{}).ResolvedWeights(); got.Success != 0.8 || got.Tokens != 0.2 {
		t.Errorf("unset config should keep 0.8/0.2: %+v", got)
	}
}

func TestEvaluatorWithDefaults_FillsWeightsAndPatterns(t *testing.T) {
	e := EvaluatorWithDefaults(EvaluatorConfig{})
	if e.Weights.IsZero() || len(e.CorrectionsPatterns) == 0 {
		t.Fatalf("defaults missing: %+v", e)
	}
	for _, p := range e.CorrectionsPatterns {
		re := regexp.MustCompile(p)
		for _, neutral := range []string{"no", "no problem", "no, that's fine", "no pasa nada"} {
			if re.MatchString(neutral) {
				t.Errorf("default pattern %q matches neutral turn %q", p, neutral)
			}
		}
	}
}
