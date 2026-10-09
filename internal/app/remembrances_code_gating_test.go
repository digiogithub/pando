package app

import (
	"sync/atomic"
	"testing"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
)

func TestIsSecondaryAtStartup(t *testing.T) {
	standalone := &App{DBQuerier: db.New(nil)}
	if standalone.isSecondaryAtStartup() {
		t.Fatal("standalone app must not be treated as secondary")
	}

	secondary := &App{DBQuerier: dbproxy.New(db.New(nil), nil, "")}
	if !secondary.isSecondaryAtStartup() {
		t.Fatal("app with a DBProxy querier must be treated as secondary")
	}

	secondary.IPCIsPrimary = true
	if secondary.isSecondaryAtStartup() {
		t.Fatal("app that is already primary must not be treated as secondary")
	}
}

func TestStartDeferredCodeIndexRunsOnce(t *testing.T) {
	app := &App{}
	app.startDeferredCodeIndex() // nothing parked: no-op

	var calls int32
	app.deferredCodeIndex = func() { atomic.AddInt32(&calls, 1) }
	app.startDeferredCodeIndex()
	app.startDeferredCodeIndex()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("deferred start ran %d times, want 1", got)
	}
}
