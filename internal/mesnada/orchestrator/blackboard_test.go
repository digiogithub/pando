package orchestrator

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func TestBlackboardPostAndLatestLastWriteWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blackboard.json")
	bb, err := NewBlackboard(path)
	if err != nil {
		t.Fatalf("NewBlackboard: %v", err)
	}

	mustPost := func(swarm, key, val, author string) {
		if err := bb.Post(swarm, BlackboardEntry{Key: key, Value: json.RawMessage(val), Author: author}); err != nil {
			t.Fatalf("Post(%s,%s): %v", swarm, key, err)
		}
	}

	mustPost("s1", "api", `"v1"`, "worker-a")
	mustPost("s1", "owner", `"worker-b"`, "worker-b")
	mustPost("s1", "api", `"v2"`, "worker-c") // overwrites key "api"
	mustPost("s2", "api", `"other"`, "worker-d")

	latest := bb.Latest("s1")
	if len(latest) != 2 {
		t.Fatalf("want 2 merged keys, got %d", len(latest))
	}
	// Sorted by key: "api" then "owner".
	if latest[0].Key != "api" || string(latest[0].Value) != `"v2"` || latest[0].Author != "worker-c" {
		t.Fatalf("last-write-wins failed: %+v", latest[0])
	}
	if latest[1].Key != "owner" {
		t.Fatalf("want owner, got %s", latest[1].Key)
	}

	// Full append log preserves history (3 posts on s1).
	if got := len(bb.List("s1")); got != 3 {
		t.Fatalf("append log want 3, got %d", got)
	}
	// Swarms are isolated.
	if got := len(bb.Latest("s2")); got != 1 {
		t.Fatalf("s2 isolation: want 1, got %d", got)
	}
}

func TestBlackboardPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blackboard.json")
	bb, _ := NewBlackboard(path)
	if err := bb.Post("s1", BlackboardEntry{Key: "k", Value: json.RawMessage(`{"n":1}`), Author: "a"}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	reopened, err := NewBlackboard(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	latest := reopened.Latest("s1")
	if len(latest) != 1 || string(latest[0].Value) != `{"n":1}` {
		t.Fatalf("persistence failed: %+v", latest)
	}
}

func TestBlackboardPostValidation(t *testing.T) {
	bb, _ := NewBlackboard("")
	if err := bb.Post("", BlackboardEntry{Key: "k"}); err == nil {
		t.Fatal("expected error on empty swarm_id")
	}
	if err := bb.Post("s1", BlackboardEntry{Key: ""}); err == nil {
		t.Fatal("expected error on empty key")
	}
}

func TestSwarmKeyForTaskPrecedence(t *testing.T) {
	o := &Orchestrator{}
	cases := []struct {
		task *models.Task
		want string
	}{
		{&models.Task{ParentSessionID: "sess", ParentTaskID: "pt", CorrelationID: "c"}, "sess"},
		{&models.Task{ParentTaskID: "pt", CorrelationID: "c"}, "pt"},
		{&models.Task{CorrelationID: "c"}, "c"},
		{&models.Task{}, ""},
	}
	for i, tc := range cases {
		if got := o.swarmKeyForTask(tc.task); got != tc.want {
			t.Fatalf("case %d: want %q, got %q", i, tc.want, got)
		}
	}
}
