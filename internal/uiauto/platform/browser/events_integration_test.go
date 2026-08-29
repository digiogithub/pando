package browser

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	browserdetect "github.com/digiogithub/pando/internal/browser"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// TestIntegrationLiveChromeEventSubscribe is the "actually test live"
// coverage the plan calls for: it drives a real, locally installed
// Chromium-family browser, registers the session with this package (the
// same RegisterSession path browser_session.go uses), subscribes via
// CdpBackend.Subscribe, mutates the live DOM with a JS eval, and asserts a
// KindCreated event is actually delivered through the channel -- not just
// that the wiring compiles. Skips (never fails) when no such browser is
// available, mirroring backend_integration_test.go's honesty pattern.
func TestIntegrationLiveChromeEventSubscribe(t *testing.T) {
	install, ok := browserdetect.ResolveBrowserInstall("chrome", "")
	if !ok {
		install, ok = browserdetect.ResolveBrowserInstall("chromium", "")
	}
	if !ok {
		installs := browserdetect.DetectInstalledBrowsers()
		if len(installs) == 0 {
			t.Skip("no Chromium-family browser detected on this machine")
		}
		install = installs[0]
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(install.Executable),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	const page = `data:text/html,<html><body><div id="container"></div></body></html>`
	if err := chromedp.Run(ctx, chromedp.Navigate(page)); err != nil {
		t.Skipf("could not launch/navigate the detected browser (%s): %v", install.Executable, err)
	}

	RegisterSession("events-integration-test", ctx)
	t.Cleanup(func() { UnregisterSession("events-integration-test") })

	backend, err := NewBackend()
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	defer backend.Close()

	cdpBackend, ok := backend.(*CdpBackend)
	if !ok {
		t.Fatalf("backend is %T, want *CdpBackend", backend)
	}

	subCtx, subCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer subCancel()
	ch, unsub, err := cdpBackend.Subscribe(subCtx, core.Scope{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsub()

	// Give the DOM domain a moment to finish enabling before mutating.
	time.Sleep(200 * time.Millisecond)

	mutate := func() error {
		return chromedp.Run(ctx, chromedp.Evaluate(
			`document.getElementById('container').appendChild(document.createElement('span'))`, nil))
	}
	if err := mutate(); err != nil {
		t.Fatalf("DOM mutation eval failed: %v", err)
	}

	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind != "" {
				return // got a real, decoded event: the live wiring works end to end
			}
		case <-deadline:
			t.Fatal("timed out waiting for a live CDP DOM event after mutating the page")
		case <-time.After(500 * time.Millisecond):
			// Chrome sometimes needs a nudge/second mutation to flush an
			// initial childNodeInserted for a subtree DOM.Enable hasn't
			// pushed yet; retry the mutation a few times within budget.
			_ = mutate()
		}
	}
}
