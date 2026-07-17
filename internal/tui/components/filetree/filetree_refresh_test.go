package filetree

import (
	"testing"
)

func TestAutoRefreshTickReloadsOnlyWhenRendered(t *testing.T) {
	tree := New(t.TempDir())
	if cmd := tree.Init(); cmd == nil {
		t.Fatal("Init should start the auto-refresh tick chain")
	}

	t.Run("hidden tree reschedules without reloading", func(t *testing.T) {
		tree.rendered = false
		before := tree.tickSeq
		_, cmd := tree.Update(autoRefreshTickMsg{seq: tree.tickSeq})
		if cmd == nil {
			t.Fatal("tick must always reschedule itself")
		}
		if tree.tickSeq == before {
			t.Error("tick chain was not rescheduled")
		}
	})

	t.Run("visible tree clears the rendered flag and reloads", func(t *testing.T) {
		tree.rendered = true
		_, cmd := tree.Update(autoRefreshTickMsg{seq: tree.tickSeq})
		if cmd == nil {
			t.Fatal("expected a reload batch")
		}
		if tree.rendered {
			t.Error("rendered should be cleared until View marks the tree on screen again")
		}
	})

	t.Run("stale tick is dropped", func(t *testing.T) {
		before := tree.tickSeq
		_, cmd := tree.Update(autoRefreshTickMsg{seq: before - 1})
		if cmd != nil {
			t.Error("a stale tick must not fork a second tick chain")
		}
		if tree.tickSeq != before {
			t.Error("a stale tick must not reschedule")
		}
	})

	t.Run("filter input suppresses the reload", func(t *testing.T) {
		tree.rendered = true
		tree.filterMode = true
		defer func() { tree.filterMode = false }()
		_, cmd := tree.Update(autoRefreshTickMsg{seq: tree.tickSeq})
		if cmd == nil {
			t.Fatal("tick must still reschedule while filtering")
		}
		if !tree.rendered {
			t.Error("rendered should be untouched when the reload is skipped")
		}
	})
}
