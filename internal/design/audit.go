package design

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Audit rule codes. They are stable identifiers: an issue travels to the UI, to
// the agent and into the critique history, and all three need to be able to
// group and suppress findings by something other than their prose.
const (
	RuleImageAlt           = "a11y.image-alt"
	RuleControlName        = "a11y.control-name"
	RuleHeadingOrder       = "a11y.heading-order"
	RuleMissingH1          = "a11y.missing-h1"
	RuleContrast           = "a11y.contrast"
	RuleTapTarget          = "a11y.tap-target"
	RuleDocumentTitle      = "a11y.document-title"
	RuleConsoleError       = "runtime.console-error"
	RuleNetworkFailure     = "runtime.network-failure"
	RuleHorizontalOverflow = "layout.horizontal-overflow"
	RuleEmptyDocument      = "layout.empty-document"
	RuleDeckNoSlides       = "deck.no-slides"
	RuleDeckPageBreak      = "deck.page-break"
	RuleSystemUnlinked     = "system.unlinked"
	RuleSystemHardcoded    = "system.hardcoded"
)

// Thresholds the accessibility rules apply. They are WCAG 2.1 AA: 4.5:1 for
// body text, 3:1 for large text (18.66px bold or 24px regular), and a 24x24
// minimum target size (2.5.8 AA, not the 44x44 of AAA).
const (
	contrastMinimum      = 4.5
	contrastMinimumLarge = 3.0
	largeTextPx          = 24.0
	largeBoldTextPx      = 18.66
	boldWeight           = 700
	minTargetSize        = 24.0
)

// maxIssuesPerRule bounds how many findings one rule contributes. A page that
// breaks the same rule forty times produces forty identical lines that a reader
// skips; the first few plus a count is what actually gets acted on.
const maxIssuesPerRule = 5

// AuditInput is everything the deterministic quality pass reads. It is a plain
// struct rather than a service call so the rules can be tested against a
// hand-built render — no browser, no database, no project on disk.
type AuditInput struct {
	// Rendered reports that the browser half of the pass actually happened.
	// Every accessibility, runtime, layout and deck rule reads a render, so
	// with no render they must not run at all: firing "the document has no
	// title" because nobody loaded the document is a finding about the audit,
	// not about the artifact.
	Rendered bool
	Kind     Kind
	Viewport Viewport
	Title    string
	Slides   int
	Width    float64
	Nodes    []Node
	Facts    []NodeFacts
	Console  []ConsoleEntry
	Failures []NetworkFailure
	// Breaks is the per-slide print behaviour, filled only when the caller
	// rendered under print emulation. Empty means the deck print rule is not
	// evaluated at all, rather than evaluated and passed.
	Breaks []SlideBreak
	// SystemLinked reports whether the entry document links the design system
	// stylesheet, and SystemFindings are the hardcoded values the P6 audit
	// found. RequiresSystem turns the unlinked finding on: an artifact built
	// from a template that declares no design system is not in breach.
	SystemLinked   bool
	RequiresSystem bool
	SystemFindings []SystemFinding
}

// AuditResult is one deterministic quality pass.
type AuditResult struct {
	Score   float64 `json:"score"`
	Summary string  `json:"summary"`
	Issues  []Issue `json:"issues"`
	// Counts is how many times each rule fired, including the occurrences that
	// were folded away by maxIssuesPerRule.
	Counts map[string]int `json:"counts,omitempty"`
}

// Audit runs every deterministic rule over one render and scores the result.
// It never calls a model: this is the evidence a critic pass argues from, and
// evidence that changes between two identical runs is not evidence.
func Audit(in AuditInput) AuditResult {
	var issues []Issue
	counts := map[string]int{}
	add := func(issue Issue) {
		counts[issue.Code]++
		if counts[issue.Code] > maxIssuesPerRule {
			return
		}
		issues = append(issues, issue)
	}

	if in.Rendered {
		auditDocument(in, add)
		auditRuntime(in, add)
		auditNodes(in, add)
		auditDeck(in, add)
	}
	auditSystem(in, add)

	sortIssues(issues)
	score := scoreIssues(counts)
	return AuditResult{
		Score:   score,
		Summary: auditSummary(score, counts),
		Issues:  issues,
		Counts:  counts,
	}
}

func auditDocument(in AuditInput, add func(Issue)) {
	if strings.TrimSpace(in.Title) == "" {
		add(Issue{
			Code:     RuleDocumentTitle,
			Severity: SeverityWarning,
			Message:  "the document has no title",
			Fix:      "add a <title> that names the page; it is the first thing a screen reader and a browser tab announce",
		})
	}
	if len(in.Nodes) == 0 {
		add(Issue{
			Code:     RuleEmptyDocument,
			Severity: SeverityBlocking,
			Message:  "the render produced no elements at all",
			Fix:      "check the entry document loads and its body is not empty or hidden",
		})
		return
	}
	if in.Viewport.W > 0 && in.Width > float64(in.Viewport.W)+1 {
		add(Issue{
			Code:     RuleHorizontalOverflow,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("the document is %.0fpx wide at a %dpx viewport, so it scrolls sideways",
				in.Width, in.Viewport.W),
			Fix: "constrain the overflowing element with max-width: 100% or a wrapping layout",
		})
	}
}

func auditRuntime(in AuditInput, add func(Issue)) {
	for _, entry := range in.Console {
		level := strings.ToLower(entry.Level)
		if level != "error" && level != "exception" && level != "assert" {
			continue
		}
		add(Issue{
			Code:     RuleConsoleError,
			Severity: SeverityError,
			Message:  "console " + level + ": " + truncate(entry.Message, 200),
			Fix:      "fix the script error; a page that throws while rendering is not finished",
		})
	}
	for _, failure := range in.Failures {
		detail := failure.Error
		if detail == "" {
			detail = fmt.Sprintf("status %d", failure.Status)
		}
		target := truncate(failure.URL, 120)
		if target == "" {
			target = "a request the page made"
		}
		add(Issue{
			Code:     RuleNetworkFailure,
			Severity: SeverityError,
			Message:  "failed to load " + target + " (" + detail + ")",
			Fix:      "fix the reference or remove it; a missing asset renders differently on every machine",
		})
	}
}

func auditNodes(in AuditInput, add func(Issue)) {
	previousHeading := 0
	sawH1 := false
	sawHeading := false

	for _, f := range in.Facts {
		if f.AriaHidden {
			continue
		}
		named := strings.TrimSpace(f.Name) != ""

		if f.Tag == "img" && !named && !f.AltPresent {
			add(Issue{
				Code:     RuleImageAlt,
				Severity: SeverityError,
				NodeID:   f.NodeID,
				Slide:    f.Slide,
				Message:  "image has no alt text",
				Fix:      `describe it in alt="", or set alt="" explicitly when it is decorative`,
			})
		}
		if f.Interactive && !named {
			add(Issue{
				Code:     RuleControlName,
				Severity: SeverityError,
				NodeID:   f.NodeID,
				Slide:    f.Slide,
				Message:  "the " + f.Tag + " control has no accessible name",
				Fix:      "give it visible text, or an aria-label when the design calls for an icon only",
			})
		}
		if f.Interactive && f.Box.W > 0 && f.Box.H > 0 &&
			(f.Box.W < minTargetSize || f.Box.H < minTargetSize) {
			add(Issue{
				Code:     RuleTapTarget,
				Severity: SeverityWarning,
				NodeID:   f.NodeID,
				Slide:    f.Slide,
				Message: fmt.Sprintf("target is %.0fx%.0fpx, below the %.0fx%.0fpx minimum",
					f.Box.W, f.Box.H, minTargetSize, minTargetSize),
				Fix: "grow the control or its padding until it is at least 24x24 CSS pixels",
			})
		}
		if f.HeadingLevel > 0 {
			sawHeading = true
			if f.HeadingLevel == 1 {
				sawH1 = true
			}
			if previousHeading > 0 && f.HeadingLevel > previousHeading+1 {
				add(Issue{
					Code:     RuleHeadingOrder,
					Severity: SeverityWarning,
					NodeID:   f.NodeID,
					Slide:    f.Slide,
					Message: fmt.Sprintf("heading jumps from h%d to h%d",
						previousHeading, f.HeadingLevel),
					Fix: "use the next level down, or restyle the heading instead of skipping a level",
				})
			}
			previousHeading = f.HeadingLevel
		}

		if issue, ok := contrastIssue(f); ok {
			add(issue)
		}
	}

	if sawHeading && !sawH1 {
		add(Issue{
			Code:     RuleMissingH1,
			Severity: SeverityWarning,
			Message:  "the document has headings but no h1",
			Fix:      "promote the page's own title to h1 so the outline has a root",
		})
	}
}

// contrastIssue evaluates one text node against WCAG AA. It returns false
// whenever the colours cannot be parsed: an unreadable value is unknown, and
// reporting unknown as a failure would train the reader to ignore the rule.
func contrastIssue(f NodeFacts) (Issue, bool) {
	if !f.HasText || strings.TrimSpace(f.Name) == "" {
		return Issue{}, false
	}
	foreground, ok := ParseCSSColor(f.Color)
	if !ok {
		return Issue{}, false
	}
	background, ok := ParseCSSColor(f.Background)
	if !ok {
		return Issue{}, false
	}
	ratio := ContrastRatio(foreground, background)
	minimum := contrastMinimum
	if f.FontSize >= largeTextPx || (f.FontSize >= largeBoldTextPx && f.FontWeight >= boldWeight) {
		minimum = contrastMinimumLarge
	}
	if ratio >= minimum {
		return Issue{}, false
	}
	return Issue{
		Code:     RuleContrast,
		Severity: SeverityError,
		NodeID:   f.NodeID,
		Slide:    f.Slide,
		Message: fmt.Sprintf("text contrast is %.2f:1 against its background, below the %.1f:1 minimum (%q)",
			ratio, minimum, truncate(f.Name, 40)),
		Fix: "darken the text or lighten the surface — pick the pair from the design system rather than by eye",
	}, true
}

func auditDeck(in AuditInput, add func(Issue)) {
	if in.Kind != KindDeck {
		return
	}
	if in.Slides == 0 {
		add(Issue{
			Code:     RuleDeckNoSlides,
			Severity: SeverityBlocking,
			Message:  "the deck has no slides the renderer can find",
			Fix:      "wrap each slide in <section class=\"slide\"> or mark it with data-slide",
		})
		return
	}
	for i, brk := range in.Breaks {
		// The last slide needs no break after it: nothing follows it to push
		// onto a new page.
		if i == len(in.Breaks)-1 {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(brk.BreakAfter))
		if value == "page" || value == "always" || value == "left" || value == "right" {
			continue
		}
		add(Issue{
			Code:     RuleDeckPageBreak,
			Severity: SeverityError,
			Slide:    brk.Index,
			Message:  fmt.Sprintf("slide %d is not followed by a page break, so the PDF will run slides together", brk.Index+1),
			Fix:      "add break-after: page to the slide in a @media print block",
		})
	}
}

func auditSystem(in AuditInput, add func(Issue)) {
	if in.RequiresSystem && !in.SystemLinked {
		add(Issue{
			Code:     RuleSystemUnlinked,
			Severity: SeverityError,
			Message:  "the artifact does not link the design system stylesheet",
			Fix:      "run design_system apply, or add the <link> to the entry document by hand",
		})
	}
	for _, finding := range in.SystemFindings {
		location := finding.File
		if finding.Line > 0 {
			location = fmt.Sprintf("%s:%d", finding.File, finding.Line)
		}
		property := finding.Property
		if property != "" {
			property = " " + property + ":"
		}
		add(Issue{
			Code:     RuleSystemHardcoded,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s hardcodes%s %s", location, property, finding.Value),
			Fix:      "use var(" + finding.Token + ") instead",
		})
	}
}

// severityWeight is what one occurrence of a rule costs against a perfect ten.
func severityWeight(severity string) float64 {
	switch severity {
	case SeverityBlocking:
		return 4.0
	case SeverityError:
		return 1.5
	case SeverityWarning:
		return 0.5
	default:
		return 0
	}
}

// maxRuleDeduction caps what any single rule can cost. Without it a page with
// thirty low-contrast labels scores zero and a page that fails to render at all
// also scores zero, and the score stops telling the two apart.
const maxRuleDeduction = 3.0

// ruleSeverity is the severity a rule reports at. Scoring works from counts,
// not from the trimmed issue list, so folded-away occurrences still cost.
func ruleSeverity(code string) string {
	switch code {
	case RuleEmptyDocument, RuleDeckNoSlides:
		return SeverityBlocking
	case RuleImageAlt, RuleControlName, RuleContrast, RuleConsoleError,
		RuleNetworkFailure, RuleSystemUnlinked, RuleDeckPageBreak:
		return SeverityError
	default:
		return SeverityWarning
	}
}

func scoreIssues(counts map[string]int) float64 {
	score := 10.0
	for code, count := range counts {
		weight := severityWeight(ruleSeverity(code))
		deduction := weight * float64(count)
		// The cap bounds repetition, never a single occurrence: a rule that
		// costs more than the cap on its own is meant to.
		limit := math.Max(maxRuleDeduction, weight)
		if deduction > limit {
			deduction = limit
		}
		score -= deduction
	}
	if score < 0 {
		score = 0
	}
	return math.Round(score*10) / 10
}

func auditSummary(score float64, counts map[string]int) string {
	total := 0
	worst := SeverityInfo
	for code, count := range counts {
		total += count
		if severityWeight(ruleSeverity(code)) > severityWeight(worst) {
			worst = ruleSeverity(code)
		}
	}
	if total == 0 {
		return "no automated issues: every check that ran passes"
	}
	return fmt.Sprintf("%d automated issue(s) across %d rule(s), worst severity %s, score %.1f/10",
		total, len(counts), worst, score)
}

// sortIssues orders findings the way a reader works through them: worst first,
// then by rule so repeated findings stay together, then by node.
func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		if wa, wb := severityWeight(a.Severity), severityWeight(b.Severity); wa != wb {
			return wa > wb
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.NodeID < b.NodeID
	})
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// RGB is an 8-bit colour with an alpha channel, as a CSS colour value carries.
type RGB struct {
	R, G, B float64
	A       float64
}

// ParseCSSColor reads the colour notations a computed style actually produces —
// rgb(), rgba(), and the hex forms an inline style may still carry. It returns
// false for anything else (named colours, colour functions), because a rule
// that guesses at a colour reports contrast failures that are not real.
func ParseCSSColor(value string) (RGB, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.HasPrefix(value, "#"):
		return parseHexColor(value)
	case strings.HasPrefix(value, "rgb"):
		return parseRGBColor(value)
	default:
		return RGB{}, false
	}
}

func parseHexColor(value string) (RGB, bool) {
	digits := strings.TrimPrefix(value, "#")
	if len(digits) == 3 || len(digits) == 4 {
		expanded := ""
		for _, c := range digits {
			expanded += string(c) + string(c)
		}
		digits = expanded
	}
	if len(digits) != 6 && len(digits) != 8 {
		return RGB{}, false
	}
	component := func(i int) (float64, bool) {
		n, err := strconv.ParseUint(digits[i:i+2], 16, 8)
		if err != nil {
			return 0, false
		}
		return float64(n), true
	}
	r, okR := component(0)
	g, okG := component(2)
	b, okB := component(4)
	if !okR || !okG || !okB {
		return RGB{}, false
	}
	alpha := 1.0
	if len(digits) == 8 {
		a, ok := component(6)
		if !ok {
			return RGB{}, false
		}
		alpha = a / 255
	}
	return RGB{R: r, G: g, B: b, A: alpha}, true
}

func parseRGBColor(value string) (RGB, bool) {
	open := strings.Index(value, "(")
	closing := strings.LastIndex(value, ")")
	if open < 0 || closing < open {
		return RGB{}, false
	}
	body := value[open+1 : closing]
	body = strings.ReplaceAll(body, "/", " ")
	body = strings.ReplaceAll(body, ",", " ")
	parts := strings.Fields(body)
	if len(parts) < 3 {
		return RGB{}, false
	}
	channel := func(s string) (float64, bool) {
		if strings.HasSuffix(s, "%") {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
			if err != nil {
				return 0, false
			}
			return n * 255 / 100, true
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	r, okR := channel(parts[0])
	g, okG := channel(parts[1])
	b, okB := channel(parts[2])
	if !okR || !okG || !okB {
		return RGB{}, false
	}
	alpha := 1.0
	if len(parts) >= 4 {
		a, ok := channel(parts[3])
		if !ok {
			return RGB{}, false
		}
		// The alpha channel is 0-1, not 0-255, unless it was a percentage —
		// which channel() has already scaled to 0-255.
		if strings.HasSuffix(parts[3], "%") {
			alpha = a / 255
		} else {
			alpha = a
		}
	}
	return RGB{R: r, G: g, B: b, A: alpha}, true
}

// relativeLuminance is the WCAG definition, not perceived brightness.
func relativeLuminance(c RGB) float64 {
	channel := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// ContrastRatio returns the WCAG contrast ratio between a foreground and the
// surface behind it. A translucent foreground is composited over the
// background first: that is what the eye sees, and what the browser paints.
func ContrastRatio(foreground, background RGB) float64 {
	if foreground.A < 1 {
		foreground = RGB{
			R: foreground.R*foreground.A + background.R*(1-foreground.A),
			G: foreground.G*foreground.A + background.G*(1-foreground.A),
			B: foreground.B*foreground.A + background.B*(1-foreground.A),
			A: 1,
		}
	}
	l1 := relativeLuminance(foreground)
	l2 := relativeLuminance(background)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}
