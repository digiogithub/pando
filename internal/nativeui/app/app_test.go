package app

import (
	"context"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBatchCoalescesPushes(t *testing.T) {
	q := &QueueDispatcher{}
	var got [][]int
	b := NewBatch(q.Update, func(items []int) { got = append(got, items) })
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() { defer wg.Done(); b.Push(i) }()
	}
	wg.Wait()
	q.Drain()
	total := 0
	for _, g := range got {
		total += len(g)
	}
	if total != 100 || len(got) != 1 {
		t.Errorf("batches %d, items %d; want one batch of 100", len(got), total)
	}
}

type fakeView struct{ id string }

func (v *fakeView) Build(c *ui.Context, r *ui.Route) { ui.Text(c, "view "+v.id) }

func TestRegistryAndRouting(t *testing.T) {
	// Every Web UI area has an entry (placeholder until its package lands).
	for _, id := range []string{"chat", "simple-chat", "projects", "editor", "terminal", "orchestrator", "design",
		"logs", "agentvcs", "evaluator", "instances", "extensions", "settings", "setup"} {
		if _, ok := Lookup(id); !ok {
			t.Errorf("area %q not registered", id)
		}
	}
	// A real registration replaces the placeholder; a later placeholder
	// does not replace the real one.
	Register(Area{ID: "logs", Path: "/logs", TitleKey: "nav.logs", Section: SectionTools, New: func(*Context) View { return &fakeView{"logs"} }})
	Register(Area{ID: "logs", Path: "/logs", Placeholder: true, New: func(*Context) View { return nil }})
	if ar, _ := Lookup("logs"); ar.Placeholder {
		t.Fatal("placeholder replaced a real area")
	}

	a := NewContext(context.Background(), nil)
	root := NewRoot(a)
	tt := ui.NewTester(root.View, 1100, 700)
	if !tt.HasText("Code Editor") || !tt.HasText("Settings") {
		t.Errorf("sidebar incomplete: %q", tt.Texts())
	}
	if err := tt.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("view logs") || a.Router.Path() != "/logs" {
		t.Errorf("routing to /logs failed: path %q texts %q", a.Router.Path(), tt.Texts())
	}
	if err := tt.Click("Terminal"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("This area is not available in the native desktop yet. Use the browser UI (pando app) meanwhile.") {
		t.Errorf("placeholder not shown: %q", tt.Texts())
	}
}
