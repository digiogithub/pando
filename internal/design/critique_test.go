package design

import "testing"

func TestGateStopsWhenTheScoreClearsTheThreshold(t *testing.T) {
	settings := CritiqueSettings{Enabled: true, MaxRounds: 3, Threshold: 8, Policy: PolicyStandard}
	decision := settings.Gate(Critique{Score: 8.5}, 1)
	if !decision.Pass || decision.Iterate {
		t.Errorf("a passing score kept the loop running: %+v", decision)
	}
}

func TestGateIteratesUntilTheRoundsRunOut(t *testing.T) {
	settings := CritiqueSettings{Enabled: true, MaxRounds: 2, Threshold: 8, Policy: PolicyStandard}

	first := settings.Gate(Critique{Score: 6}, 1)
	if first.Pass || !first.Iterate {
		t.Errorf("round 1 below threshold should iterate: %+v", first)
	}
	last := settings.Gate(Critique{Score: 6}, 2)
	if last.Pass {
		t.Errorf("the gate passed a version that never met the bar: %+v", last)
	}
	if last.Iterate {
		t.Errorf("the loop kept going past its last round: %+v", last)
	}
}

// Strict is the only policy that refuses a high score with errors still in it:
// a 9.5 with an unlabelled control is still an unlabelled control.
func TestStrictPolicyRefusesRemainingErrors(t *testing.T) {
	critique := Critique{Score: 9.6, Issues: []Issue{
		{Code: RuleControlName, Severity: SeverityError, Message: "no name"},
	}}

	strict := CritiqueSettings{Enabled: true, MaxRounds: 3, Policy: PolicyStrict}.WithPolicy(PolicyStrict)
	if decision := strict.Gate(critique, 1); decision.Pass {
		t.Errorf("strict passed with an error outstanding: %+v", decision)
	} else if decision.Blocking != 1 {
		t.Errorf("blocking count = %d, want 1", decision.Blocking)
	}

	standard := CritiqueSettings{Enabled: true, MaxRounds: 3, Threshold: 8, Policy: PolicyStandard}
	if decision := standard.Gate(critique, 1); !decision.Pass {
		t.Errorf("standard should pass on score alone: %+v", decision)
	}
}

// Strict also raises the bar: a project cannot configure a threshold of 5 and
// still call the result strict.
func TestStrictRaisesTheThreshold(t *testing.T) {
	settings := CritiqueSettings{Enabled: true, MaxRounds: 3, Threshold: 5, Policy: PolicyStrict}.normalized()
	if settings.Threshold != strictThreshold {
		t.Errorf("strict threshold = %.1f, want %.1f", settings.Threshold, strictThreshold)
	}
}

func TestNoneScoresButNeverBlocks(t *testing.T) {
	settings := CritiqueSettings{Enabled: true, MaxRounds: 3, Threshold: 8, Policy: PolicyNone}
	decision := settings.Gate(Critique{Score: 2, Issues: []Issue{{Severity: SeverityBlocking}}}, 1)
	if !decision.Pass || decision.Iterate {
		t.Errorf("policy none blocked: %+v", decision)
	}
	if decision.Score != 2 {
		t.Errorf("policy none suppressed the score: %+v", decision)
	}
}

func TestDisabledCritiqueNeverGates(t *testing.T) {
	settings := CritiqueSettings{Enabled: false, MaxRounds: 3, Threshold: 8, Policy: PolicyStrict}
	if decision := settings.Gate(Critique{Score: 0}, 1); !decision.Pass {
		t.Errorf("a disabled critic loop still blocked: %+v", decision)
	}
}

// A skill says what critique it wants; an unknown or absent policy leaves the
// project's own configuration alone.
func TestSkillPolicyOverridesButOnlyWhenItSaysSomething(t *testing.T) {
	base := CritiqueSettings{Enabled: true, MaxRounds: 3, Threshold: 8, Policy: PolicyStandard}

	if got := base.WithPolicy("strict").Policy; got != PolicyStrict {
		t.Errorf("policy = %q, want strict", got)
	}
	if got := base.WithPolicy("").Policy; got != PolicyStandard {
		t.Errorf("an empty policy changed the settings to %q", got)
	}
	if got := base.WithPolicy("whatever-the-template-invented").Policy; got != PolicyStandard {
		t.Errorf("an unknown policy was adopted as %q", got)
	}
}

func TestNormalizeBoundsWhatConfigurationCanAskFor(t *testing.T) {
	settings := CritiqueSettings{Enabled: true, MaxRounds: 999, Threshold: 42, Policy: "nonsense"}.normalized()
	if settings.MaxRounds != maxCritiqueRounds {
		t.Errorf("max rounds = %d, want capped at %d", settings.MaxRounds, maxCritiqueRounds)
	}
	if settings.Threshold != 10 {
		t.Errorf("threshold = %.1f, want capped at 10", settings.Threshold)
	}
	if settings.Policy != PolicyStandard {
		t.Errorf("policy = %q, want the standard default", settings.Policy)
	}
}

// The critic adds judgement; it does not get to re-report a rule the audit
// already fired on the same node.
func TestMergeIssuesDropsEchoesAndKeepsJudgement(t *testing.T) {
	audited := []Issue{
		{Code: RuleContrast, Severity: SeverityError, NodeID: "n1", Message: "text contrast is 2.10:1"},
	}
	written := []Issue{
		{Code: RuleContrast, Severity: SeverityError, NodeID: "n1", Message: "text contrast is 2.10:1"},
		{Severity: SeverityWarning, NodeID: "n4", Message: "the hero says nothing the reader did not already know"},
		{Message: "spacing is inconsistent between the two cards"},
	}

	merged := MergeIssues(audited, written)
	if len(merged) != 3 {
		t.Fatalf("merged %d issues, want 3 (the echo dropped): %+v", len(merged), merged)
	}
	for _, issue := range merged {
		if issue.Severity == "" {
			t.Errorf("a critic issue with no severity was kept unlabelled: %+v", issue)
		}
	}
}

// Neither half of the verdict is allowed to be the whole verdict.
func TestBlendScoreWeightsBothHalves(t *testing.T) {
	if got := BlendScore(10, 0); got != 10 {
		t.Errorf("a critic with no score of its own changed the audit score to %.1f", got)
	}
	if got := BlendScore(10, 5); got != 8 {
		t.Errorf("BlendScore(10, 5) = %.1f, want 8", got)
	}
	if got := BlendScore(4, 10); got <= 4 || got >= 10 {
		t.Errorf("BlendScore(4, 10) = %.1f, want a number between the two", got)
	}
}

func TestDefaultSettingsAreUsableWithoutConfiguration(t *testing.T) {
	settings := DefaultCritiqueSettings()
	if settings.MaxRounds <= 0 || settings.Threshold <= 0 || settings.Policy == "" {
		t.Errorf("defaults are unusable: %+v", settings)
	}
}
