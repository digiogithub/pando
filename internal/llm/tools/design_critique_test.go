package tools

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/design"
)

func TestDesignCritiqueToolAdvertisesTheLoop(t *testing.T) {
	info := NewDesignCritiqueTool().Info()
	if info.Name != DesignCritiqueToolName {
		t.Fatalf("name = %q", info.Name)
	}
	if len(info.Required) != 1 || info.Required[0] != "artifact_id" {
		t.Errorf("required = %v, want just artifact_id", info.Required)
	}
	for _, param := range []string{"policy", "score", "issues", "skip_render", "record"} {
		if _, ok := info.Parameters[param]; !ok {
			t.Errorf("the tool does not expose %q, so half the loop is unreachable", param)
		}
	}
	// The description is the only place the model learns that fixing the files
	// is the response to a finding, not arguing with the score.
	for _, phrase := range []string{"design_versions", "strict", "iterate"} {
		if !strings.Contains(info.Description, phrase) {
			t.Errorf("the description never mentions %q", phrase)
		}
	}
}

// The verdict has to be readable in the first line: an agent that has to parse
// a table to learn whether it is done will keep iterating.
func TestCritiqueDescriptionLeadsWithTheDecision(t *testing.T) {
	report := design.CritiqueReport{
		Artifact: design.Artifact{Title: "Landing"},
		Version:  2,
		Rendered: true,
		Audit: design.AuditResult{
			Score:  6.5,
			Counts: map[string]int{design.RuleContrast: 9},
			Issues: []design.Issue{
				{Code: design.RuleContrast, Severity: design.SeverityError, NodeID: "n3",
					Message: "text contrast is 2.10:1", Fix: "darken the text"},
			},
		},
		Critique: design.Critique{Version: 2, Score: 6.5, Issues: []design.Issue{
			{Code: design.RuleContrast, Severity: design.SeverityError, NodeID: "n3",
				Message: "text contrast is 2.10:1", Fix: "darken the text"},
		}},
		Decision: design.GateDecision{
			Iterate: true, Reason: "scored 6.5/10, below the 8.0 threshold",
			Round: 2, MaxRounds: 3, Threshold: 8, Policy: design.PolicyStandard,
		},
	}

	out := describeCritique(report)
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, "6.5/10") || !strings.Contains(first, "Landing") {
		t.Errorf("the first line does not carry the verdict: %q", first)
	}
	for _, want := range []string{"ITERATE", "n3", "darken the text", "design_versions"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report never mentions %q:\n%s", want, out)
		}
	}
	// Nine contrast failures reported as one line would read as one problem.
	if !strings.Contains(out, "9 total") {
		t.Errorf("the fold is invisible, so the reader thinks there is one finding:\n%s", out)
	}
}

// A pass that never rendered scored a fraction of the artifact. Saying so is
// the difference between a degraded check and a wrong one.
func TestCritiqueDescriptionFlagsAPassThatCouldNotRender(t *testing.T) {
	out := describeCritique(design.CritiqueReport{
		Artifact:    design.Artifact{Title: "Headless"},
		Version:     1,
		Rendered:    false,
		RenderError: "design: no browser available",
		Decision:    design.GateDecision{Pass: true, Reason: "scored 10.0/10", Policy: design.PolicyStandard},
	})
	if !strings.Contains(out, "NOT RENDERED") || !strings.Contains(out, "no browser available") {
		t.Errorf("a headless pass does not say what it skipped:\n%s", out)
	}
}

func TestStoredCritiqueReadsBackWithoutARender(t *testing.T) {
	out := describeStoredCritique(design.Critique{
		Version: 3, Score: 9.2, Summary: "clean",
		Issues: []design.Issue{{Severity: design.SeverityWarning, Message: "the footer is cramped"}},
	})
	for _, want := range []string{"v3", "9.2", "clean", "cramped"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stored critique never mentions %q:\n%s", want, out)
		}
	}
}

func TestDecisionWordCoversEveryOutcome(t *testing.T) {
	cases := map[string]design.GateDecision{
		"PASS":    {Pass: true},
		"ITERATE": {Iterate: true},
		"STOP":    {},
	}
	for want, decision := range cases {
		if got := decisionWord(decision); got != want {
			t.Errorf("decisionWord(%+v) = %q, want %q", decision, got, want)
		}
	}
}
