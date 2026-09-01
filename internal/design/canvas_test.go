package design

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// withDesignConfig points the process config at a temp project for one test.
func withDesignConfig(t *testing.T, project string) func() {
	t.Helper()
	previous := config.Get()
	cfg := &config.Config{WorkingDir: project}
	cfg.Design.OutputDir = "designer"
	cfg.Design.SystemDir = "_system"
	config.SetForTests(cfg)
	return func() { config.SetForTests(previous) }
}

// The canvas has to describe an artifact well enough for a frame to load it and
// for a label to say what it is, without the page ever asking a second question.
func TestArtboardsDescribeEveryArtifact(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)
	server := withPreviewServer(t)

	boards, err := svc.Artboards(ctx, server, "")
	if err != nil {
		t.Fatalf("Artboards: %v", err)
	}
	if len(boards) != 1 {
		t.Fatalf("got %d artboards, want 1", len(boards))
	}
	board := boards[0]
	if board.ID != artifact.ID || board.Title != artifact.Title || board.Slug != artifact.Slug {
		t.Fatalf("artboard does not identify its artifact: %+v", board)
	}
	if board.Status != "ready" {
		t.Fatalf("status = %q, want ready (%s)", board.Status, board.Note)
	}
	if !strings.Contains(board.URL, server.Addr()) {
		t.Fatalf("artboard URL is not served by the preview server: %q", board.URL)
	}
	if board.Width <= 0 || board.Height <= 0 {
		t.Fatalf("artboard has no viewport to frame it at: %+v", board)
	}

	// The URL on the artboard is what an iframe loads, so it has to answer.
	response, err := http.Get(board.URL)
	if err != nil {
		t.Fatalf("GET %s: %v", board.URL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the artboard document answered %d", response.StatusCode)
	}
}

// The canvas has to move while the agent edits, not only when it renders: the
// revision an artboard reports must change on any write inside the artifact.
func TestArtboardRevisionMovesOnAnyWrite(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)
	server := withPreviewServer(t)

	before, err := svc.Artboards(ctx, server, "")
	if err != nil {
		t.Fatalf("Artboards: %v", err)
	}
	dir, err := svc.Layout().AbsDir(artifact.Dir)
	if err != nil {
		t.Fatalf("AbsDir: %v", err)
	}
	// The revision is second-resolution, so age the write past the read.
	future := time.Now().Add(2 * time.Second)
	stylesheet := filepath.Join(dir, "styles.css")
	if err := os.WriteFile(stylesheet, []byte("body { margin: 1px }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(stylesheet, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	after, err := svc.Artboards(ctx, server, "")
	if err != nil {
		t.Fatalf("Artboards: %v", err)
	}
	if after[0].Revision <= before[0].Revision {
		t.Fatalf("revision did not move on a write: %d then %d", before[0].Revision, after[0].Revision)
	}
}

// A directory whose entry document is gone must be reported on its own artboard,
// not blank the whole canvas: the user is usually looking at four other things
// that are fine.
func TestArtboardReportsAMissingEntryWithoutFailingTheCanvas(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)
	server := withPreviewServer(t)

	dir, err := svc.Layout().AbsDir(artifact.Dir)
	if err != nil {
		t.Fatalf("AbsDir: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "index.html")); err != nil {
		t.Fatalf("remove entry: %v", err)
	}

	boards, err := svc.Artboards(ctx, server, "")
	if err != nil {
		t.Fatalf("Artboards: %v", err)
	}
	if len(boards) != 1 {
		t.Fatalf("got %d artboards, want 1", len(boards))
	}
	if boards[0].Status != "error" || boards[0].Note == "" {
		t.Fatalf("a missing entry must be reported on the artboard: %+v", boards[0])
	}
	if boards[0].URL != "" {
		t.Fatal("an artboard with no document must not hand the page a URL to frame")
	}
}

// A render in flight is what the "building" badge exists for. It has to clear
// when the render finishes, including when renders nest.
func TestRenderingBadgeTracksNestedRenders(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)
	server := withPreviewServer(t)

	outer := markRendering(artifact.ID)
	inner := markRendering(artifact.ID)

	boards, err := svc.Artboards(ctx, server, "")
	if err != nil {
		t.Fatalf("Artboards: %v", err)
	}
	if boards[0].Status != "building" {
		t.Fatalf("status = %q while a render is running", boards[0].Status)
	}

	inner()
	boards, _ = svc.Artboards(ctx, server, "")
	if boards[0].Status != "building" {
		t.Fatal("the inner render finishing must not clear the outer one")
	}

	outer()
	boards, _ = svc.Artboards(ctx, server, "")
	if boards[0].Status != "ready" {
		t.Fatalf("status = %q after every render finished", boards[0].Status)
	}
	if isRendering(artifact.ID) {
		t.Fatal("the rendering set leaked an entry")
	}
}

// Publishing the canvas twice must hand back the window that is already open.
func TestCanvasPresentationIsStable(t *testing.T) {
	server := withPreviewServer(t)

	first, err := CanvasPresentation("session-1")
	if err != nil {
		t.Fatalf("CanvasPresentation: %v", err)
	}
	second, err := CanvasPresentation("session-1")
	if err != nil {
		t.Fatalf("CanvasPresentation again: %v", err)
	}
	if first != second {
		t.Fatalf("the canvas URL changed between calls: %q then %q", first, second)
	}
	if !strings.Contains(first, server.Addr()) {
		t.Fatalf("the canvas is not served by this process: %q", first)
	}
}

// The brief is not free, so it must stay out of the prompt of a project that
// has never designed anything.
func TestPromptBriefOnlyAppearsOnceTheProjectDesigns(t *testing.T) {
	project := t.TempDir()
	restore := withDesignConfig(t, project)
	defer restore()

	if got := PromptBrief(); got != "" {
		t.Fatalf("a project with no designer directory pays for the brief: %q", got)
	}
	if err := os.MkdirAll(filepath.Join(project, "designer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := PromptBrief(); !strings.Contains(got, "<design_brief>") {
		t.Fatalf("the brief is missing once the project designs: %q", got)
	}
	if !strings.Contains(Brief(), "<design_brief>") {
		t.Fatal("Brief() must return the same standing brief unconditionally")
	}
}
