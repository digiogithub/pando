package core

import (
	"testing"
	"time"
)

func buildTestSnapshot(id string) *Snapshot {
	root := &Element{ID: FormatElementRef(id, "e1"), Role: RoleWindow, Name: "Settings"}
	child := &Element{ID: FormatElementRef(id, "e2"), Role: RoleButton, Name: "Save", ParentID: root.ID}
	root.ChildIDs = []ElementRef{child.ID}
	return &Snapshot{
		Backend: "null",
		AppID:   "TestApp",
		Root:    root,
		Elements: map[string]*Element{
			"e1": root,
			"e2": child,
		},
	}
}

func TestSnapshotStore_PutGet(t *testing.T) {
	store := NewSnapshotStore(time.Minute, 10)
	snap := store.Put(buildTestSnapshot(""))
	if snap.ID == "" {
		t.Fatalf("expected Put to assign a snapshot id")
	}
	got, err := store.Get(snap.ID)
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got != snap {
		t.Fatalf("Get() returned a different snapshot instance")
	}
}

func TestSnapshotStore_Get_NotFound(t *testing.T) {
	store := NewSnapshotStore(time.Minute, 10)
	_, err := store.Get("does-not-exist")
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrSnapshotNotFound {
		t.Fatalf("expected SNAPSHOT_NOT_FOUND, got %v", err)
	}
}

func TestSnapshotStore_TTLExpiry(t *testing.T) {
	store := NewSnapshotStore(10*time.Millisecond, 10)
	snap := store.Put(buildTestSnapshot(""))
	time.Sleep(30 * time.Millisecond)
	_, err := store.Get(snap.ID)
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrStaleRef {
		t.Fatalf("expected STALE_REF after TTL expiry, got %v", err)
	}
}

func TestSnapshotStore_Resolve(t *testing.T) {
	store := NewSnapshotStore(time.Minute, 10)
	snap := store.Put(buildTestSnapshot(""))

	ref := FormatElementRef(snap.ID, "e2")
	gotSnap, gotEl, err := store.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if gotSnap.ID != snap.ID {
		t.Fatalf("Resolve() returned wrong snapshot")
	}
	if gotEl.Name != "Save" {
		t.Fatalf("Resolve() returned wrong element: %+v", gotEl)
	}
}

func TestSnapshotStore_Resolve_ElementNotFound(t *testing.T) {
	store := NewSnapshotStore(time.Minute, 10)
	snap := store.Put(buildTestSnapshot(""))
	ref := FormatElementRef(snap.ID, "e999")
	_, _, err := store.Resolve(ref)
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrElementNotFound {
		t.Fatalf("expected ELEMENT_NOT_FOUND, got %v", err)
	}
}

func TestSnapshotStore_Resolve_StaleSnapshot(t *testing.T) {
	store := NewSnapshotStore(10*time.Millisecond, 10)
	snap := store.Put(buildTestSnapshot(""))
	ref := FormatElementRef(snap.ID, "e2")
	time.Sleep(30 * time.Millisecond)
	_, _, err := store.Resolve(ref)
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrStaleRef {
		t.Fatalf("expected STALE_REF, got %v", err)
	}
}

func TestSnapshotStore_Resolve_InvalidRef(t *testing.T) {
	store := NewSnapshotStore(time.Minute, 10)
	_, _, err := store.Resolve("not-a-valid-ref")
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrInvalidArgs {
		t.Fatalf("expected INVALID_ARGS, got %v", err)
	}
}

func TestSnapshotStore_LRUEviction(t *testing.T) {
	store := NewSnapshotStore(0, 2)
	s1 := store.Put(buildTestSnapshot(""))
	time.Sleep(2 * time.Millisecond)
	s2 := store.Put(buildTestSnapshot(""))
	time.Sleep(2 * time.Millisecond)
	// Touch s1 so it becomes more recently used than s2.
	if _, err := store.Get(s1.ID); err != nil {
		t.Fatalf("unexpected error touching s1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	s3 := store.Put(buildTestSnapshot(""))

	if store.Len() != 2 {
		t.Fatalf("expected store to cap at 2 entries, got %d", store.Len())
	}
	if _, err := store.Get(s2.ID); err == nil {
		t.Fatalf("expected s2 (least recently used) to have been evicted")
	}
	if _, err := store.Get(s1.ID); err != nil {
		t.Fatalf("expected s1 (recently touched) to still be present: %v", err)
	}
	if _, err := store.Get(s3.ID); err != nil {
		t.Fatalf("expected s3 (most recently added) to still be present: %v", err)
	}
}

func TestParseElementRef_FormatElementRef(t *testing.T) {
	ref := FormatElementRef("s8f3k2p9", "e17")
	if ref != "@s8f3k2p9:e17" {
		t.Fatalf("unexpected formatted ref: %s", ref)
	}
	snapID, elemID, err := ParseElementRef(string(ref))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if snapID != "s8f3k2p9" || elemID != "e17" {
		t.Fatalf("unexpected parse result: snap=%q elem=%q", snapID, elemID)
	}
}

func TestParseElementRef_Invalid(t *testing.T) {
	cases := []string{"", "no-at-prefix:e1", "@missingcolon", "@:e1", "@snap:"}
	for _, ref := range cases {
		t.Run(ref, func(t *testing.T) {
			_, _, err := ParseElementRef(ref)
			de, ok := AsDesktopError(err)
			if !ok || de.Code != ErrInvalidArgs {
				t.Fatalf("ParseElementRef(%q): expected INVALID_ARGS, got %v", ref, err)
			}
		})
	}
}
