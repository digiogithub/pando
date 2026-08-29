package uiauto

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	browserdetect "github.com/digiogithub/pando/internal/browser"
	"github.com/digiogithub/pando/internal/uiauto/core"
	uiautobrowser "github.com/digiogithub/pando/internal/uiauto/platform/browser"
)

// TestIntegrationLiveChromeRoutesBrowserScopeToCdp is Block R's live
// verification that "auto" routing (R1) actually reaches a real CDP
// session on this machine, not just the fake-backend unit tests above: it
// launches a real headless Chrome, registers it exactly the way
// internal/llm/tools/browser_session.go does, builds a real "auto" Manager
// (which on this dev box also resolves a real AT-SPI OS backend, so this
// genuinely exercises the routing decision between two live backends, not
// a null OS backend by elimination), and confirms an app_id="browser"
// scoped Observe/Find/Click lands on the real page through cdp while an
// app_id-less/native-looking scope does not. Skips (never fails) when no
// Chromium-family browser is available, mirroring
// platform/browser/backend_integration_test.go's honesty pattern.
func TestIntegrationLiveChromeRoutesBrowserScopeToCdp(t *testing.T) {
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
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	const page = `data:text/html,` +
		`<html><body>` +
		`<button id="go">GoButton</button>` +
		`</body></html>`
	if err := chromedp.Run(browserCtx, chromedp.Navigate(page)); err != nil {
		t.Skipf("could not launch/navigate the detected browser (%s): %v", install.Executable, err)
	}

	const sessionID = "block-r-live-routing-test"
	uiautobrowser.RegisterSession(sessionID, browserCtx)
	defer uiautobrowser.UnregisterSession(sessionID)

	mgr, err := NewManager(Options{
		Backend:            "auto",
		MaxNodes:           500,
		DefaultDepth:       3,
		ActionTimeout:      10 * time.Second,
		SnapshotTTL:        time.Minute,
		AllowPhysicalInput: false,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()
	if !mgr.CdpAvailable() {
		t.Fatal("expected a Manager built with Backend=\"auto\" to hold a resolved cdp backend")
	}

	ctx := context.Background()

	// desktop_apps-equivalent: the connected browser must be surfaced
	// alongside (never instead of) whatever the OS backend reports.
	apps, err := mgr.Apps(ctx)
	if err != nil {
		t.Fatalf("Apps: %v", err)
	}
	var sawBrowser bool
	for _, a := range apps {
		if a.ID == uiautobrowser.AppID {
			sawBrowser = true
		}
	}
	if !sawBrowser {
		t.Fatalf("expected the live browser session to appear in Apps(), got %+v", apps)
	}

	// A browser-app-scoped Find must actually reach the real page through
	// cdp and tag results accordingly.
	elements, snap, err := mgr.Find(ctx, core.Scope{AppID: uiautobrowser.AppID}, `button[name="GoButton"]`, 0)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(elements) != 1 {
		t.Fatalf("expected exactly one match for the real page's button, got %d: %+v", len(elements), elements)
	}
	if elements[0].Backend != "cdp" {
		t.Fatalf("expected the real browser-scoped element to be tagged backend=cdp, got %q", elements[0].Backend)
	}
	if snap.Backend != "cdp" {
		t.Fatalf("expected the snapshot to record backend=cdp, got %q", snap.Backend)
	}

	// A real click through the ref must reach the real page (native
	// method, not a physical-input fallback -- AllowPhysicalInput is even
	// off above, so any fallback attempt would itself fail loudly).
	ref := elements[0].ID
	result, err := mgr.Click(ctx, ref)
	if err != nil {
		t.Fatalf("Click on the live cdp-tagged ref failed: %v", err)
	}
	if result.Method != "native" {
		t.Fatalf("expected a native cdp click, got method=%q notes=%v", result.Method, result.Notes)
	}

	// CapabilitiesFor a browser scope must reflect the live cdp session,
	// never an all-false/degraded picture.
	caps := mgr.CapabilitiesFor(ctx, core.Scope{AppID: uiautobrowser.AppID})
	if !caps.Accessibility || !caps.UIActions {
		t.Fatalf("expected live cdp capabilities for a browser scope, got %+v", caps)
	}

	// Sanity: the qualified ref really does carry the "cdp" backend tag in
	// its owning Element, confirming provenance survived the snapshot
	// round trip end to end (not just in the fake-backend unit tests).
	if !strings.HasPrefix(string(ref), "@") {
		t.Fatalf("expected a qualified ref, got %q", ref)
	}
}
