package design_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/ipc/dbproxy/proxytest"
)

func artifact(id string) design.Artifact {
	return design.Artifact{
		ID: id, Title: "P5", Slug: "p5", Dir: "design/p5",
		Kind: design.KindWeb, CurrentVersion: 1,
	}
}

// On an IPC secondary the design store writes through the proxy, including
// its multi-statement writes, which are forwarded as one batch and applied in
// one transaction on the primary.
func TestDesignStoreWritesThroughProxyOnSecondary(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()
	store := design.NewStoreWithProxy(tp.SecondaryDB, tp.Proxy)

	tp.HoldWriteLock(t, 700*time.Millisecond)
	started := time.Now()
	a, err := store.CreateArtifact(ctx, artifact("dsg_p5a"))
	if err != nil {
		t.Fatalf("CreateArtifact under primary lock: %v", err)
	}
	if time.Since(started) < 500*time.Millisecond {
		t.Fatalf("CreateArtifact returned in %s while the lock was held", time.Since(started))
	}

	// Multi-statement: version row + artifact pointer, atomic across the proxy.
	tp.HoldWriteLock(t, 500*time.Millisecond)
	if err := store.AddVersion(ctx, design.Version{ArtifactID: a.ID, Number: 2, SnapshotID: "snap", Summary: "v2"}); err != nil {
		t.Fatalf("AddVersion under primary lock: %v", err)
	}
	got, err := store.GetArtifact(ctx, a.ID)
	if err != nil || got.CurrentVersion != 2 {
		t.Fatalf("artifact after AddVersion = %+v err=%v", got, err)
	}
	if _, err := store.GetVersion(ctx, a.ID, 2); err != nil {
		t.Fatalf("GetVersion: %v", err)
	}

	// Batch with many statements: clear + N inserts.
	nodes := make([]design.Node, 0, 3)
	for _, id := range []string{"n1", "n2", "n3"} {
		nodes = append(nodes, design.Node{ArtifactID: a.ID, Version: 2, NodeID: id,
			Selector: "#" + id, Role: "box", Box: design.Rect{W: 10, H: 20},
			Styles: map[string]string{"color": "red"}})
	}
	tp.HoldWriteLock(t, 500*time.Millisecond)
	if err := store.ReplaceNodes(ctx, a.ID, 2, nodes); err != nil {
		t.Fatalf("ReplaceNodes under primary lock: %v", err)
	}
	listed, err := store.ListNodes(ctx, a.ID, 2, -1)
	if err != nil || len(listed) != 3 {
		t.Fatalf("ListNodes = %d err=%v", len(listed), err)
	}
	if listed[0].Styles["color"] != "red" {
		t.Fatalf("styles lost across the proxy: %+v", listed[0].Styles)
	}

	// Rows affected survive the round trip, so "not found" is still an error.
	tp.HoldWriteLock(t, 400*time.Millisecond)
	if err := store.UpdateArtifact(ctx, got); err != nil {
		t.Fatalf("UpdateArtifact under primary lock: %v", err)
	}
	if err := store.DeleteArtifact(ctx, "dsg_missing"); !errors.Is(err, design.ErrNotFound) {
		t.Fatalf("deleting a missing artifact = %v, want ErrNotFound", err)
	}
	tp.HoldWriteLock(t, 400*time.Millisecond)
	if err := store.DeleteArtifact(ctx, a.ID); err != nil {
		t.Fatalf("DeleteArtifact under primary lock: %v", err)
	}
	if _, err := store.GetArtifact(ctx, a.ID); !errors.Is(err, design.ErrNotFound) {
		t.Fatalf("artifact still present: %v", err)
	}
}

// The primary path is unchanged: a store with no proxy writes directly.
func TestDesignStoreDirectOnPrimary(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()
	store := design.NewStore(tp.PrimaryDB)

	a, err := store.CreateArtifact(ctx, artifact("dsg_p5b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddVersion(ctx, design.Version{ArtifactID: a.ID, Number: 2, SnapshotID: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrentVersion(ctx, a.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrentVersion(ctx, "dsg_missing", 1); !errors.Is(err, design.ErrNotFound) {
		t.Fatalf("missing artifact = %v, want ErrNotFound", err)
	}
	if _, err := store.AddCritique(ctx, design.Critique{ArtifactID: a.ID, Version: 1, Score: 7, Summary: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LatestCritique(ctx, a.ID, 1); err != nil {
		t.Fatal(err)
	}
}
