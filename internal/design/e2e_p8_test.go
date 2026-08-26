package design

import (
	"context"
	"strings"
	"testing"
)

// A template says how hard its own gate should be, and that is the whole point
// of carrying od.critique.policy: a landing page is held to a stricter bar than
// a throwaway prototype, without the user configuring anything.
func TestTemplatePolicyDecidesTheGate(t *testing.T) {
	svc, _ := newTestService(t)

	strict := svc.CritiqueSettingsFor("landing-page")
	if strict.Policy != PolicyStrict {
		t.Errorf("landing-page resolved to policy %q, want strict", strict.Policy)
	}
	if strict.Threshold < strictThreshold {
		t.Errorf("a strict template kept the loose threshold %.1f", strict.Threshold)
	}

	if got := svc.CritiqueSettingsFor("deck-basic").Policy; got != PolicyStandard {
		t.Errorf("deck-basic resolved to policy %q, want standard", got)
	}
	if got := svc.CritiqueSettingsFor("design-system-extract").Policy; got != PolicyNone {
		t.Errorf("a workflow template resolved to policy %q, want none", got)
	}
	// A skill Pando knows nothing about must not change the project's own gate.
	if got := svc.CritiqueSettingsFor("some-third-party-skill").Policy; got != DefaultCritiqueSettings().Policy {
		t.Errorf("an unknown skill changed the policy to %q", got)
	}
}

// A machine with no Chromium still gets the design-system half of the audit —
// and must be told which half it did not get. A partial score presented as a
// whole one is the failure worth guarding.
func TestCritiqueWithoutABrowserSaysWhatItCouldNotCheck(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, _, err := svc.SaveSystem(DefaultDesignSystem()); err != nil {
		t.Fatalf("save system: %v", err)
	}

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Unlinked",
		Kind:  KindWeb,
		Files: map[string]string{
			"index.html": "<!doctype html><html><head><title>Unlinked</title></head>" +
				"<body><h1>Hello</h1></body></html>",
			"style.css": "body { color: #1a1a1a; }",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	report, err := svc.Critique(ctx, artifact.ID, CritiqueOptions{SkipRender: true, Record: true})
	if err != nil {
		t.Fatalf("critique: %v", err)
	}
	if report.Rendered {
		t.Error("SkipRender still rendered")
	}
	if report.RenderError == "" || !strings.Contains(report.Critique.Summary, "design-system checks only") {
		t.Errorf("the report does not say which rules did not run: %+v", report)
	}
	if report.Audit.Counts[RuleSystemUnlinked] == 0 {
		t.Errorf("an artifact that does not link the committed system passed: %v", report.Audit.Counts)
	}
	if report.Audit.Counts[RuleContrast] != 0 {
		t.Error("a rule that needs a render fired without one")
	}
	if !report.Recorded {
		t.Error("the pass was not recorded")
	}

	// The audit must never be the thing that edits what it audits: running it
	// twice has to give the same answer.
	again, err := svc.Critique(ctx, artifact.ID, CritiqueOptions{SkipRender: true, Record: false})
	if err != nil {
		t.Fatalf("second critique: %v", err)
	}
	if again.Audit.Score != report.Audit.Score {
		t.Errorf("a second pass scored %.1f after the first scored %.1f; the audit changed the artifact",
			again.Audit.Score, report.Audit.Score)
	}
	if again.Audit.Counts[RuleSystemUnlinked] == 0 {
		t.Error("the first pass linked the stylesheet it was only supposed to report on")
	}
}

// The critic's judgement and the automated evidence end up in one record, and
// the version history is where a surface reads it back from.
func TestCriticJudgementJoinsTheAutomatedEvidence(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Judged",
		Kind:  KindWeb,
		Files: map[string]string{"index.html": "<!doctype html><html><head><title>t</title></head><body><p>x</p></body></html>"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	report, err := svc.Critique(ctx, artifact.ID, CritiqueOptions{
		SkipRender: true,
		Record:     true,
		Score:      4,
		Summary:    "the layout is a stack of boxes with nothing leading the eye",
		Issues: []Issue{{
			Severity: SeverityWarning,
			Message:  "the hero repeats the page title and adds nothing",
			Fix:      "say what the product does for the reader instead",
		}},
	})
	if err != nil {
		t.Fatalf("critique: %v", err)
	}
	if report.Critique.Score >= report.Audit.Score {
		t.Errorf("a critic scoring 4 did not pull the %.1f automated score down: %.1f",
			report.Audit.Score, report.Critique.Score)
	}
	if !strings.Contains(report.Critique.Summary, "stack of boxes") {
		t.Errorf("the critic's summary was replaced by the automated one: %q", report.Critique.Summary)
	}

	versions, err := svc.Versions(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions) == 0 || versions[0].Critique == nil {
		t.Fatalf("the version history carries no critique: %+v", versions)
	}
	found := false
	for _, issue := range versions[0].Critique.Issues {
		if strings.Contains(issue.Message, "repeats the page title") {
			found = true
		}
	}
	if !found {
		t.Errorf("the critic's own finding did not survive the round trip: %+v", versions[0].Critique.Issues)
	}
}

// A pass that could not render must not be allowed to declare the artifact
// finished: two thirds of the rules did not run, so a clean score is a score
// for the checks that happened, not a verdict on the design.
func TestAHeadlessPassNeverPasses(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Headless",
		Kind:  KindWeb,
		Files: map[string]string{"index.html": "<!doctype html><html><head><title>t</title></head><body><h1>t</h1></body></html>"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	gated, err := svc.Critique(ctx, artifact.ID, CritiqueOptions{SkipRender: true})
	if err != nil {
		t.Fatalf("critique: %v", err)
	}
	if gated.Audit.Score != 10 {
		t.Fatalf("the design-system checks did not pass cleanly: %+v", gated.Audit)
	}
	if gated.Decision.Pass || gated.Decision.Iterate {
		t.Errorf("a headless pass reached a verdict: %+v", gated.Decision)
	}
	if !strings.Contains(gated.Decision.Reason, "not enough to call it finished") {
		t.Errorf("the reason does not explain itself: %q", gated.Decision.Reason)
	}

	// A policy that does not gate is a project saying it does not want a
	// verdict, and that answer stays honest with no browser too.
	ungated, err := svc.Critique(ctx, artifact.ID, CritiqueOptions{SkipRender: true, Policy: PolicyNone})
	if err != nil {
		t.Fatalf("critique: %v", err)
	}
	if !ungated.Decision.Pass {
		t.Errorf("policy none started blocking: %+v", ungated.Decision)
	}
}
