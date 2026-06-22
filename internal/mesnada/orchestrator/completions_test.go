package orchestrator

import (
	"testing"
	"time"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func TestSubscribeCompletionsReceivesTask(t *testing.T) {
	o := &Orchestrator{}
	ch, unsubscribe := o.SubscribeCompletions()
	defer unsubscribe()

	task := &models.Task{ID: "t1", Status: models.TaskStatusCompleted}
	o.broadcastCompletion(task)

	select {
	case got := <-ch:
		if got == nil || got.ID != "t1" {
			t.Fatalf("expected task t1, got %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion broadcast")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	o := &Orchestrator{}
	ch, unsubscribe := o.SubscribeCompletions()
	unsubscribe()

	// After unsubscribe the channel is closed; a broadcast must not panic and the
	// channel must not deliver a new task.
	o.broadcastCompletion(&models.Task{ID: "t2", Status: models.TaskStatusCompleted})

	select {
	case got, ok := <-ch:
		if ok {
			t.Fatalf("expected no delivery after unsubscribe, got %#v", got)
		}
		// closed channel -> ok=false is acceptable.
	case <-time.After(200 * time.Millisecond):
		// no delivery is also acceptable.
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	o := &Orchestrator{}
	_, unsubscribe := o.SubscribeCompletions()
	unsubscribe()
	unsubscribe() // must not panic (double close guarded by sync.Once)
}

func TestBroadcastDropsWhenBufferFull(t *testing.T) {
	o := &Orchestrator{}
	ch, unsubscribe := o.SubscribeCompletions()
	defer unsubscribe()

	// Fill the buffer (cap 16) plus extras; broadcast must never block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			o.broadcastCompletion(&models.Task{ID: "x", Status: models.TaskStatusCompleted})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcastCompletion blocked when buffer full")
	}
	_ = ch
}
