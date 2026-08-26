package design

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/design/preview"
	"github.com/digiogithub/pando/internal/pubsub"
)

// withPreviewServer installs a loopback preview server for one test and takes
// it down afterwards, so the process-wide pointer never leaks between tests.
func withPreviewServer(t *testing.T) *preview.Server {
	t.Helper()
	server := preview.New(PreviewOptions(nil, nil))
	if err := server.StartLoopback(); err != nil {
		t.Fatalf("start preview: %v", err)
	}
	previous := PreviewServer()
	SetPreviewServer(server)
	t.Cleanup(func() {
		server.Close()
		previewServer.Store(previous)
	})
	return server
}

func TestPresentationPrefersTheServedPreviewAndKeepsTheFileURL(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)
	server := withPreviewServer(t)

	p, err := svc.Presentation(ctx, artifact.ID, 0, "")
	if err != nil {
		t.Fatalf("presentation: %v", err)
	}
	if !strings.HasPrefix(p.URL, "http://"+server.Addr()+preview.Prefix) {
		t.Fatalf("URL should be the served preview, got %q", p.URL)
	}
	if !strings.HasPrefix(p.FileURL, "file://") {
		t.Fatalf("the file address must stay available, got %q", p.FileURL)
	}
	if !strings.Contains(p.BridgeURL, "bridge=1") {
		t.Fatalf("the UI needs a bridged address, got %q", p.BridgeURL)
	}

	// The served document must actually answer at that address.
	resp, err := http.Get(p.URL)
	if err != nil {
		t.Fatalf("get preview: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview answered %d", resp.StatusCode)
	}
}

func TestPresentationFallsBackToFileWithoutAPreviewServer(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)

	p, err := svc.Presentation(ctx, artifact.ID, 0, "")
	if err != nil {
		t.Fatalf("presentation: %v", err)
	}
	if !strings.HasPrefix(p.URL, "file://") {
		t.Fatalf("without a server the address stays file://, got %q", p.URL)
	}
	if p.BridgeURL != "" {
		t.Fatalf("there is no bridge without a server, got %q", p.BridgeURL)
	}
}

func TestPreviewStampScriptMatchesTheRenderersOwnWalker(t *testing.T) {
	script := string(previewStampScript())
	if script == "" {
		t.Fatal("the stamping preamble must exist: without it a preview has no data-pando-id to select")
	}
	// The point of the preamble is that it *is* the renderer's walker, not a
	// second implementation that could number elements differently.
	for _, marker := range []string{"data-pando-id", "selectorFor", "data-pando-slide", DefaultSlideSelector} {
		if !strings.Contains(script, marker) {
			t.Fatalf("the preamble is missing %q", marker)
		}
	}
	if !strings.Contains(script, "DOMContentLoaded") {
		t.Fatal("the walker measures boxes, so it must wait for the document to parse")
	}
}

func TestLifecycleEventsArePublished(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := Events().Subscribe(ctx)

	svc, _ := newTestService(t)
	artifact, err := svc.Create(ctx, CreateParams{Title: "Event Landing", Kind: KindWeb})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CommitVersion(ctx, artifact.ID, "second pass"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Creating an artifact commits version 1 before the artifact record is
	// complete, so the initial version event legitimately precedes the created
	// one. Asserting the exact sequence keeps that ordering from drifting
	// silently: a timeline built from this stream depends on it.
	got := drainEvents(t, events, 3)
	if got[0].Kind != EventVersion || got[0].Version != 1 || got[0].Summary != "initial version" {
		t.Fatalf("first event wrong: %+v", got[0])
	}
	if got[1].Kind != EventCreated || got[1].ArtifactID != artifact.ID {
		t.Fatalf("second event wrong: %+v", got[1])
	}
	if got[1].ArtifactKind != KindWeb || got[1].Version != 1 || got[1].Slug != "event-landing" {
		t.Fatalf("the created event must describe the artifact: %+v", got[1])
	}
	if got[2].Kind != EventVersion || got[2].Version != 2 || got[2].Summary != "second pass" {
		t.Fatalf("third event wrong: %+v", got[2])
	}
	if got[2].At.IsZero() {
		t.Fatal("events must be timestamped")
	}
}

// drainEvents collects n events or fails the test.
func drainEvents(t *testing.T, ch <-chan pubsub.Event[Event], n int) []Event {
	t.Helper()
	out := make([]Event, 0, n)
	for len(out) < n {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatalf("event stream closed after %d events", len(out))
			}
			out = append(out, event.Payload)
		default:
			t.Fatalf("expected %d events, saw %d", n, len(out))
		}
	}
	return out
}
