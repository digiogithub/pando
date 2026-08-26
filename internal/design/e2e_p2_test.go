package design

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The end-to-end loop P2 exists to make possible: create, render, find a node
// in the index, patch the source through that node id, re-render and confirm
// the change reached the live DOM. This is the one test that proves the node
// index and the patch engine agree about what an element is.
func TestEndToEndRenderInspectPatchRender(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	svc = svc.WithRenderer(newTestRenderer(t, svc))

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Landing",
		Kind:  KindWeb,
		Files: map[string]string{"index.html": webFixture},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.Render(ctx, artifact.ID, RenderOptions{}); err != nil {
		t.Fatalf("render: %v", err)
	}

	found, err := svc.Inspect(ctx, artifact.ID, 0, InspectOptions{Text: "Ship design faster"})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(found.Nodes) == 0 {
		t.Fatal("the heading is not in the index")
	}
	heading := found.Nodes[0]
	if heading.Box.H <= 0 {
		t.Fatalf("the index should carry a real layout box, got %+v", heading.Box)
	}

	plan, version, err := svc.Patch(ctx, artifact.ID, []PatchOp{
		{NodeID: heading.NodeID, Op: OpSetText, Value: "Diseña más rápido"},
		{NodeID: heading.NodeID, Op: OpSetStyle, Style: map[string]string{"color": "rgb(255, 0, 85)"}},
	}, "translate and recolour the hero heading", true)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected version 2, got %d", version)
	}
	if len(plan.Files) != 1 {
		t.Fatalf("the patch should have touched one file, got %d", len(plan.Files))
	}

	after, err := svc.Render(ctx, artifact.ID, RenderOptions{})
	if err != nil {
		t.Fatalf("re-render: %v", err)
	}
	var reindexed *Node
	for i := range after.Nodes {
		if strings.Contains(after.Nodes[i].Text, "Diseña más rápido") {
			reindexed = &after.Nodes[i]
			break
		}
	}
	if reindexed == nil {
		t.Fatal("the patched text did not reach the rendered DOM")
	}

	styled, err := svc.Inspect(ctx, artifact.ID, 0, InspectOptions{
		NodeID: reindexed.NodeID, IncludeStyles: true, StyleProps: []string{"color"},
	})
	if err != nil {
		t.Fatalf("inspect styles: %v", err)
	}
	if len(styled.Nodes) == 0 || styled.Nodes[0].Styles["color"] != "rgb(255, 0, 85)" {
		t.Fatalf("the inline style did not take effect: %+v", styled.Nodes)
	}
}

// A deck has to survive the whole path an agent takes it down: render, patch a
// slide, export a PDF that is really one page per slide.
func TestEndToEndDeckPatchAndExport(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	svc = svc.WithRenderer(newTestRenderer(t, svc))

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Deck",
		Kind:  KindDeck,
		Files: map[string]string{"index.html": deckFixture},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	result, err := svc.Render(ctx, artifact.ID, RenderOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if result.Slides < 2 {
		t.Fatalf("expected the deck slides to be counted, got %d", result.Slides)
	}

	if _, _, err := svc.Patch(ctx, artifact.ID, []PatchOp{
		{Selector: "#t0", Op: OpSetText, Value: "Portada"},
	}, "retitle the first slide", false); err != nil {
		t.Fatalf("patch: %v", err)
	}

	export, err := svc.Export(ctx, artifact.ID, ExportOptions{Format: ExportPDF})
	if err != nil {
		t.Fatalf("export pdf: %v", err)
	}
	data, err := os.ReadFile(export.Path)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatalf("export is not a PDF: %q", data[:min(8, len(data))])
	}
	if export.Note != "" {
		t.Fatalf("the fixture does carry print breaks, so no warning was expected: %s", export.Note)
	}

	absDir, _ := svc.AbsDir(artifact)
	entry, _ := os.ReadFile(filepath.Join(absDir, "index.html"))
	if !strings.Contains(string(entry), ">Portada<") {
		t.Fatalf("the slide title was not patched:\n%s", entry)
	}
}

// A deck without print styles must be reported, not silently exported as a
// single long page: that failure is invisible until someone opens the PDF.
func TestDeckExportWarnsWithoutPrintBreaks(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	svc = svc.WithRenderer(newTestRenderer(t, svc))

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Plain deck",
		Kind:  KindDeck,
		Files: map[string]string{"index.html": deckWithoutPrintStyles},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	export, err := svc.Export(ctx, artifact.ID, ExportOptions{Format: ExportPDF})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(export.Note, "break-after") {
		t.Fatalf("expected a missing-print-CSS warning, got %q", export.Note)
	}
}

func TestCanvasRasterizationWritesIntoTheWorkspace(t *testing.T) {
	ctx := context.Background()
	svc, project := newTestService(t)
	renderer := newTestRenderer(t, svc)

	png, err := renderer.Rasterize(ctx, `<!doctype html><html><body style="margin:0">
<canvas id="c" width="64" height="64"></canvas>
<script>
  var g = document.getElementById('c').getContext('2d');
  g.fillStyle = '#2f6feb'; g.fillRect(0, 0, 64, 64);
</script></body></html>`, 64, 64, 0)
	if err != nil {
		t.Fatalf("rasterize: %v", err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatal("rasterize did not return a PNG")
	}
	rel, err := svc.WriteWorkspaceFile("designer/landing/assets/bg.png", png)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("generated image not on disk: %v", err)
	}
}
