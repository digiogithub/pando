package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/target"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

func elementWithHandle(targetID target.ID, axNodeID accessibility.NodeID, backendID cdp.BackendNodeID) *core.Element {
	return &core.Element{
		Role: core.RoleButton,
		Name: "Save",
		Native: core.NativeData{
			Data: map[string]any{
				nativeTargetIDKey:  string(targetID),
				nativeAXNodeIDKey:  string(axNodeID),
				nativeBackendIDKey: int64(backendID),
			},
		},
	}
}

func TestAvailableNoSessionRegistered(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	caps, err := b.Available(context.Background())
	if err == nil {
		t.Fatal("expected an error when no browser session is registered")
	}
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrAppNotFound {
		t.Fatalf("err = %v, want APP_NOT_FOUND", err)
	}
	if caps.Accessibility || caps.UIActions || caps.UIInspection {
		t.Fatalf("caps = %+v, want all false", caps)
	}
}

func TestAvailableSessionRegisteredButUnreachable(t *testing.T) {
	f := newFakeConn()
	f.failVersion = errors.New("simulated: connection reset")
	b := newBackendWithConn(f)
	withActiveSession(t)

	caps, err := b.Available(context.Background())
	if err == nil {
		t.Fatal("expected an error when the registered session is unreachable")
	}
	if caps.Accessibility {
		t.Fatalf("caps = %+v, want all false", caps)
	}
}

func TestAvailableSessionReachable(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	withActiveSession(t)

	caps, err := b.Available(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !caps.Accessibility || !caps.UIActions || !caps.UIInspection {
		t.Fatalf("caps = %+v, want accessibility/uiActions/uiInspection true", caps)
	}
	if caps.Mouse || caps.Keyboard || caps.Screenshot {
		t.Fatalf("caps = %+v, want mouse/keyboard/screenshot false (Phase 3's job)", caps)
	}
}

func TestApps(t *testing.T) {
	f := newFakeConn()
	f.version = "Chrome/123.0.0.0"
	f.targets = []*target.Info{
		{TargetID: "T1", Type: "page", Title: "One"},
		{TargetID: "T2", Type: "page", Title: "Two"},
		{TargetID: "SW1", Type: "service_worker"},
	}
	b := newBackendWithConn(f)
	withActiveSession(t)

	apps, err := b.Apps(context.Background())
	if err != nil {
		t.Fatalf("Apps: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != appID || apps[0].Windows != 2 {
		t.Fatalf("apps = %+v", apps)
	}
	if apps[0].Name != "Chrome/123.0.0.0" {
		t.Fatalf("name = %q", apps[0].Name)
	}
}

func TestWindowsFiltersPageTargets(t *testing.T) {
	f := newFakeConn()
	f.targets = []*target.Info{
		{TargetID: "T1", Type: "page", Title: "One", Attached: true},
		{TargetID: "T2", Type: "page", URL: "https://example.com"},
		{TargetID: "IF1", Type: "iframe"},
	}
	b := newBackendWithConn(f)
	withActiveSession(t)

	wins, err := b.Windows(context.Background(), "")
	if err != nil {
		t.Fatalf("Windows: %v", err)
	}
	if len(wins) != 2 {
		t.Fatalf("wins = %+v", wins)
	}
	if wins[0].ID != "T1" || !wins[0].Focused {
		t.Fatalf("wins[0] = %+v", wins[0])
	}
	if wins[1].Title != "https://example.com" {
		t.Fatalf("expected title fallback to URL, got %q", wins[1].Title)
	}
}

func TestWindowsUnknownAppID(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	withActiveSession(t)

	_, err := b.Windows(context.Background(), "not-the-browser")
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrAppNotFound {
		t.Fatalf("err = %v, want APP_NOT_FOUND", err)
	}
}

func TestChildrenFromSyntheticRoot(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	b := newBackendWithConn(f)
	withActiveSession(t)

	root := &core.Element{WindowID: "T1"} // Manager's synthetic root, no Native.
	children, err := b.Children(context.Background(), root)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 2 { // group + textField
		t.Fatalf("children = %+v", children)
	}
}

func TestPerformFocus(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "3", 30)

	if err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionFocus}); err != nil {
		t.Fatalf("Perform focus: %v", err)
	}
	if len(f.focused) != 1 || f.focused[0] != 30 {
		t.Fatalf("focused = %v", f.focused)
	}
}

func TestPerformInvokeClicks(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "3", 30)

	if err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionInvoke}); err != nil {
		t.Fatalf("Perform invoke: %v", err)
	}
	if len(f.clicked) != 1 || f.clicked[0] != 30 {
		t.Fatalf("clicked = %v", f.clicked)
	}
}

func TestPerformSetValueAndType(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "5", 50)

	if err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionSetValue, Text: "hello"}); err != nil {
		t.Fatalf("Perform setvalue: %v", err)
	}
	if f.setValues[50] != "hello" {
		t.Fatalf("setValues = %v", f.setValues)
	}

	if err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionType, Text: "world"}); err != nil {
		t.Fatalf("Perform type: %v", err)
	}
	if f.insertedText[50] != "world" {
		t.Fatalf("insertedText = %v", f.insertedText)
	}
}

func TestPerformScroll(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "5", 50)

	if err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionScroll, Amount: 100}); err != nil {
		t.Fatalf("Perform scroll: %v", err)
	}
	if len(f.scrolled) != 1 || f.scrolled[0] != 50 {
		t.Fatalf("scrolled = %v", f.scrolled)
	}
}

func TestPerformUnsupportedActionReturnsPlatformNotSupported(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "5", 50)

	err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionPress, Key: "Enter"})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("err = %v, want PLATFORM_NOT_SUPPORTED", err)
	}
}

func TestPerformBackendFailureMapsToActionFailed(t *testing.T) {
	f := newFakeConn()
	f.failAction[30] = errors.New("simulated cdp failure")
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "3", 30)

	err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionInvoke})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrActionFailed {
		t.Fatalf("err = %v, want ACTION_FAILED", err)
	}
}

func TestPerformNoBackendNodeID(t *testing.T) {
	f := newFakeConn()
	b := newBackendWithConn(f)
	el := &core.Element{
		Native: core.NativeData{Data: map[string]any{
			nativeTargetIDKey: "T1",
			nativeAXNodeIDKey: "3",
		}},
	}
	err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionInvoke})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrActionFailed {
		t.Fatalf("err = %v, want ACTION_FAILED", err)
	}
}

func TestPerformPopulatesBoundsLazily(t *testing.T) {
	f := newFakeConn()
	f.setBoxModel(30, &dom.BoxModel{Content: dom.Quad{10, 20, 110, 20, 110, 70, 10, 70}})
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "3", 30)
	if !el.Bounds.Empty() {
		t.Fatal("precondition: element should start with empty bounds")
	}

	if err := b.Perform(context.Background(), el, core.Action{Kind: core.ActionFocus}); err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if el.Bounds.Empty() {
		t.Fatal("expected Perform to opportunistically fill in Bounds via BoxModel")
	}
	if el.Bounds.X != 10 || el.Bounds.Y != 20 || el.Bounds.W != 100 || el.Bounds.H != 50 {
		t.Fatalf("bounds = %+v", el.Bounds)
	}
}

func TestPropertiesReturnsBoundsOnRequest(t *testing.T) {
	f := newFakeConn()
	f.setBoxModel(30, &dom.BoxModel{Content: dom.Quad{0, 0, 10, 0, 10, 10, 0, 10}})
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "3", 30)

	props, err := b.Properties(context.Background(), el, []string{"bounds"})
	if err != nil {
		t.Fatalf("Properties: %v", err)
	}
	bounds, ok := props["bounds"].(core.Bounds)
	if !ok {
		t.Fatalf("expected bounds in props, got %+v", props)
	}
	if bounds.W != 10 || bounds.H != 10 {
		t.Fatalf("bounds = %+v", bounds)
	}
}

func TestPropertiesDefaultIncludesBounds(t *testing.T) {
	f := newFakeConn()
	f.setBoxModel(30, &dom.BoxModel{Content: dom.Quad{0, 0, 10, 0, 10, 10, 0, 10}})
	b := newBackendWithConn(f)
	el := elementWithHandle("T1", "3", 30)

	props, err := b.Properties(context.Background(), el, nil)
	if err != nil {
		t.Fatalf("Properties: %v", err)
	}
	if _, ok := props["bounds"]; !ok {
		// props==nil is documented as the "cheap default" path, which for
		// this backend still includes bounds (single extra round trip,
		// same as AT-SPI's default GetExtents call) -- see Properties'
		// doc comment. Assert it is present and correct instead.
		t.Fatalf("expected bounds present on default Properties call, got %+v", props)
	}
}

func TestBoundsFromBoxModelFallsBackToBorder(t *testing.T) {
	m := &dom.BoxModel{Border: dom.Quad{5, 5, 15, 5, 15, 25, 5, 25}}
	b := boundsFromBoxModel(m)
	if b.X != 5 || b.Y != 5 || b.W != 10 || b.H != 20 {
		t.Fatalf("bounds = %+v", b)
	}
}

func TestBoundsFromBoxModelNil(t *testing.T) {
	if b := boundsFromBoxModel(nil); !b.Empty() {
		t.Fatalf("expected empty bounds for nil model, got %+v", b)
	}
}
