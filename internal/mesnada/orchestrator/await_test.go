package orchestrator

import (
	"sync"
	"testing"
	"time"
)

func TestAwaitIntentSetGetClear(t *testing.T) {
	o := &Orchestrator{}

	if _, ok := o.AwaitIntentFor("sess-1"); ok {
		t.Fatal("expected no intent before set")
	}

	intent := &AwaitIntent{
		SessionID: "sess-1",
		TaskIDs:   []string{"task-a", "task-b"},
		Policy:    AwaitPolicyAll,
		Deadline:  time.Now().Add(time.Minute),
		CreatedAt: time.Now(),
	}
	o.SetAwaitIntent(intent)

	got, ok := o.AwaitIntentFor("sess-1")
	if !ok {
		t.Fatal("expected intent after set")
	}
	if got.Policy != AwaitPolicyAll || len(got.TaskIDs) != 2 {
		t.Fatalf("unexpected intent: %#v", got)
	}

	// Setting again replaces the existing intent.
	o.SetAwaitIntent(&AwaitIntent{SessionID: "sess-1", Policy: AwaitPolicyAny, TaskIDs: []string{"task-c"}})
	got, _ = o.AwaitIntentFor("sess-1")
	if got.Policy != AwaitPolicyAny || len(got.TaskIDs) != 1 {
		t.Fatalf("expected replacement intent, got %#v", got)
	}

	o.ClearAwaitIntent("sess-1")
	if _, ok := o.AwaitIntentFor("sess-1"); ok {
		t.Fatal("expected no intent after clear")
	}

	// Nil / empty-session sets are ignored.
	o.SetAwaitIntent(nil)
	o.SetAwaitIntent(&AwaitIntent{SessionID: ""})
	if _, ok := o.AwaitIntentFor(""); ok {
		t.Fatal("expected empty-session intent to be ignored")
	}
}

func TestAwaitIntentConcurrent(t *testing.T) {
	o := &Orchestrator{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sid := "sess"
			o.SetAwaitIntent(&AwaitIntent{SessionID: sid, Policy: AwaitPolicyAll})
			_, _ = o.AwaitIntentFor(sid)
			o.ClearAwaitIntent(sid)
		}(i)
	}
	wg.Wait()
}
