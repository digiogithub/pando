package orchestrator

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

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

func TestBlackboardCompactionBoundsLogButKeepsWinners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blackboard.json")
	// Cap at 3 entries per swarm; TTL off so only the size axis is exercised.
	bb, err := NewBlackboard(path, WithBlackboardLimits(3, 0))
	if err != nil {
		t.Fatalf("NewBlackboard: %v", err)
	}
	if bb.ttl != DefaultBlackboardTTL {
		t.Fatalf("non-positive ttl must leave the default, got %v", bb.ttl)
	}

	// "owner" is written once, early, then never again: its winner falls outside
	// the newest-3 window and must survive compaction anyway.
	post := func(key, val string) {
		if err := bb.Post("s1", BlackboardEntry{Key: key, Value: json.RawMessage(val), Author: "a"}); err != nil {
			t.Fatalf("Post(%s): %v", key, err)
		}
	}
	post("owner", `"worker-a"`) // winner for "owner", will age out of the window
	post("step", `"1"`)
	post("step", `"2"`)
	post("step", `"3"`)
	post("step", `"4"`) // 5 posts, cap is 3

	log := bb.List("s1")
	// Newest 3 posts, plus the out-of-window "owner" winner = 4 retained.
	if len(log) != 4 {
		t.Fatalf("compaction want 4 retained (3 newest + 1 orphan winner), got %d: %+v", len(log), log)
	}

	// The merged view is unchanged by GC: both keys present, latest values.
	latest := bb.Latest("s1")
	if len(latest) != 2 {
		t.Fatalf("compaction must not drop a key, got %d", len(latest))
	}
	got := map[string]string{}
	for _, e := range latest {
		got[e.Key] = string(e.Value)
	}
	if got["owner"] != `"worker-a"` {
		t.Errorf("orphan winner lost: owner=%q", got["owner"])
	}
	if got["step"] != `"4"` {
		t.Errorf("latest step lost: step=%q", got["step"])
	}

	// Bound survives a reopen (compacted state was persisted).
	reopened, err := NewBlackboard(path, WithBlackboardLimits(3, 0))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := len(reopened.List("s1")); got != 4 {
		t.Fatalf("compacted log must persist, got %d", got)
	}
}

func TestBlackboardTTLPurgesStaleSwarms(t *testing.T) {
	bb, err := NewBlackboard("", WithBlackboardLimits(0, time.Hour))
	if err != nil {
		t.Fatalf("NewBlackboard: %v", err)
	}
	if bb.maxEntriesPerSwarm != DefaultBlackboardMaxEntriesPerSwarm {
		t.Fatalf("non-positive max must leave the default, got %d", bb.maxEntriesPerSwarm)
	}

	stale := time.Now().Add(-2 * time.Hour)
	bb.entries["old"] = []BlackboardEntry{{Key: "k", Value: json.RawMessage(`"v"`), CreatedAt: stale}}

	// A fresh Post on another swarm triggers the global TTL sweep.
	if err := bb.Post("fresh", BlackboardEntry{Key: "k", Value: json.RawMessage(`"v"`)}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if got := len(bb.List("old")); got != 0 {
		t.Errorf("stale swarm must be purged, got %d entries", got)
	}
	if got := len(bb.List("fresh")); got != 1 {
		t.Errorf("fresh swarm must survive, got %d", got)
	}
}

func TestBlackboardPruneExpiredOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blackboard.json")
	bb, _ := NewBlackboard(path, WithBlackboardLimits(0, time.Hour))
	// Seed one fresh and one stale swarm, then persist.
	bb.entries["stale"] = []BlackboardEntry{{Key: "k", CreatedAt: time.Now().Add(-3 * time.Hour)}}
	if err := bb.Post("live", BlackboardEntry{Key: "k", Value: json.RawMessage(`"v"`)}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	// Post already swept "stale"; write it straight to the file to simulate a
	// swarm that expired while pando was down, then reopen.
	bb.entries["stale"] = []BlackboardEntry{{Key: "k", CreatedAt: time.Now().Add(-3 * time.Hour)}}
	bb.mu.Lock()
	_ = bb.saveLocked()
	bb.mu.Unlock()

	reopened, err := NewBlackboard(path, WithBlackboardLimits(0, time.Hour))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := len(reopened.List("stale")); got != 0 {
		t.Errorf("expired swarm must be pruned on open, got %d", got)
	}
	if got := len(reopened.List("live")); got != 1 {
		t.Errorf("live swarm must survive open-prune, got %d", got)
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
