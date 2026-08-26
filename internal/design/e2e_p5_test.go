package design

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// P5 gives the Design Studio its non-browser surfaces: the `pando design` CLI,
// the TUI page, and the ACP slash commands. None of them ships its own lookup
// or its own idea of where a preview lives — they all go through Resolve and
// LiveURL, which is what makes "the same artifact from every surface" true
// rather than three implementations that agree today.
//
// This test exercises that shared floor: one artifact, found the way each
// surface's user would type it, then served over HTTP at the URL every surface
// hands out.
func TestOneArtifactReachableFromEverySurface(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	t.Cleanup(ClosePreviewServer)

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Quarterly Review",
		Kind:  KindDeck,
		Files: map[string]string{"index.html": deckFixture},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The four ways a person refers to an artifact, one per surface habit: the
	// id a script pasted, the slug a shell user tab-completed, the id prefix a
	// TUI showed, and the words an ACP user remembered from the title.
	for _, ref := range []string{artifact.ID, "quarterly-review", artifact.ID[:10], "Quarterly"} {
		found, err := svc.Resolve(ctx, ref)
		if err != nil {
			t.Fatalf("resolve %q: %v", ref, err)
		}
		if found.ID != artifact.ID {
			t.Fatalf("resolve %q returned %s, want %s", ref, found.ID, artifact.ID)
		}
	}

	// An empty reference is "the one I am working on", which every surface uses
	// as the default for a bare /design-open or `pando design open`.
	if found, err := svc.Resolve(ctx, ""); err != nil || found.ID != artifact.ID {
		t.Fatalf("empty ref resolved to %+v (%v)", found, err)
	}

	presentation, err := svc.LiveURL(ctx, artifact.ID, 2)
	if err != nil {
		t.Fatalf("live url: %v", err)
	}
	if !strings.HasPrefix(presentation.URL, "http://127.0.0.1:") {
		t.Fatalf("LiveURL should have started a loopback preview, got %q", presentation.URL)
	}
	if presentation.Slide != 2 || !strings.HasSuffix(presentation.URL, "#slide-2") {
		t.Fatalf("the slide deep link did not survive: %q", presentation.URL)
	}

	// The bridged URL is the WebUI's; a plain browser (CLI `open`, TUI `o`, an
	// ACP resource_link) gets the unbridged one. Both must be servable.
	for _, target := range []string{presentation.URL, presentation.BridgeURL} {
		if target == "" {
			t.Fatal("both a plain and a bridged preview URL are expected once a server is running")
		}
		body, status := getPreview(t, target)
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d", target, status)
		}
		if !strings.Contains(body, "<html") {
			t.Fatalf("GET %s did not return the deck document:\n%s", target, body)
		}
	}
}

// A reference that matches two artifacts must say so instead of silently
// picking one: every surface here acts on the result (opens a browser, exports
// a file), so guessing is the expensive failure.
func TestAmbiguousReferenceIsReported(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)

	for _, title := range []string{"Pricing Page A", "Pricing Page B"} {
		if _, err := svc.Create(ctx, CreateParams{Title: title, Kind: KindWeb}); err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
	}
	_, err := svc.Resolve(ctx, "pricing")
	var ambiguous *ErrAmbiguousRef
	if !errors.As(err, &ambiguous) {
		t.Fatalf("expected an ambiguity error, got %v", err)
	}
	if len(ambiguous.Artifacts) != 2 {
		t.Fatalf("expected both candidates, got %d", len(ambiguous.Artifacts))
	}
	if !strings.Contains(ambiguous.Error(), "pricing-page-a") {
		t.Fatalf("the error should name the candidates: %s", ambiguous.Error())
	}
}

func TestResolveRejectsUnknownReference(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	if _, err := svc.Create(ctx, CreateParams{Title: "Landing", Kind: KindWeb}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Resolve(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func getPreview(t *testing.T, url string) (string, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), resp.StatusCode
}
