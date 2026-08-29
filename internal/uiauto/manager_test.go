package uiauto

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/events"
	browserpkg "github.com/digiogithub/pando/internal/uiauto/platform/browser"
	"github.com/digiogithub/pando/internal/uiauto/screen"
)

// fakeBackend is a minimal, deterministic core.Backend used to exercise
// Manager without any OS call. Non-root elements are matched by pointer
// identity (they are always the exact *core.Element the backend itself
// created and previously returned); the synthetic window-root element that
// Manager.rootElement builds is matched by WindowID instead, since Manager
// constructs that pointer itself.
type fakeBackend struct {
	// name lets a fakeBackend impersonate a specific registered backend
	// name (e.g. "cdp") for routing tests; defaults to "fake".
	name       string
	caps       core.Capabilities
	apps       []core.AppInfo
	windows    []core.WindowInfo
	childrenOf map[*core.Element][]*core.Element
	rootByWin  map[string][]*core.Element

	findResult []*core.Element
	findErr    error

	performErr    error
	performCalls  int
	childrenCalls int
	windowsCalls  int
	availableErr  error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		childrenOf: make(map[*core.Element][]*core.Element),
		rootByWin:  make(map[string][]*core.Element),
	}
}

func (b *fakeBackend) Name() string {
	if b.name != "" {
		return b.name
	}
	return "fake"
}

func (b *fakeBackend) Available(ctx context.Context) (core.Capabilities, error) {
	return b.caps, b.availableErr
}

func (b *fakeBackend) Apps(ctx context.Context) ([]core.AppInfo, error) {
	return b.apps, nil
}

func (b *fakeBackend) Windows(ctx context.Context, appID string) ([]core.WindowInfo, error) {
	b.windowsCalls++
	if appID == "" {
		return b.windows, nil
	}
	var out []core.WindowInfo
	for _, w := range b.windows {
		if w.AppID == appID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (b *fakeBackend) Find(ctx context.Context, scope core.Scope, sel *core.Selector, limit int) ([]*core.Element, error) {
	return b.findResult, b.findErr
}

func (b *fakeBackend) Children(ctx context.Context, el *core.Element) ([]*core.Element, error) {
	b.childrenCalls++
	if el.Role == core.RoleWindow && el.WindowID != "" {
		if kids, ok := b.rootByWin[el.WindowID]; ok {
			return kids, nil
		}
	}
	return b.childrenOf[el], nil
}

func (b *fakeBackend) Properties(ctx context.Context, el *core.Element, props []string) (map[string]any, error) {
	return nil, nil
}

func (b *fakeBackend) Perform(ctx context.Context, el *core.Element, action core.Action) error {
	b.performCalls++
	return b.performErr
}

func (b *fakeBackend) Close() error { return nil }

func testOptions(overrides func(*Options)) Options {
	opts := Options{
		Backend:            "fake",
		MaxNodes:           500,
		DefaultDepth:       3,
		ActionTimeout:      2 * time.Second,
		SnapshotTTL:        time.Minute,
		AllowPhysicalInput: true,
	}
	if overrides != nil {
		overrides(&opts)
	}
	return opts
}

// managerWithBackend builds a Manager wired directly to backend as its
// osBackend, bypassing the Registry (Options.Backend is irrelevant here;
// NewManager only touches the registry to resolve a backend, so we
// construct the Manager fields that matter for these tests through a tiny
// local constructor mirroring NewManager's defaulting). It never wires a
// cdpBackend (pinned=false but cdpBackend=nil is the same as pinned for
// routing purposes: CdpAvailable() is false either way), so callers that
// want to exercise routing use managerWithBackends instead.
func managerWithBackend(t *testing.T, backend core.Backend, opts Options) *Manager {
	t.Helper()
	return managerWithBackends(t, backend, nil, opts)
}

// managerWithBackends builds a Manager wired directly to osBackend and
// cdpBackend (either may be nil), bypassing the Registry, for tests that
// need to exercise per-scope routing without a real "cdp" registration.
func managerWithBackends(t *testing.T, osBackend, cdpBackend core.Backend, opts Options) *Manager {
	t.Helper()
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 500
	}
	if opts.DefaultDepth <= 0 {
		opts.DefaultDepth = 3
	}
	if opts.ActionTimeout <= 0 {
		opts.ActionTimeout = 2 * time.Second
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = time.Minute
	}
	caps, _ := osBackend.Available(context.Background())
	return &Manager{
		osBackend:     osBackend,
		osBackendName: osBackend.Name(),
		cdpBackend:    cdpBackend,
		pinned:        cdpBackend == nil,
		snapshots:     core.NewSnapshotStore(opts.SnapshotTTL, defaultSnapshotCap),
		capabilities:  caps,
		opts:          opts,
	}
}

func elem(role core.Role, name string) *core.Element {
	return &core.Element{Role: role, Name: name, Enabled: true, Visible: true}
}

func TestNewManagerFallsBackToNullBackend(t *testing.T) {
	mgr, err := NewManager(Options{Backend: "does-not-exist"})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if mgr.BackendName() != "null" {
		t.Fatalf("expected fallback to null backend, got %q", mgr.BackendName())
	}
	if _, err := mgr.Apps(context.Background()); err == nil {
		t.Fatal("expected PLATFORM_NOT_SUPPORTED from null backend Apps")
	} else if de, ok := core.AsDesktopError(err); !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("expected PLATFORM_NOT_SUPPORTED, got %v", err)
	}
}

func TestManagerScreenshotPlatformNotSupportedWhenScreenCapturerFails(t *testing.T) {
	restore := fakeCaptureScreen(func(ctx context.Context, target screen.Target) (image.Image, error) {
		return nil, core.NewPlatformNotSupportedError("no screen capture backend is available on this platform/session")
	})
	defer restore()

	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	_, _, err := mgr.Screenshot(context.Background(), "screen", false)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("expected PLATFORM_NOT_SUPPORTED, got %v", err)
	}
}

func TestManagerObserveBuildsTreeWithDepthAndBudget(t *testing.T) {
	b := newFakeBackend()
	b.windows = []core.WindowInfo{{ID: "w1", AppID: "app1", Title: "Main Window"}}

	// w1-root -> A -> [B, C]; B -> [D, E]; C has no children.
	a := elem(core.RoleGroup, "A")
	bEl := elem(core.RoleGroup, "B")
	c := elem(core.RoleButton, "C")
	d := elem(core.RoleButton, "D")
	e := elem(core.RoleButton, "E")
	b.rootByWin["w1"] = []*core.Element{a}
	b.childrenOf[a] = []*core.Element{bEl, c}
	b.childrenOf[bEl] = []*core.Element{d, e}

	mgr := managerWithBackend(t, b, testOptions(func(o *Options) { o.DefaultDepth = 2 }))

	snap, err := mgr.Observe(context.Background(), core.Scope{AppID: "app1", WindowID: "w1"}, 0)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}
	// root(depth0=window) + A(depth1) + B,C(depth2) = 4 elements; D/E at
	// depth3 are beyond DefaultDepth=2 and must not appear.
	if got, want := len(snap.Elements), 4; got != want {
		t.Fatalf("expected %d elements, got %d: %+v", want, got, snap.Elements)
	}
	if snap.Root == nil || snap.Root.Role != core.RoleWindow {
		t.Fatalf("expected window root, got %+v", snap.Root)
	}

	// Every element ID must be resolvable through the snapshot store.
	for id, el := range snap.Elements {
		_, resolved, err := mgr.snapshots.Resolve(el.ID)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		if resolved != el {
			t.Fatalf("resolved element mismatch for %s", id)
		}
	}
}

func TestManagerObserveRespectsMaxNodes(t *testing.T) {
	b := newFakeBackend()
	b.windows = []core.WindowInfo{{ID: "w1", AppID: "app1", Title: "Main"}}
	var kids []*core.Element
	for i := 0; i < 10; i++ {
		kids = append(kids, elem(core.RoleButton, "child"))
	}
	b.rootByWin["w1"] = kids

	mgr := managerWithBackend(t, b, testOptions(func(o *Options) { o.MaxNodes = 3; o.DefaultDepth = 5 }))
	snap, err := mgr.Observe(context.Background(), core.Scope{AppID: "app1", WindowID: "w1"}, 0)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}
	if len(snap.Elements) != 3 {
		t.Fatalf("expected snapshot capped at MaxNodes=3, got %d", len(snap.Elements))
	}
}

func TestManagerObserveWindowNotFound(t *testing.T) {
	b := newFakeBackend()
	b.windows = []core.WindowInfo{{ID: "w1", AppID: "app1"}}
	mgr := managerWithBackend(t, b, testOptions(nil))
	_, err := mgr.Observe(context.Background(), core.Scope{AppID: "app1", WindowID: "missing"}, 0)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrElementNotFound {
		t.Fatalf("expected ELEMENT_NOT_FOUND, got %v", err)
	}
}

func TestManagerPolicyDeniedList(t *testing.T) {
	b := newFakeBackend()
	b.windows = []core.WindowInfo{{ID: "w1", AppID: "blocked"}}
	mgr := managerWithBackend(t, b, testOptions(func(o *Options) { o.DeniedApps = []string{"blocked"} }))

	_, err := mgr.Windows(context.Background(), "blocked")
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPolicyDenied {
		t.Fatalf("expected POLICY_DENIED, got %v", err)
	}
}

func TestManagerPolicyAllowList(t *testing.T) {
	b := newFakeBackend()
	b.windows = []core.WindowInfo{{ID: "w1", AppID: "app1"}, {ID: "w2", AppID: "app2"}}
	mgr := managerWithBackend(t, b, testOptions(func(o *Options) { o.AllowedApps = []string{"app1"} }))

	if _, err := mgr.Windows(context.Background(), "app1"); err != nil {
		t.Fatalf("app1 should be allowed: %v", err)
	}
	_, err := mgr.Windows(context.Background(), "app2")
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPolicyDenied {
		t.Fatalf("expected POLICY_DENIED for app2, got %v", err)
	}
}

func TestManagerAppsFiltersByPolicy(t *testing.T) {
	b := newFakeBackend()
	b.apps = []core.AppInfo{{ID: "app1", Name: "Allowed"}, {ID: "app2", Name: "Blocked"}}
	mgr := managerWithBackend(t, b, testOptions(func(o *Options) { o.DeniedApps = []string{"app2"} }))

	apps, err := mgr.Apps(context.Background())
	if err != nil {
		t.Fatalf("Apps failed: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "app1" {
		t.Fatalf("expected only app1, got %+v", apps)
	}
}

func TestManagerFindAndActions(t *testing.T) {
	b := newFakeBackend()
	target := elem(core.RoleButton, "Save")
	target.AppID = "app1"
	target.Bounds = core.Bounds{X: 10, Y: 10, W: 20, H: 20}
	b.findResult = []*core.Element{target}

	mgr := managerWithBackend(t, b, testOptions(nil))
	elements, snap, err := mgr.Find(context.Background(), core.Scope{AppID: "app1"}, `button[name="Save"]`, 0)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	if len(elements) != 1 || snap.ID == "" {
		t.Fatalf("unexpected find result: %+v snap=%+v", elements, snap)
	}
	ref := elements[0].ID

	// Read
	read, err := mgr.Read(context.Background(), ref)
	if err != nil || read.Name != "Save" {
		t.Fatalf("Read failed: %v %+v", err, read)
	}

	// Click: native success.
	res, err := mgr.Click(context.Background(), ref)
	if err != nil {
		t.Fatalf("Click failed: %v", err)
	}
	if res.Method != "native" {
		t.Fatalf("expected native click, got %q", res.Method)
	}

	// Type, Scroll, Focus, Key all succeed natively too.
	if _, err := mgr.Type(context.Background(), ref, "hello"); err != nil {
		t.Fatalf("Type failed: %v", err)
	}
	if _, err := mgr.Scroll(context.Background(), ref, -100); err != nil {
		t.Fatalf("Scroll failed: %v", err)
	}
	if _, err := mgr.Focus(context.Background(), ref); err != nil {
		t.Fatalf("Focus failed: %v", err)
	}
	if _, err := mgr.Key(context.Background(), ref, "Enter"); err != nil {
		t.Fatalf("Key failed: %v", err)
	}
}

func TestManagerFindInvalidSelector(t *testing.T) {
	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	_, _, err := mgr.Find(context.Background(), core.Scope{}, "!!!not a selector", 0)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrInvalidArgs {
		t.Fatalf("expected INVALID_ARGS, got %v", err)
	}
}

func TestManagerActionOnStaleRef(t *testing.T) {
	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	_, err := mgr.Click(context.Background(), core.ElementRef("@snonexistent:e1"))
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrSnapshotNotFound {
		t.Fatalf("expected SNAPSHOT_NOT_FOUND, got %v", err)
	}
}

func TestManagerWaitTimesOut(t *testing.T) {
	b := newFakeBackend()
	b.findResult = nil // never matches
	mgr := managerWithBackend(t, b, testOptions(nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := mgr.Wait(ctx, core.Scope{}, `button[name="Ghost"]`, core.ConditionExists, 50*time.Millisecond)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrTimeout {
		t.Fatalf("expected TIMEOUT, got %v", err)
	}
}

func TestManagerKeyGlobalWithoutPhysicalInput(t *testing.T) {
	mgr := managerWithBackend(t, newFakeBackend(), testOptions(func(o *Options) { o.AllowPhysicalInput = true }))
	// managerWithBackend always wires a nil PhysicalInput (it bypasses
	// NewManager's own Phase 3 wiring on purpose, to isolate Manager's
	// tree/policy logic from the platform layer in the tests above), so a
	// global (ref-less) key press must fail with PLATFORM_NOT_SUPPORTED
	// rather than panic.
	_, err := mgr.Key(context.Background(), "", "Enter")
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("expected PLATFORM_NOT_SUPPORTED, got %v", err)
	}
}

// ---- Phase 3: physical input / screen capture wiring ----

// fakePhysical is a deterministic core.PhysicalInput used to verify the
// ActionResolver's native-first/physical-fallback path actually reaches
// the physical layer through Manager, and to record what it was asked to
// do.
type fakePhysical struct {
	clicks    [][2]int
	moves     [][2]int
	typed     []string
	pressed   []string
	scrolls   [][3]int
	returnErr error
}

func (f *fakePhysical) Click(x, y int) error {
	f.clicks = append(f.clicks, [2]int{x, y})
	return f.returnErr
}
func (f *fakePhysical) MoveMouse(x, y int) error {
	f.moves = append(f.moves, [2]int{x, y})
	return f.returnErr
}
func (f *fakePhysical) TypeText(s string) error {
	f.typed = append(f.typed, s)
	return f.returnErr
}
func (f *fakePhysical) PressKey(key string) error {
	f.pressed = append(f.pressed, key)
	return f.returnErr
}
func (f *fakePhysical) Scroll(x, y, amount int) error {
	f.scrolls = append(f.scrolls, [3]int{x, y, amount})
	return f.returnErr
}

// fakeCaptureScreen substitutes captureScreen for the duration of a test
// and returns a restore func.
func fakeCaptureScreen(fn func(ctx context.Context, target screen.Target) (image.Image, error)) func() {
	orig := captureScreen
	captureScreen = fn
	return func() { captureScreen = orig }
}

// fakeNewPhysicalInput substitutes newPhysicalInput/physicalCapabilities
// for the duration of a test and returns a restore func.
func fakeNewPhysicalInput(p core.PhysicalInput, caps core.Capabilities) func() {
	origNew, origCaps := newPhysicalInput, physicalCapabilities
	newPhysicalInput = func() (core.PhysicalInput, error) { return p, nil }
	physicalCapabilities = func() core.Capabilities { return caps }
	return func() {
		newPhysicalInput = origNew
		physicalCapabilities = origCaps
	}
}

func fakeScreenCapabilities(caps core.Capabilities) func() {
	orig := screenCapabilities
	screenCapabilities = func() core.Capabilities { return caps }
	return func() { screenCapabilities = orig }
}

// TestManagerActionResolverPhysicalFallback verifies that a native
// Perform failure on the backend actually reaches the physical layer
// through Manager.Click — i.e. that NewManager really wires
// internal/uiauto/input's PhysicalInput into the ActionResolver (Phase
// 3), not just that the Phase 0 core logic works in isolation.
func TestManagerActionResolverPhysicalFallback(t *testing.T) {
	fp := &fakePhysical{}
	restore := fakeNewPhysicalInput(fp, core.Capabilities{Mouse: true, Keyboard: true})
	defer restore()

	globalRegistry.Register("fake-physical-fallback-backend", func() (core.Backend, error) {
		b := newFakeBackend()
		b.performErr = core.NewActionFailedError("native action unsupported by fake backend")
		target := elem(core.RoleButton, "Save")
		target.AppID = "app1"
		target.Bounds = core.Bounds{X: 10, Y: 20, W: 30, H: 40}
		b.findResult = []*core.Element{target}
		return b, nil
	})

	mgr, err := NewManager(testOptions(func(o *Options) {
		o.Backend = "fake-physical-fallback-backend"
		o.AllowPhysicalInput = true
	}))
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	elements, _, err := mgr.Find(context.Background(), core.Scope{AppID: "app1"}, `button[name="Save"]`, 0)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	ref := elements[0].ID

	res, err := mgr.Click(context.Background(), ref)
	if err != nil {
		t.Fatalf("Click failed: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected physical fallback, got %q", res.Method)
	}
	if len(fp.clicks) != 1 || fp.clicks[0] != [2]int{25, 40} {
		t.Fatalf("expected one physical click at element bounds center (25,40), got %v", fp.clicks)
	}
}

// TestNewManagerMergesCapabilities verifies that NewManager ORs the
// backend's, the physical input's, and the screen capturer's capabilities
// together, and never reports a capability none of them actually offer.
func TestNewManagerMergesCapabilities(t *testing.T) {
	globalRegistry.Register("fake-caps-backend", func() (core.Backend, error) {
		b := newFakeBackend()
		b.caps = core.Capabilities{Accessibility: true, UIActions: true} // no Mouse/Keyboard/Screenshot
		return b, nil
	})

	t.Run("physical input off", func(t *testing.T) {
		restoreInput := fakeNewPhysicalInput(&fakePhysical{}, core.Capabilities{Mouse: true, Keyboard: true})
		defer restoreInput()
		restoreScreen := fakeScreenCapabilities(core.Capabilities{Screenshot: true})
		defer restoreScreen()

		mgr, err := NewManager(Options{Backend: "fake-caps-backend", AllowPhysicalInput: false})
		if err != nil {
			t.Fatalf("NewManager error: %v", err)
		}
		caps := mgr.Capabilities()
		if caps.Mouse || caps.Keyboard {
			t.Fatalf("expected no Mouse/Keyboard when AllowPhysicalInput is false, got %+v", caps)
		}
		if !caps.Screenshot {
			t.Fatalf("expected Screenshot true from the screen capturer regardless of AllowPhysicalInput, got %+v", caps)
		}
		if !caps.Accessibility || !caps.UIActions {
			t.Fatalf("expected backend capabilities to be preserved, got %+v", caps)
		}
	})

	t.Run("physical input on", func(t *testing.T) {
		restoreInput := fakeNewPhysicalInput(&fakePhysical{}, core.Capabilities{Mouse: true, Keyboard: true})
		defer restoreInput()
		restoreScreen := fakeScreenCapabilities(core.Capabilities{})
		defer restoreScreen()

		mgr, err := NewManager(Options{Backend: "fake-caps-backend", AllowPhysicalInput: true})
		if err != nil {
			t.Fatalf("NewManager error: %v", err)
		}
		caps := mgr.Capabilities()
		if !caps.Mouse || !caps.Keyboard {
			t.Fatalf("expected Mouse/Keyboard true from the physical input layer, got %+v", caps)
		}
		if caps.Screenshot {
			t.Fatalf("expected Screenshot false when the screen capturer offers nothing, got %+v", caps)
		}
	})
}

// syntheticImage builds a solid-color image.RGBA of the given size, so
// tests can assert on captured/scaled/cropped pixel dimensions without any
// real display.
func syntheticImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestManagerScreenshotEncodesPNG(t *testing.T) {
	restore := fakeCaptureScreen(func(ctx context.Context, target screen.Target) (image.Image, error) {
		if target.Region != nil || target.WindowID != "" {
			t.Fatalf("expected a whole-screen target for \"screen\", got %+v", target)
		}
		return syntheticImage(64, 48, color.RGBA{R: 10, G: 20, B: 30, A: 255}), nil
	})
	defer restore()

	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	data, mime, err := mgr.Screenshot(context.Background(), "screen", false)
	if err != nil {
		t.Fatalf("Screenshot failed: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("expected image/png, got %q", mime)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding screenshot PNG failed: %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 48 {
		t.Fatalf("expected 64x48, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestManagerScreenshotAppliesScreenshotScale(t *testing.T) {
	restore := fakeCaptureScreen(func(ctx context.Context, target screen.Target) (image.Image, error) {
		return syntheticImage(100, 200, color.RGBA{R: 1, G: 2, B: 3, A: 255}), nil
	})
	defer restore()

	mgr := managerWithBackend(t, newFakeBackend(), testOptions(func(o *Options) { o.ScreenshotScale = 0.5 }))
	data, _, err := mgr.Screenshot(context.Background(), "screen", false)
	if err != nil {
		t.Fatalf("Screenshot failed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding screenshot PNG failed: %v", err)
	}
	if img.Bounds().Dx() != 50 || img.Bounds().Dy() != 100 {
		t.Fatalf("expected 50x100 after a 0.5 ScreenshotScale, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestManagerScreenshotCropsToElementBounds(t *testing.T) {
	var gotTarget screen.Target
	restore := fakeCaptureScreen(func(ctx context.Context, target screen.Target) (image.Image, error) {
		gotTarget = target
		if target.Region == nil {
			t.Fatalf("expected a Region target for an element ref screenshot")
		}
		return syntheticImage(target.Region.W, target.Region.H, color.RGBA{R: 9, G: 9, B: 9, A: 255}), nil
	})
	defer restore()

	b := newFakeBackend()
	target := elem(core.RoleButton, "Save")
	target.AppID = "app1"
	target.Bounds = core.Bounds{X: 5, Y: 6, W: 40, H: 20}
	b.findResult = []*core.Element{target}

	mgr := managerWithBackend(t, b, testOptions(nil))
	elements, _, err := mgr.Find(context.Background(), core.Scope{AppID: "app1"}, `button[name="Save"]`, 0)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	ref := string(elements[0].ID)

	data, _, err := mgr.Screenshot(context.Background(), ref, false)
	if err != nil {
		t.Fatalf("Screenshot failed: %v", err)
	}
	if gotTarget.Region.X != 5 || gotTarget.Region.Y != 6 || gotTarget.Region.W != 40 || gotTarget.Region.H != 20 {
		t.Fatalf("unexpected capture region: %+v", gotTarget.Region)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding screenshot PNG failed: %v", err)
	}
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 {
		t.Fatalf("expected 40x20, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestManagerScreenshotWindowTarget(t *testing.T) {
	var gotTarget screen.Target
	restore := fakeCaptureScreen(func(ctx context.Context, target screen.Target) (image.Image, error) {
		gotTarget = target
		return syntheticImage(10, 10, color.RGBA{A: 255}), nil
	})
	defer restore()

	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	if _, _, err := mgr.Screenshot(context.Background(), "window:w1", false); err != nil {
		t.Fatalf("Screenshot failed: %v", err)
	}
	if gotTarget.WindowID != "w1" {
		t.Fatalf("expected WindowID %q, got %q", "w1", gotTarget.WindowID)
	}
}

func TestManagerScreenshotInvalidTarget(t *testing.T) {
	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	_, _, err := mgr.Screenshot(context.Background(), "not-a-valid-target", false)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrInvalidArgs {
		t.Fatalf("expected INVALID_ARGS, got %v", err)
	}
}

func TestManagerScreenshotWithGridOverlaysCoordinates(t *testing.T) {
	restore := fakeCaptureScreen(func(ctx context.Context, target screen.Target) (image.Image, error) {
		return syntheticImage(120, 120, color.RGBA{R: 5, G: 5, B: 5, A: 255}), nil
	})
	defer restore()

	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	withGrid, _, err := mgr.Screenshot(context.Background(), "screen", true)
	if err != nil {
		t.Fatalf("Screenshot(grid=true) failed: %v", err)
	}
	withoutGrid, _, err := mgr.Screenshot(context.Background(), "screen", false)
	if err != nil {
		t.Fatalf("Screenshot(grid=false) failed: %v", err)
	}
	if len(withGrid) == len(withoutGrid) && bytes.Equal(withGrid, withoutGrid) {
		t.Fatal("expected the grid-annotated screenshot to differ from the plain one")
	}
	if _, err := png.Decode(bytes.NewReader(withGrid)); err != nil {
		t.Fatalf("grid-annotated screenshot is not a decodable PNG: %v", err)
	}
}

// fakeListDisplays substitutes listDisplays for the duration of a test.
func fakeListDisplays(displays []screen.DisplayInfo, err error) func() {
	orig := listDisplays
	listDisplays = func() ([]screen.DisplayInfo, error) { return displays, err }
	return func() { listDisplays = orig }
}

func TestManagerClickAtRequiresAllowPhysicalInput(t *testing.T) {
	fp := &fakePhysical{}
	restore := fakeNewPhysicalInput(fp, core.Capabilities{Mouse: true})
	defer restore()

	mgr, err := NewManager(testOptions(func(o *Options) {
		o.Backend = "null"
		o.AllowPhysicalInput = false
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	_, err = mgr.ClickAt(context.Background(), 10, 10)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPolicyDenied {
		t.Fatalf("expected POLICY_DENIED, got %v", err)
	}
	if len(fp.clicks) != 0 {
		t.Fatalf("expected no physical click when AllowPhysicalInput is false, got %v", fp.clicks)
	}
}

func TestManagerClickAtNoPhysicalBackend(t *testing.T) {
	restore := fakeNewPhysicalInput(nil, core.Capabilities{})
	// Force newPhysicalInput to fail so resolver.Physical stays nil, mirroring
	// a platform where the physical input layer could not be constructed.
	newPhysicalInput = func() (core.PhysicalInput, error) { return nil, fmt.Errorf("no input backend") }
	defer restore()

	mgr, err := NewManager(testOptions(func(o *Options) {
		o.Backend = "null"
		o.AllowPhysicalInput = true
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	_, err = mgr.ClickAt(context.Background(), 10, 10)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("expected PLATFORM_NOT_SUPPORTED, got %v", err)
	}
}

func TestManagerClickAtValidatesCoordinates(t *testing.T) {
	fp := &fakePhysical{}
	restore := fakeNewPhysicalInput(fp, core.Capabilities{Mouse: true})
	defer restore()
	restoreDisplays := fakeListDisplays([]screen.DisplayInfo{
		{Index: 0, Bounds: core.Bounds{X: 0, Y: 0, W: 1920, H: 1080}, Primary: true},
	}, nil)
	defer restoreDisplays()

	mgr, err := NewManager(testOptions(func(o *Options) {
		o.Backend = "null"
		o.AllowPhysicalInput = true
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if _, err := mgr.ClickAt(context.Background(), 5000, 5000); err == nil {
		t.Fatal("expected an error for out-of-bounds coordinates")
	} else if de, ok := core.AsDesktopError(err); !ok || de.Code != core.ErrInvalidArgs {
		t.Fatalf("expected INVALID_ARGS, got %v", err)
	}
	if len(fp.clicks) != 0 {
		t.Fatalf("expected no physical click for invalid coordinates, got %v", fp.clicks)
	}

	res, err := mgr.ClickAt(context.Background(), 100, 100)
	if err != nil {
		t.Fatalf("ClickAt failed for valid coordinates: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected method=physical, got %q", res.Method)
	}
	if len(fp.clicks) != 1 || fp.clicks[0] != [2]int{100, 100} {
		t.Fatalf("expected one physical click at (100,100), got %v", fp.clicks)
	}
}

func TestManagerClickAtSkipsValidationWhenDisplaysUnknown(t *testing.T) {
	fp := &fakePhysical{}
	restore := fakeNewPhysicalInput(fp, core.Capabilities{Mouse: true})
	defer restore()
	restoreDisplays := fakeListDisplays(nil, fmt.Errorf("cannot enumerate displays"))
	defer restoreDisplays()

	mgr, err := NewManager(testOptions(func(o *Options) {
		o.Backend = "null"
		o.AllowPhysicalInput = true
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := mgr.ClickAt(context.Background(), 999999, 999999); err != nil {
		t.Fatalf("expected validation to be skipped when displays are unknown, got %v", err)
	}
}

func TestManagerSemanticAvailable(t *testing.T) {
	mgr := managerWithBackend(t, newFakeBackend(), testOptions(nil))
	mgr.capabilities = core.Capabilities{Accessibility: true, UIActions: true}
	if !mgr.SemanticAvailable() {
		t.Fatal("expected SemanticAvailable() true when Accessibility+UIActions are both set")
	}
	mgr.capabilities = core.Capabilities{Accessibility: true, UIActions: false}
	if mgr.SemanticAvailable() {
		t.Fatal("expected SemanticAvailable() false when UIActions is missing")
	}
	mgr.capabilities = core.Capabilities{}
	if mgr.SemanticAvailable() {
		t.Fatal("expected SemanticAvailable() false for an all-false Capabilities")
	}
}

// eventSubscribingBackend wraps fakeBackend and additionally implements
// events.Subscriber, so Manager.Wait's type-assertion detection of a live
// event path can be exercised end to end.
type eventSubscribingBackend struct {
	*fakeBackend
	ch             chan events.Event
	subscribeCalls int
}

func (b *eventSubscribingBackend) Subscribe(ctx context.Context, scope core.Scope) (<-chan events.Event, func(), error) {
	b.subscribeCalls++
	return b.ch, func() {}, nil
}

func TestManagerWaitUsesLiveSubscriptionWhenBackendSupportsIt(t *testing.T) {
	fb := newFakeBackend()
	fb.windows = []core.WindowInfo{{ID: "w1", AppID: "app1"}}
	target := elem(core.RoleButton, "OK")
	target.AppID = "app1"
	sub := &eventSubscribingBackend{fakeBackend: fb, ch: make(chan events.Event, 1)}

	globalRegistry.Register("fake-event-backend", func() (core.Backend, error) { return sub, nil })
	mgr, err := NewManager(testOptions(func(o *Options) { o.Backend = "fake-event-backend" }))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	fb.findErr = core.NewElementNotFoundError("not yet")
	go func() {
		time.Sleep(30 * time.Millisecond)
		fb.findResult = []*core.Element{target}
		fb.findErr = nil
		sub.ch <- events.Event{Kind: events.KindCreated}
	}()

	el, err := mgr.Wait(context.Background(), core.Scope{AppID: "app1"}, `button[name="OK"]`, core.ConditionExists, 2*time.Second)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if el == nil || el.Name != "OK" {
		t.Fatalf("unexpected element: %+v", el)
	}
	if sub.subscribeCalls != 1 {
		t.Fatalf("expected Subscribe to be called once, got %d", sub.subscribeCalls)
	}
}

// ---- Block R: per-scope backend routing ----

// newFakeCdpBackend builds a fakeBackend impersonating the registered "cdp"
// backend name, for routing tests that don't want to depend on a real
// browser session.
func newFakeCdpBackend() *fakeBackend {
	b := newFakeBackend()
	b.name = "cdp"
	return b
}

func TestManagerRoutesBrowserScopeToCdp(t *testing.T) {
	os := newFakeBackend()
	os.windows = []core.WindowInfo{{ID: "w-native", AppID: "app1", Title: "Native"}}

	cdp := newFakeCdpBackend()
	cdp.windows = []core.WindowInfo{{ID: "w-cdp", AppID: browserpkg.AppID, Title: "Page"}}
	page := elem(core.RoleGroup, "PageRoot")
	cdp.rootByWin["w-cdp"] = nil // window root itself has no children in this test

	mgr := managerWithBackends(t, os, cdp, testOptions(nil))
	_ = page

	snap, err := mgr.Observe(context.Background(), core.Scope{AppID: browserpkg.AppID}, 1)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}
	if snap.Backend != "cdp" {
		t.Fatalf("expected snapshot routed to cdp, got backend %q", snap.Backend)
	}
	if os.windowsCalls != 0 {
		t.Fatalf("expected the OS backend to never be consulted for a browser-app scope, got %d Windows calls", os.windowsCalls)
	}
	if cdp.windowsCalls == 0 {
		t.Fatal("expected the cdp backend to be consulted for a browser-app scope")
	}
	if snap.Root == nil || snap.Root.Backend != "cdp" {
		t.Fatalf("expected root element tagged backend=cdp, got %+v", snap.Root)
	}
}

func TestManagerRoutesNativeScopeToOSBackend(t *testing.T) {
	os := newFakeBackend()
	os.windows = []core.WindowInfo{{ID: "w-native", AppID: "app1", Title: "Native"}}
	os.rootByWin["w-native"] = nil

	cdp := newFakeCdpBackend()
	cdp.windows = []core.WindowInfo{{ID: "w-cdp", AppID: browserpkg.AppID}}

	mgr := managerWithBackends(t, os, cdp, testOptions(nil))

	snap, err := mgr.Observe(context.Background(), core.Scope{AppID: "app1"}, 1)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}
	if snap.Backend != "fake" {
		t.Fatalf("expected snapshot routed to the OS backend, got %q", snap.Backend)
	}
	if cdp.windowsCalls != 0 {
		t.Fatalf("expected the cdp backend to never be consulted for a native-app scope, got %d Windows calls", cdp.windowsCalls)
	}
}

// TestManagerPinnedBackendDisablesRouting verifies R1's "hard pin" contract:
// with a cdpBackend held (routing would otherwise be possible), a Manager
// built with a pinned OS backend (mirroring DesktopBackend="atspi" etc.)
// must still send a browser-app-scoped Observe to the OS backend, never to
// cdp.
func TestManagerPinnedBackendDisablesRouting(t *testing.T) {
	os := newFakeBackend()
	os.windows = []core.WindowInfo{{ID: "w1", AppID: browserpkg.AppID, Title: "whatever the OS backend calls it"}}
	os.rootByWin["w1"] = nil

	mgr := managerWithBackend(t, os, testOptions(nil)) // cdpBackend nil -> pinned
	if mgr.CdpAvailable() {
		t.Fatal("expected CdpAvailable() false for a pinned manager")
	}

	snap, err := mgr.Observe(context.Background(), core.Scope{AppID: browserpkg.AppID}, 1)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}
	if snap.Backend != "fake" {
		t.Fatalf("expected a pinned manager to route even a browser-app scope to the OS backend, got %q", snap.Backend)
	}
}

// TestManagerRefProvenanceHonoredAcrossOperations verifies R1's ref
// provenance requirement: an element returned by a cdp-routed Find keeps
// its Backend="cdp" tag, and a later Click on that ref routes back to cdp
// even though nothing about the Click call itself names a scope.
func TestManagerRefProvenanceHonoredAcrossOperations(t *testing.T) {
	os := newFakeBackend()
	cdp := newFakeCdpBackend()

	cdpTarget := elem(core.RoleButton, "Submit")
	cdpTarget.AppID = browserpkg.AppID
	cdpTarget.Bounds = core.Bounds{X: 1, Y: 1, W: 10, H: 10}
	cdp.findResult = []*core.Element{cdpTarget}

	osTarget := elem(core.RoleButton, "Save")
	osTarget.AppID = "app1"
	osTarget.Bounds = core.Bounds{X: 1, Y: 1, W: 10, H: 10}
	os.findResult = []*core.Element{osTarget}

	mgr := managerWithBackends(t, os, cdp, testOptions(nil))

	// Find in the browser scope -> element tagged backend=cdp.
	elements, _, err := mgr.Find(context.Background(), core.Scope{AppID: browserpkg.AppID}, `button[name="Submit"]`, 0)
	if err != nil {
		t.Fatalf("Find (cdp) failed: %v", err)
	}
	cdpRef := elements[0].ID
	if elements[0].Backend != "cdp" {
		t.Fatalf("expected element backend=cdp, got %q", elements[0].Backend)
	}

	// Find in a native scope -> element tagged backend=<os name>.
	elements, _, err = mgr.Find(context.Background(), core.Scope{AppID: "app1"}, `button[name="Save"]`, 0)
	if err != nil {
		t.Fatalf("Find (os) failed: %v", err)
	}
	osRef := elements[0].ID
	if elements[0].Backend != "fake" {
		t.Fatalf("expected element backend=fake, got %q", elements[0].Backend)
	}

	// Click on the cdp-tagged ref must reach the cdp backend, not the OS one.
	if _, err := mgr.Click(context.Background(), cdpRef); err != nil {
		t.Fatalf("Click on cdp ref failed: %v", err)
	}
	if cdp.performCalls != 1 || os.performCalls != 0 {
		t.Fatalf("expected exactly one cdp Perform call and zero OS Perform calls, got cdp=%d os=%d", cdp.performCalls, os.performCalls)
	}

	// Click on the OS-tagged ref must reach the OS backend, not cdp.
	if _, err := mgr.Click(context.Background(), osRef); err != nil {
		t.Fatalf("Click on os ref failed: %v", err)
	}
	if cdp.performCalls != 1 || os.performCalls != 1 {
		t.Fatalf("expected the second click to reach only the OS backend, got cdp=%d os=%d", cdp.performCalls, os.performCalls)
	}
}

// TestManagerCdpNeverConsultedWithoutSession verifies the "cdp stays
// inert" property survives routing: when the cdp backend reports it has no
// active session (Available/Apps erroring, mirroring
// platform/browser.CdpBackend with nothing registered), Manager.Apps still
// succeeds using just the OS backend's apps -- the cdp error is absorbed,
// never surfaced as a failure of the whole call, and nothing about
// resolving/holding a cdpBackend reference ever "probes" beyond a cheap
// Apps/Windows call this test already models honestly.
func TestManagerCdpNeverConsultedWithoutSession(t *testing.T) {
	os := newFakeBackend()
	os.apps = []core.AppInfo{{ID: "app1", Name: "Native App"}}

	cdp := newFakeCdpBackend()
	cdp.availableErr = core.NewAppNotFoundError("no active browser session")
	// Apps() on fakeBackend does not consult availableErr today (mirrors
	// core.Backend.Apps semantics, not Available), so simulate "no
	// session" the way CdpBackend.Apps really behaves: return the same
	// error from Apps itself.
	cdpAppsErrBackend := &fakeAppsErrBackend{fakeBackend: cdp, appsErr: core.NewAppNotFoundError("no active browser session")}

	mgr := managerWithBackends(t, os, cdpAppsErrBackend, testOptions(nil))

	apps, err := mgr.Apps(context.Background())
	if err != nil {
		t.Fatalf("expected Apps to succeed from the OS backend alone, got error: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != "app1" {
		t.Fatalf("expected only the OS backend's app, got %+v", apps)
	}
}

// fakeAppsErrBackend wraps a fakeBackend to force Apps() to fail
// regardless of the embedded fakeBackend.apps field, modeling
// CdpBackend.Apps' errNoActiveSession behavior precisely.
type fakeAppsErrBackend struct {
	*fakeBackend
	appsErr error
}

func (b *fakeAppsErrBackend) Apps(ctx context.Context) ([]core.AppInfo, error) {
	return nil, b.appsErr
}

func TestManagerCapabilitiesForScopeMatchesServingBackend(t *testing.T) {
	os := newFakeBackend()
	os.caps = core.Capabilities{Accessibility: true}

	cdp := newFakeCdpBackend()
	cdp.caps = core.Capabilities{Accessibility: true, UIActions: true, Events: true}

	mgr := managerWithBackends(t, os, cdp, testOptions(nil))
	mgr.capabilities = os.caps // mirror what NewManager would have cached

	nativeCaps := mgr.CapabilitiesFor(context.Background(), core.Scope{AppID: "app1"})
	if nativeCaps.UIActions || nativeCaps.Events {
		t.Fatalf("expected native scope capabilities to never claim cdp-only capabilities, got %+v", nativeCaps)
	}

	browserCaps := mgr.CapabilitiesFor(context.Background(), core.Scope{AppID: browserpkg.AppID})
	if !browserCaps.UIActions || !browserCaps.Events {
		t.Fatalf("expected browser scope capabilities to reflect the live cdp session, got %+v", browserCaps)
	}
}
