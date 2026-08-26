package design

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/digiogithub/pando/internal/design/preview"
)

// The loop P3 exists to make possible: an artifact published over HTTP, loaded
// by a real browser at the URL a surface would open, with the selection bridge
// live and the ids it exposes matching the ids the renderer stored.
//
// It is the one test that proves the browser a user opens and the index an
// agent patches through are talking about the same elements.
func TestEndToEndBridgedPreviewInARealBrowser(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	renderer := newTestRenderer(t, svc)
	svc = svc.WithRenderer(renderer)

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Preview Landing",
		Kind:  KindWeb,
		Files: map[string]string{"index.html": webFixture},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Render(ctx, artifact.ID, RenderOptions{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	indexed, err := svc.Inspect(ctx, artifact.ID, 0, InspectOptions{Text: "Ship design faster"})
	if err != nil || len(indexed.Nodes) == 0 {
		t.Fatalf("inspect: %v (%d nodes)", err, len(indexed.Nodes))
	}
	headingID := indexed.Nodes[0].NodeID

	server := withPreviewServer(t)
	presentation, err := svc.Presentation(ctx, artifact.ID, 0, headingID)
	if err != nil {
		t.Fatalf("presentation: %v", err)
	}
	if !strings.HasPrefix(presentation.URL, "http://"+server.Addr()) {
		t.Fatalf("the presented URL is not the served preview: %q", presentation.URL)
	}

	browserCtx, err := renderer.session.context()
	if err != nil {
		t.Fatalf("browser: %v", err)
	}
	timed, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	defer cancel()

	var stampedHeading, bridgeLoaded, selection string
	err = chromedp.Run(timed,
		chromedp.Navigate(presentation.BridgeURL),
		chromedp.WaitReady("body"),
		// The stamping preamble runs on DOMContentLoaded; give the bridge its
		// own tick to report before reading back.
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(document.querySelector('#hero h1') || {}).getAttribute ? document.querySelector('#hero h1').getAttribute('data-pando-id') : ''`, &stampedHeading),
		chromedp.Evaluate(`typeof document.querySelector('script[src$="_bridge.js"]') !== 'undefined' && document.querySelector('script[src$="_bridge.js"]') ? 'yes' : 'no'`, &bridgeLoaded),
		// Drive the bridge the way the Studio does: ask it to select a node and
		// read back what it marked.
		chromedp.Evaluate(`window.postMessage({source:'pando-design', type:'select', nodeId:'`+headingID+`'}, '*'), 'sent'`, new(string)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(document.querySelector('[data-pando-selected]') || {getAttribute:function(){return ''}}).getAttribute('data-pando-id') || ''`, &selection),
	)
	if err != nil {
		t.Fatalf("browser run: %v", err)
	}

	if bridgeLoaded != "yes" {
		t.Fatal("the bridge script tag was not injected into the served document")
	}
	if stampedHeading != headingID {
		t.Fatalf("the browser stamped the heading %q but the stored index calls it %q; the selection protocol would be broken", stampedHeading, headingID)
	}
	if selection != headingID {
		t.Fatalf("the bridge selected %q, expected %q", selection, headingID)
	}
}

// A preview opened without the bridge must be exactly the file on disk: an
// artifact the user opens directly, or exports, carries none of Pando's
// instrumentation.
func TestPlainPreviewIsUninstrumented(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	svc = svc.WithRenderer(newTestRenderer(t, svc))

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Clean Landing",
		Kind:  KindWeb,
		Files: map[string]string{"index.html": webFixture},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	server := withPreviewServer(t)
	if _, err := svc.PublishPreview(ctx, artifact.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	url, err := server.URL(artifact.ID, preview.URLOptions{})
	if err != nil {
		t.Fatalf("url: %v", err)
	}

	browserCtx, err := svc.Renderer().session.context()
	if err != nil {
		t.Fatalf("browser: %v", err)
	}
	timed, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	defer cancel()

	var stamped int
	if err := chromedp.Run(timed,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('[data-pando-id]').length`, &stamped),
	); err != nil {
		t.Fatalf("browser run: %v", err)
	}
	if stamped != 0 {
		t.Fatalf("a plain preview must carry no instrumentation, found %d stamped elements", stamped)
	}
}
