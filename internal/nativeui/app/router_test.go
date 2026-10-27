package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
)

// listView is an area view holding one ListState, as the chat and agentvcs
// views do: building it twice in one frame panics in MyGo.
type listView struct {
	list   ui.ListState
	builds int
}

func (v *listView) Build(c *ui.Context, r *ui.Route) {
	v.builds++
	label := "list " + r.Path()
	ui.List(c, &v.list, 3, func(i int) { ui.Text(c, label) }).Grow(1)
}

// TestSameAreaRouteTransition navigates between two routes of the same area
// (deeper and back up, which slide in MyGo and build the page going away
// too): the area view must be built once per frame.
func TestSameAreaRouteTransition(t *testing.T) {
	v := &listView{}
	Register(Area{ID: "zz-listarea", Path: "/zz", Patterns: []string{"/zz", "/zz/{id}"}, TitleKey: "nav.logs",
		Section: SectionTools, Order: 99, New: func(*Context) View { return v }})
	a := NewContext(context.Background(), nil)
	a.Router.Transition = ui.TransitionAuto // slides: the leaving page is built too
	a.Router.Replace("/zz")
	root := NewRoot(a)
	tt := ui.NewTester(root.View, 1100, 700)
	for _, p := range []string{"/zz/1", "/zz/2", "/zz", "/zz/3"} {
		a.Router.Push(p)
		for range 5 { // frames of the transition
			tt.Frame()
		}
		if !tt.HasText("list " + p) {
			t.Fatalf("route %s not shown: %q", p, tt.Texts())
		}
	}
	a.Router.Back()
	tt.Frame()
	if !tt.HasText("list /zz") {
		t.Errorf("back not shown: %q", tt.Texts())
	}
	// The app's routers show pages at once (a single page is built).
	if NewContext(context.Background(), nil).Router.Transition != ui.TransitionNone {
		t.Error("app router animates page changes")
	}
}
