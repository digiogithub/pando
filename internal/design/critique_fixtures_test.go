package design

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"testing"
)

//go:embed fixtures/critique
var critiqueFixtures embed.FS

// fixtureCase is one regression brief: a first cut with known defects and the
// version that fixes them. The pair is the measurement — a single artifact
// tells you what the audit found, a pair tells you whether iterating helped.
type fixtureCase struct {
	name string
	kind Kind
	// wantV1 are rules the first version must break. A fixture that stops
	// breaking them is a fixture that stopped testing anything.
	wantV1 []string
	// goneInVN are rules the fixed version must not break any more.
	goneInVN []string
	// policy is the gate the brief is critiqued under. A single warning is
	// not meant to fail a standard gate, so a fixture whose defect is a
	// warning states the strict policy it actually needs instead of the audit
	// being bent to make it fail.
	policy string
}

var critiqueCases = []fixtureCase{
	{
		name: "landing",
		kind: KindWeb,
		wantV1: []string{
			RuleDocumentTitle, RuleImageAlt, RuleControlName,
			RuleContrast, RuleHeadingOrder, RuleMissingH1,
			RuleTapTarget, RuleHorizontalOverflow,
		},
		goneInVN: []string{
			RuleDocumentTitle, RuleImageAlt, RuleControlName,
			RuleContrast, RuleHeadingOrder, RuleMissingH1,
			RuleTapTarget, RuleHorizontalOverflow,
		},
		policy: PolicyStandard,
	},
	{
		name:     "deck",
		kind:     KindDeck,
		policy:   PolicyStandard,
		wantV1:   []string{RuleDeckPageBreak},
		goneInVN: []string{RuleDeckPageBreak},
	},
	{
		name:     "runtime",
		kind:     KindWeb,
		policy:   PolicyStrict,
		wantV1:   []string{RuleConsoleError},
		goneInVN: []string{RuleConsoleError},
	},
}

func fixtureHTML(t *testing.T, name, version string) string {
	t.Helper()
	raw, err := critiqueFixtures.ReadFile("fixtures/critique/" + name + "/" + version + ".html")
	if err != nil {
		t.Fatalf("read fixture %s/%s: %v", name, version, err)
	}
	return string(raw)
}

// TestCritiqueFixturesImproveFromV1ToVN is the P8 exit criterion: over a set of
// briefs, the second version must score measurably better than the first, and
// the specific rules the first version broke must be gone rather than traded
// for different ones.
func TestCritiqueFixturesImproveFromV1ToVN(t *testing.T) {
	svc, _ := newTestService(t)
	renderer := newTestRenderer(t, svc)
	svc.WithRenderer(renderer)
	ctx := context.Background()

	for _, tc := range critiqueCases {
		t.Run(tc.name, func(t *testing.T) {
			artifact, err := svc.Create(ctx, CreateParams{
				Title: tc.name,
				Kind:  tc.kind,
				Files: map[string]string{"index.html": fixtureHTML(t, tc.name, "v1")},
			})
			if err != nil {
				t.Fatalf("create: %v", err)
			}

			first, err := svc.Critique(ctx, artifact.ID, CritiqueOptions{Record: true, Policy: tc.policy})
			if err != nil {
				t.Fatalf("critique v1: %v", err)
			}
			if !first.Rendered {
				t.Fatalf("v1 was not rendered: %s", first.RenderError)
			}
			for _, code := range tc.wantV1 {
				if first.Audit.Counts[code] == 0 {
					t.Errorf("v1 did not break %s, so the fixture no longer tests it; fired: %v",
						code, first.Audit.Counts)
				}
			}
			if first.Decision.Pass {
				t.Errorf("the gate passed a version with %d known defects: %+v",
					len(tc.wantV1), first.Decision)
			}
			if !first.Decision.Iterate {
				t.Errorf("round 1 of a failing artifact did not ask for another round: %+v", first.Decision)
			}

			// Apply the fix and commit it as the next version, which is what a
			// designer round actually does.
			absDir, err := svc.AbsDir(artifact)
			if err != nil {
				t.Fatalf("abs dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(absDir, "index.html"),
				[]byte(fixtureHTML(t, tc.name, "vn")), 0o644); err != nil {
				t.Fatalf("write vn: %v", err)
			}
			if _, err := svc.CommitVersion(ctx, artifact.ID, "fix the findings"); err != nil {
				t.Fatalf("commit: %v", err)
			}

			second, err := svc.Critique(ctx, artifact.ID, CritiqueOptions{Record: true, Policy: tc.policy})
			if err != nil {
				t.Fatalf("critique vN: %v", err)
			}
			if second.Audit.Score <= first.Audit.Score {
				t.Errorf("score did not improve: v1 %.1f, vN %.1f (v1 %v, vN %v)",
					first.Audit.Score, second.Audit.Score, first.Audit.Counts, second.Audit.Counts)
			}
			for _, code := range tc.goneInVN {
				if second.Audit.Counts[code] != 0 {
					t.Errorf("vN still breaks %s: %v", code, second.Audit.Counts)
				}
			}
			if !second.Decision.Pass {
				t.Errorf("the fixed version still does not pass its own gate: %+v (%v)",
					second.Decision, second.Audit.Counts)
			}
			if second.Version != 2 {
				t.Errorf("vN critique recorded against version %d", second.Version)
			}

			// The history is what a UI reads back; a pass that scores and
			// forgets is not a pass anyone can act on later.
			stored, err := svc.LatestCritique(ctx, artifact.ID, 1)
			if err != nil {
				t.Fatalf("latest critique for v1: %v", err)
			}
			if stored.Score != first.Critique.Score {
				t.Errorf("stored v1 score %.1f, reported %.1f", stored.Score, first.Critique.Score)
			}
		})
	}
}

// A render is deterministic enough for a perceptual-diff regression check to be
// worth having: the same artifact twice must not drift, and a real change must
// register.
func TestRenderIsStableEnoughForPerceptualDiff(t *testing.T) {
	svc, _ := newTestService(t)
	renderer := newTestRenderer(t, svc)
	svc.WithRenderer(renderer)
	ctx := context.Background()

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "diff",
		Kind:  KindWeb,
		Files: map[string]string{"index.html": fixtureHTML(t, "landing", "vn")},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	shot := func() []byte {
		t.Helper()
		png, err := renderer.Screenshot(ctx, artifact, ScreenshotOptions{
			RenderOptions: RenderOptions{Viewport: Viewport{W: 800, H: 600}},
			Slide:         -1,
		})
		if err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		return png
	}

	golden := shot()
	again := shot()
	diff, err := ComparePNG(golden, again, 0)
	if err != nil {
		t.Fatalf("ComparePNG: %v", err)
	}
	if diff.Fraction > 0.001 {
		t.Errorf("two renders of the same artifact differ by %.4f, so a golden check would be noise: %+v",
			diff.Fraction, diff)
	}

	absDir, err := svc.AbsDir(artifact)
	if err != nil {
		t.Fatalf("abs dir: %v", err)
	}
	changed := fixtureHTML(t, "landing", "vn")
	changed = replaceOnce(changed, "background: #eeeeee", "background: #102080")
	if err := os.WriteFile(filepath.Join(absDir, "index.html"), []byte(changed), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	after, err := ComparePNG(golden, shot(), 0)
	if err != nil {
		t.Fatalf("ComparePNG: %v", err)
	}
	if after.Changed == 0 {
		t.Errorf("a repainted band produced no pixel difference: %+v", after)
	}
}

func replaceOnce(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
