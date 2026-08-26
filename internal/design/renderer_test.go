package design

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestRenderer builds a renderer over the service layout, skipping the test
// when the machine has no Chromium-family browser: rendering is the one part of
// the design package that cannot be exercised without one.
func newTestRenderer(t *testing.T, svc *Service) *Renderer {
	t.Helper()
	r := NewRenderer(svc.Layout(), BrowserOptions{Headless: true})
	if !r.Available() {
		t.Skip("no Chromium-family browser available")
	}
	t.Cleanup(r.Close)
	return r
}

const webFixture = `<!doctype html>
<html><head><title>Fixture</title><style>
  body { margin: 0; }
  #hero { height: 400px; }
  #hero-cta { display: block; width: 200px; height: 44px; font-size: 16px; }
</style></head>
<body>
  <section id="hero"><h1>Ship design faster</h1><button id="hero-cta">Get started</button></section>
  <footer id="footer">footer text</footer>
  <script>console.error('boom');</script>
</body></html>`

const deckFixture = `<!doctype html>
<html><head><title>Deck</title><style>
  body { margin: 0; }
  .slide { width: 1280px; height: 720px; }
  @page { size: 1280px 720px; margin: 0; }
  @media print { .slide { break-after: page; } .slide:last-child { break-after: auto; } }
</style></head>
<body>
  <section class="slide" data-slide="0"><h1 id="t0">Title slide</h1></section>
  <section class="slide" data-slide="1"><h2 id="t1">Second slide</h2></section>
  <section class="slide" data-slide="2"><h2 id="t2">Third slide</h2></section>
</body></html>`

// deckWithoutPrintStyles is the failure the plan flags as a deck risk: it looks
// right on screen and collapses into one PDF page.
const deckWithoutPrintStyles = `<!doctype html>
<html><head><title>Deck</title><style>.slide { height: 720px; }</style></head>
<body>
  <section class="slide"><h1>One</h1></section>
  <section class="slide"><h1>Two</h1></section>
</body></html>`

func newRenderedArtifact(t *testing.T, kind Kind, entry string) (*Service, Artifact, *Renderer) {
	t.Helper()
	svc, _ := newTestService(t)
	artifact, err := svc.Create(context.Background(), CreateParams{
		Title: "Fixture",
		Kind:  kind,
		Files: map[string]string{"index.html": entry},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	renderer := newTestRenderer(t, svc)
	svc.WithRenderer(renderer)
	return svc, artifact, renderer
}

func TestRenderIndexesNodesAndCapturesConsole(t *testing.T) {
	svc, artifact, _ := newRenderedArtifact(t, KindWeb, webFixture)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := svc.Render(ctx, artifact.ID, RenderOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if result.Title != "Fixture" {
		t.Fatalf("title = %q", result.Title)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("render produced an empty node index")
	}

	var cta *Node
	for i := range result.Nodes {
		if result.Nodes[i].Selector == "#hero-cta" {
			cta = &result.Nodes[i]
		}
	}
	if cta == nil {
		t.Fatalf("#hero-cta missing from the index")
	}
	if cta.Text != "Get started" {
		t.Fatalf("cta text = %q", cta.Text)
	}
	if cta.Box.W < 100 || cta.Box.H < 20 {
		t.Fatalf("cta box looks wrong: %+v", cta.Box)
	}
	if cta.Styles["font-size"] != "16px" {
		t.Fatalf("cta styles = %+v", cta.Styles)
	}
	if cta.ParentID == "" {
		t.Fatal("cta has no parent in the index")
	}

	var sawConsoleError bool
	for _, entry := range result.Console {
		if entry.Message == "boom" {
			sawConsoleError = true
		}
	}
	if !sawConsoleError {
		t.Fatalf("console error not captured: %+v", result.Console)
	}

	// The index is persisted against the current version, so Inspect resolves
	// without re-rendering, and the node ids the UI selects are stable.
	inspected, err := svc.Inspect(ctx, artifact.ID, 0, InspectOptions{Slide: -1, Selector: "#hero-cta"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(inspected.Nodes) != 1 || inspected.Nodes[0].NodeID != cta.NodeID {
		t.Fatalf("stored index does not match the render: %+v", inspected.Nodes)
	}

	node, err := svc.Node(ctx, artifact.ID, 0, cta.NodeID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.Selector != "#hero-cta" {
		t.Fatalf("resolved node = %+v", node)
	}
}

func TestRenderDoesNotRewriteArtifactFiles(t *testing.T) {
	svc, artifact, _ := newRenderedArtifact(t, KindWeb, webFixture)
	entry := filepath.Join(svc.Layout().Root(), artifact.Slug, "index.html")

	before, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if _, err := svc.Render(context.Background(), artifact.ID, RenderOptions{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	after, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("render wrote data-pando-id back into the artifact file")
	}
}

func TestRenderDeckCountsAndAttributesSlides(t *testing.T) {
	svc, artifact, _ := newRenderedArtifact(t, KindDeck, deckFixture)
	ctx := context.Background()

	result, err := svc.Render(ctx, artifact.ID, RenderOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if result.Slides != 3 {
		t.Fatalf("slides = %d, want 3", result.Slides)
	}

	want := map[string]int{"#t0": 0, "#t1": 1, "#t2": 2}
	seen := map[string]int{}
	for _, n := range result.Nodes {
		if slide, ok := want[n.Selector]; ok {
			seen[n.Selector] = n.Slide
			if n.Slide != slide {
				t.Fatalf("%s attributed to slide %d, want %d", n.Selector, n.Slide, slide)
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("slide headings missing from the index: %v", seen)
	}

	// The observed slide count lands in the committed manifest.
	manifest, err := ReadManifest(filepath.Join(svc.Layout().Root(), artifact.Slug))
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if manifest.Deck == nil || manifest.Deck.Slides != 3 {
		t.Fatalf("manifest deck block = %+v", manifest.Deck)
	}

	slideOnly, err := svc.Inspect(ctx, artifact.ID, 0, InspectOptions{Slide: 2})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	for _, n := range slideOnly.Nodes {
		if n.Slide != 2 {
			t.Fatalf("slide filter leaked slide %d", n.Slide)
		}
	}
}

// TestDeckPrintStylesDetected is the print fixture the plan asks for: deck PDF
// export prints one slide per page only when the deck ships print styles, so
// the renderer must be able to tell the two cases apart.
func TestDeckPrintStylesDetected(t *testing.T) {
	_, artifact, renderer := newRenderedArtifact(t, KindDeck, deckFixture)
	ctx := context.Background()

	breaks, err := renderer.SlideBreaks(ctx, artifact, RenderOptions{})
	if err != nil {
		t.Fatalf("SlideBreaks: %v", err)
	}
	if len(breaks) != 3 {
		t.Fatalf("got %d slides, want 3", len(breaks))
	}
	for _, b := range breaks[:2] {
		if b.BreakAfter != "page" {
			t.Fatalf("slide %d break-after = %q, want page", b.Index, b.BreakAfter)
		}
	}

	pdf, err := renderer.PrintPDF(ctx, artifact, PrintOptions{})
	if err != nil {
		t.Fatalf("PrintPDF: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("print output is not a PDF (%d bytes)", len(pdf))
	}

	// The same check on a deck without print styles reports no page breaks.
	_, bareArtifact, bareRenderer := newRenderedArtifact(t, KindDeck, deckWithoutPrintStyles)
	bareBreaks, err := bareRenderer.SlideBreaks(ctx, bareArtifact, RenderOptions{})
	if err != nil {
		t.Fatalf("SlideBreaks (bare deck): %v", err)
	}
	for _, b := range bareBreaks {
		if b.BreakAfter == "page" {
			t.Fatalf("deck without print styles reported a page break on slide %d", b.Index)
		}
	}
}

func TestScreenshotVariants(t *testing.T) {
	_, artifact, renderer := newRenderedArtifact(t, KindWeb, webFixture)
	ctx := context.Background()

	png, err := renderer.Screenshot(ctx, artifact, ScreenshotOptions{Slide: -1})
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !isPNG(png) {
		t.Fatalf("viewport screenshot is not a PNG (%d bytes)", len(png))
	}

	element, err := renderer.Screenshot(ctx, artifact, ScreenshotOptions{Slide: -1, Selector: "#hero-cta"})
	if err != nil {
		t.Fatalf("Screenshot(selector): %v", err)
	}
	if !isPNG(element) {
		t.Fatal("element screenshot is not a PNG")
	}
	if len(element) >= len(png) {
		t.Fatalf("element screenshot (%d bytes) is not smaller than the page (%d bytes)", len(element), len(png))
	}
}

func TestScreenshotOfOneDeckSlide(t *testing.T) {
	_, artifact, renderer := newRenderedArtifact(t, KindDeck, deckFixture)

	png, err := renderer.Screenshot(context.Background(), artifact, ScreenshotOptions{Slide: 1})
	if err != nil {
		t.Fatalf("Screenshot(slide): %v", err)
	}
	if !isPNG(png) {
		t.Fatal("slide screenshot is not a PNG")
	}

	if _, err := renderer.Screenshot(context.Background(), artifact, ScreenshotOptions{Slide: 9}); err == nil {
		t.Fatal("screenshot of a slide that does not exist succeeded")
	}
}

// TestRasterizeCanvas covers the image-generation path: the browser rasterizes
// a canvas scene, so no image-model provider is involved.
func TestRasterizeCanvas(t *testing.T) {
	svc, _ := newTestService(t)
	renderer := newTestRenderer(t, svc)

	scene := `<!doctype html><html><body style="margin:0">
<canvas id="c" width="320" height="240"></canvas>
<script>
  var ctx = document.getElementById('c').getContext('2d');
  ctx.fillStyle = '#2b6cb0'; ctx.fillRect(0, 0, 320, 240);
  ctx.fillStyle = '#fff'; ctx.font = '24px sans-serif'; ctx.fillText('pando', 20, 60);
</script></body></html>`

	png, err := renderer.Rasterize(context.Background(), scene, 320, 240, 0)
	if err != nil {
		t.Fatalf("Rasterize: %v", err)
	}
	if !isPNG(png) {
		t.Fatalf("rasterized output is not a PNG (%d bytes)", len(png))
	}

	if _, err := renderer.Rasterize(context.Background(), "  ", 320, 240, 0); err == nil {
		t.Fatal("Rasterize accepted an empty document")
	}
}

func TestRenderWithoutRendererReportsNoBrowser(t *testing.T) {
	svc, _ := newTestService(t)
	artifact, err := svc.Create(context.Background(), CreateParams{Title: "No browser"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Render(context.Background(), artifact.ID, RenderOptions{}); err == nil {
		t.Fatal("Render succeeded without a renderer attached")
	}
}

func isPNG(data []byte) bool {
	return bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
}
