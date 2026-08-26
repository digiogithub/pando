package design

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTwoArtifactsShareOneSystemAndFollowItWhenItChanges is the P6 exit
// criterion made checkable without a browser.
//
// "Visually consistent" is not something a unit test can look at, so the test
// asserts the mechanism that produces it: both artifacts resolve their colours
// through the same generated stylesheet, neither carries a literal that
// contradicts a token, and replacing the system changes what both of them
// render without either artifact's own files being touched.
func TestTwoArtifactsShareOneSystemAndFollowItWhenItChanges(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)

	// A project starts from a system extracted from a written style guide.
	extracted, err := svc.ExtractSystem(ctx, ExtractOptions{Source: SourceText, Target: "claude"})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, _, err := svc.SaveSystem(extracted.System); err != nil {
		t.Fatalf("save system: %v", err)
	}

	page, err := svc.Create(ctx, CreateParams{Title: "Landing", Kind: KindWeb})
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	deck, err := svc.Create(ctx, CreateParams{Title: "Quarterly Review", Kind: KindDeck})
	if err != nil {
		t.Fatalf("create deck: %v", err)
	}

	// Both artifacts are styled through the system, the way the constraint
	// block tells the designer to style them.
	for _, artifact := range []Artifact{page, deck} {
		absDir, err := svc.AbsDir(artifact)
		if err != nil {
			t.Fatalf("abs dir: %v", err)
		}
		writeFile(t, filepath.Join(absDir, "style.css"), strings.Join([]string{
			"body { background: var(--color-bg); color: var(--color-text); }",
			".cta { background: var(--color-accent); border-radius: var(--radius-md); }",
		}, "\n"))
	}

	var hrefs []string
	for _, artifact := range []Artifact{page, deck} {
		result, err := svc.ApplySystem(ctx, artifact.ID)
		if err != nil {
			t.Fatalf("apply to %s: %v", artifact.Slug, err)
		}
		if !result.Linked {
			t.Errorf("%s was not linked to the design system", artifact.Slug)
		}
		if len(result.Findings) != 0 {
			t.Errorf("%s carries values a token already covers: %+v", artifact.Slug, result.Findings)
		}
		hrefs = append(hrefs, result.Stylesheet)
	}

	// Different depths would mean different hrefs; both artifacts sit one level
	// under the output root, so the same relative path must reach the system.
	if hrefs[0] != hrefs[1] {
		t.Errorf("artifacts link different stylesheets: %q and %q", hrefs[0], hrefs[1])
	}
	stylesheet := filepath.Join(svc.Layout().SystemPath(), SystemStylesheet)
	before := readFile(t, stylesheet)

	// Record the artifact files so the swap can be shown not to touch them.
	pageDir, _ := svc.AbsDir(page)
	deckDir, _ := svc.AbsDir(deck)
	beforeFiles := map[string]string{
		"page": snapshotDir(t, pageDir),
		"deck": snapshotDir(t, deckDir),
	}

	// The project switches to a different system.
	replacement, err := svc.ExtractSystem(ctx, ExtractOptions{Source: SourceText, Target: "starbucks"})
	if err != nil {
		t.Fatalf("extract replacement: %v", err)
	}
	if _, _, err := svc.SaveSystem(replacement.System); err != nil {
		t.Fatalf("save replacement: %v", err)
	}

	after := readFile(t, stylesheet)
	if after == before {
		t.Fatal("replacing the design system did not change the generated stylesheet")
	}
	if snapshotDir(t, pageDir) != beforeFiles["page"] || snapshotDir(t, deckDir) != beforeFiles["deck"] {
		t.Error("replacing the design system rewrote artifact files; it must only change the shared stylesheet")
	}

	// And both artifacts still resolve through it, unchanged and still clean.
	for _, artifact := range []Artifact{page, deck} {
		result, err := svc.ApplySystem(ctx, artifact.ID)
		if err != nil {
			t.Fatalf("re-apply to %s: %v", artifact.Slug, err)
		}
		if result.Linked {
			t.Errorf("%s was re-linked; the link should already have been there", artifact.Slug)
		}
		if result.System != replacement.System.Name {
			t.Errorf("%s reports system %q, want %q", artifact.Slug, result.System, replacement.System.Name)
		}
	}
}

// snapshotDir renders a directory's files and contents as one comparable
// string, so a test can assert that nothing under it moved.
func snapshotDir(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		b.WriteString(rel + "\n" + string(raw) + "\n---\n")
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return b.String()
}
