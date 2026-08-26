package design

import (
	"strings"
	"testing"
)

// codes returns the rules that fired, so a test can assert on what the audit
// found without depending on the prose it wrote.
func codes(result AuditResult) map[string]int {
	return result.Counts
}

func has(t *testing.T, result AuditResult, code string) {
	t.Helper()
	if result.Counts[code] == 0 {
		t.Errorf("rule %s did not fire; fired: %v", code, result.Counts)
	}
}

func hasNot(t *testing.T, result AuditResult, code string) {
	t.Helper()
	if result.Counts[code] != 0 {
		t.Errorf("rule %s fired and should not have; fired: %v", code, result.Counts)
	}
}

// A clean render must score a clean ten. Without this the score has no ceiling
// anybody has verified, and every later assertion about improvement is relative
// to a number nobody checked.
func TestACleanRenderScoresTen(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb,
		Title:    "Pricing",
		Viewport: Viewport{W: 1440, H: 900},
		Width:    1440,
		Nodes:    []Node{{NodeID: "n1"}},
		Facts: []NodeFacts{
			{NodeID: "n1", Tag: "h1", Name: "Pricing", HeadingLevel: 1, HasText: true,
				Color: "rgb(17, 17, 17)", Background: "rgb(255, 255, 255)", FontSize: 40, FontWeight: 700},
			{NodeID: "n2", Tag: "button", Name: "Start free", Interactive: true, HasText: true,
				Color: "rgb(255, 255, 255)", Background: "rgb(20, 40, 120)", FontSize: 16, FontWeight: 500,
				Box: Rect{W: 160, H: 44}},
			{NodeID: "n3", Tag: "img", Name: "Team at work", AltPresent: true},
		},
	})
	if result.Score != 10 {
		t.Errorf("score = %.1f, want 10: %v", result.Score, result.Counts)
	}
	if len(result.Issues) != 0 {
		t.Errorf("clean render produced issues: %+v", result.Issues)
	}
	if !strings.Contains(result.Summary, "no automated issues") {
		t.Errorf("summary = %q", result.Summary)
	}
}

func TestAccessibilityRulesFireOnTheThingsTheyName(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb,
		Title:    "",
		Viewport: Viewport{W: 1440, H: 900},
		Width:    1900,
		Nodes:    []Node{{NodeID: "n1"}},
		Facts: []NodeFacts{
			// An image with no alt attribute at all.
			{NodeID: "n1", Tag: "img"},
			// A button with no accessible name, and too small to hit.
			{NodeID: "n2", Tag: "button", Interactive: true, Box: Rect{W: 16, H: 16}},
			// Grey on white: 2.85:1, below the 4.5 body-text minimum.
			{NodeID: "n3", Tag: "p", Name: "fine print", HasText: true,
				Color: "rgb(150, 150, 150)", Background: "rgb(255, 255, 255)", FontSize: 14, FontWeight: 400},
			// h2 then h4 skips a level, and there is no h1 anywhere.
			{NodeID: "n4", Tag: "h2", Name: "Plans", HeadingLevel: 2, HasText: true,
				Color: "rgb(0, 0, 0)", Background: "rgb(255, 255, 255)", FontSize: 32, FontWeight: 700},
			{NodeID: "n5", Tag: "h4", Name: "Team", HeadingLevel: 4, HasText: true,
				Color: "rgb(0, 0, 0)", Background: "rgb(255, 255, 255)", FontSize: 20, FontWeight: 700},
		},
	})

	for _, code := range []string{
		RuleImageAlt, RuleControlName, RuleTapTarget, RuleContrast,
		RuleHeadingOrder, RuleMissingH1, RuleDocumentTitle, RuleHorizontalOverflow,
	} {
		has(t, result, code)
	}
	if result.Score >= 8 {
		t.Errorf("a page failing eight rules scored %.1f", result.Score)
	}
	for _, issue := range result.Issues {
		if issue.Fix == "" {
			t.Errorf("issue %s has no fix; a finding nobody can act on is noise: %+v", issue.Code, issue)
		}
	}
}

// alt="" is a decision, not an omission: it says the image is decorative. An
// audit that cannot tell the two apart makes correct markup unfixable.
func TestDecorativeImagesAreNotAnAccessibilityFailure(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb,
		Title:    "Home",
		Viewport: Viewport{W: 1440, H: 900},
		Width:    1440,
		Nodes:    []Node{{NodeID: "n1"}},
		Facts:    []NodeFacts{{NodeID: "n1", Tag: "img", AltPresent: true, Name: ""}},
	})
	hasNot(t, result, RuleImageAlt)
}

// An element hidden from assistive technology cannot fail an assistive
// technology rule.
func TestAriaHiddenSubtreesAreSkipped(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb,
		Title:    "Home",
		Viewport: Viewport{W: 1440, H: 900},
		Width:    1440,
		Nodes:    []Node{{NodeID: "n1"}},
		Facts: []NodeFacts{
			{NodeID: "n1", Tag: "button", Interactive: true, AriaHidden: true, Box: Rect{W: 8, H: 8}},
		},
	})
	hasNot(t, result, RuleControlName)
	hasNot(t, result, RuleTapTarget)
}

// Large text is allowed a lower contrast ratio, and the audit has to honour the
// same exception the standard grants — otherwise every hero headline is a bug.
func TestLargeTextUsesTheLowerContrastMinimum(t *testing.T) {
	// 3.5:1 — passes at large size, fails at body size.
	fact := NodeFacts{NodeID: "n1", Tag: "p", Name: "Ship faster", HasText: true,
		Color: "rgb(122, 122, 122)", Background: "rgb(255, 255, 255)", FontWeight: 400}

	fact.FontSize = 48
	large := Audit(AuditInput{Rendered: true, Kind: KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}}, Facts: []NodeFacts{fact}})
	hasNot(t, large, RuleContrast)

	fact.FontSize = 14
	small := Audit(AuditInput{Rendered: true, Kind: KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}}, Facts: []NodeFacts{fact}})
	has(t, small, RuleContrast)
}

// A colour the parser cannot read is unknown, not failing. Reporting unknown as
// a failure teaches the reader to ignore the rule.
func TestUnparseableColoursDoNotProduceContrastFindings(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}},
		Facts: []NodeFacts{{NodeID: "n1", Tag: "p", Name: "text", HasText: true,
			Color: "color(display-p3 0.2 0.2 0.2)", Background: "rebeccapurple", FontSize: 16}},
	})
	hasNot(t, result, RuleContrast)
}

func TestRuntimeErrorsAndFailedRequestsAreFindings(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}},
		Console: []ConsoleEntry{
			{Level: "error", Message: "Uncaught TypeError: x is not a function"},
			{Level: "log", Message: "hello"},
		},
		Failures: []NetworkFailure{{URL: "https://example.test/hero.png", Status: 404}},
	})
	has(t, result, RuleConsoleError)
	has(t, result, RuleNetworkFailure)
	if result.Counts[RuleConsoleError] != 1 {
		t.Errorf("a console.log was scored as an error: %v", result.Counts)
	}
}

func TestDeckRulesGuardWhatPDFExportDependsOn(t *testing.T) {
	empty := Audit(AuditInput{Rendered: true, Kind: KindDeck, Title: "Deck", Nodes: []Node{{NodeID: "n1"}}})
	has(t, empty, RuleDeckNoSlides)

	runTogether := Audit(AuditInput{
		Rendered: true,
		Kind:     KindDeck, Title: "Deck", Slides: 3, Nodes: []Node{{NodeID: "n1"}},
		Breaks: []SlideBreak{
			{Index: 0, BreakAfter: "page"},
			{Index: 1, BreakAfter: "auto"},
			{Index: 2, BreakAfter: "auto"},
		},
	})
	// The last slide needs no break after it; only slide 2 is at fault.
	if got := runTogether.Counts[RuleDeckPageBreak]; got != 1 {
		t.Errorf("page-break rule fired %d time(s), want 1: %+v", got, runTogether.Issues)
	}
	hasNot(t, runTogether, RuleDeckNoSlides)

	// The rule cannot run at all without a print pass, and not running is not
	// the same as passing.
	noPrintPass := Audit(AuditInput{Rendered: true, Kind: KindDeck, Title: "Deck", Slides: 3, Nodes: []Node{{NodeID: "n1"}}})
	hasNot(t, noPrintPass, RuleDeckPageBreak)
}

func TestDesignSystemBreachesAreFindings(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}},
		RequiresSystem: true,
		SystemLinked:   false,
		SystemFindings: []SystemFinding{
			{File: "style.css", Line: 12, Property: "color", Value: "#1a1a1a", Token: "--color-text"},
		},
	})
	has(t, result, RuleSystemUnlinked)
	has(t, result, RuleSystemHardcoded)

	for _, issue := range result.Issues {
		if issue.Code == RuleSystemHardcoded && !strings.Contains(issue.Fix, "--color-text") {
			t.Errorf("the hardcoded finding does not name the token that replaces it: %+v", issue)
		}
	}
}

// A template that opts out of the design system is not in breach of it.
func TestAnArtifactThatNeedsNoSystemIsNotUnlinked(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}},
		RequiresSystem: false, SystemLinked: false,
	})
	hasNot(t, result, RuleSystemUnlinked)
}

// One rule breaking forty times must not read as forty lines, and must not cost
// more than a page that fails to render at all.
func TestARepeatedRuleIsFoldedAndCapped(t *testing.T) {
	facts := make([]NodeFacts, 0, 40)
	for i := 0; i < 40; i++ {
		facts = append(facts, NodeFacts{NodeID: "n" + string(rune('a'+i%26)), Tag: "img"})
	}
	result := Audit(AuditInput{Rendered: true, Kind: KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}}, Facts: facts})

	if got := len(result.Issues); got != maxIssuesPerRule {
		t.Errorf("reported %d issues for one rule, want %d folded", got, maxIssuesPerRule)
	}
	if result.Counts[RuleImageAlt] != 40 {
		t.Errorf("the fold lost the real count: %v", result.Counts)
	}
	empty := Audit(AuditInput{Rendered: true, Kind: KindWeb, Title: "t"})
	if result.Score <= empty.Score {
		t.Errorf("forty missing alts (%.1f) scored no better than a page that rendered nothing (%.1f)",
			result.Score, empty.Score)
	}
}

// Worst first: the reader works down the list, and a blocking finding under a
// warning is a finding nobody reaches.
func TestIssuesAreOrderedWorstFirst(t *testing.T) {
	result := Audit(AuditInput{
		Rendered: true,
		Kind:     KindDeck, Title: "", Nodes: nil,
		Console: []ConsoleEntry{{Level: "error", Message: "boom"}},
	})
	if len(result.Issues) < 2 {
		t.Fatalf("expected several issues, got %+v", result.Issues)
	}
	if result.Issues[0].Severity != SeverityBlocking {
		t.Errorf("first issue is %s, want the blocking one first: %+v",
			result.Issues[0].Severity, result.Issues)
	}
}

func TestContrastRatioMatchesTheStandard(t *testing.T) {
	cases := []struct {
		fg, bg string
		want   float64
	}{
		{"rgb(0, 0, 0)", "rgb(255, 255, 255)", 21},
		{"#ffffff", "#ffffff", 1},
		{"#767676", "#ffffff", 4.54},
		{"rgba(0, 0, 0, 0.5)", "rgb(255, 255, 255)", 3.98},
	}
	for _, c := range cases {
		fg, ok := ParseCSSColor(c.fg)
		if !ok {
			t.Fatalf("cannot parse %q", c.fg)
		}
		bg, ok := ParseCSSColor(c.bg)
		if !ok {
			t.Fatalf("cannot parse %q", c.bg)
		}
		if got := ContrastRatio(fg, bg); got < c.want-0.05 || got > c.want+0.05 {
			t.Errorf("ContrastRatio(%s, %s) = %.2f, want %.2f", c.fg, c.bg, got, c.want)
		}
	}
}

func TestParseCSSColorRejectsWhatItCannotRead(t *testing.T) {
	for _, value := range []string{"", "rebeccapurple", "transparent", "color(srgb 1 0 0)", "#12345"} {
		if _, ok := ParseCSSColor(value); ok {
			t.Errorf("ParseCSSColor(%q) claimed to understand it", value)
		}
	}
	for _, value := range []string{"#abc", "#AABBCC", "rgb(1,2,3)", "rgb(1 2 3 / 0.5)", "rgba(1, 2, 3, 1)"} {
		if _, ok := ParseCSSColor(value); !ok {
			t.Errorf("ParseCSSColor(%q) failed on a value a computed style produces", value)
		}
	}
}

func TestAuditCountsSurviveTheIssueFold(t *testing.T) {
	result := Audit(AuditInput{Rendered: true, Kind: KindWeb, Title: "t", Nodes: []Node{{NodeID: "n1"}}})
	if codes(result) == nil {
		t.Error("counts are nil even for a clean run")
	}
}

// Without a render there is nothing to look at, and every rule that reads one
// must stay silent. A "the document has no title" finding produced because
// nobody loaded the document is a finding about the audit, not the artifact.
func TestRulesThatNeedARenderDoNotFireWithoutOne(t *testing.T) {
	result := Audit(AuditInput{
		Kind:           KindDeck,
		RequiresSystem: true,
		SystemLinked:   false,
		SystemFindings: []SystemFinding{
			{File: "style.css", Line: 3, Property: "color", Value: "#000", Token: "--color-text"},
		},
		// Everything below reads a render that never happened.
		Console:  []ConsoleEntry{{Level: "error", Message: "boom"}},
		Failures: []NetworkFailure{{URL: "x", Status: 404}},
		Facts:    []NodeFacts{{NodeID: "n1", Tag: "img"}},
	})

	for _, code := range []string{
		RuleEmptyDocument, RuleDocumentTitle, RuleConsoleError, RuleNetworkFailure,
		RuleImageAlt, RuleDeckNoSlides,
	} {
		hasNot(t, result, code)
	}
	has(t, result, RuleSystemUnlinked)
	has(t, result, RuleSystemHardcoded)
}
