package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func footerRig(t *testing.T, st ExternalAccess) (*Context, *ui.Tester, *QueueDispatcher, func() (int, bool)) {
	t.Helper()
	var mu sync.Mutex
	cur, puts, last := st, 0, false
	mux := http.NewServeMux()
	mux.HandleFunc(externalAccessPath, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			var body struct {
				Enabled bool `json:"enabled"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			cur.Enabled, last = body.Enabled, body.Enabled
			puts++
			if body.Enabled {
				cur.URLs = []string{"http://192.168.1.5:8765"}
			}
		}
		_ = json.NewEncoder(w).Encode(cur)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := NewContext(ctx, c)
	q := &QueueDispatcher{}
	a.SetDispatcher(q)
	root := NewRoot(a)
	tt := ui.NewTester(root.View, 1100, 700)
	return a, tt, q, func() (int, bool) { mu.Lock(); defer mu.Unlock(); return puts, last }
}

func pumpUntil(t *testing.T, tt *ui.Tester, q *QueueDispatcher, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		q.Drain()
		tt.Frame()
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out; texts %q", tt.Texts())
}

func TestFooterExternalAccessToggle(t *testing.T) {
	a, tt, q, puts := footerRig(t, ExternalAccess{CanToggle: true, BasicAuthReady: true, Port: 8765})
	off := a.T("externalAccess.off")
	pumpUntil(t, tt, q, func() bool { return tt.HasText(off) })
	if err := tt.Click(off); err != nil {
		t.Fatal(err)
	}
	pumpUntil(t, tt, q, func() bool { return tt.HasText(a.T("externalAccess.on")) })
	if n, enabled := puts(); n != 1 || !enabled {
		t.Fatalf("PUT count %d enabled %v", n, enabled)
	}
	// And back off.
	if err := tt.Click(a.T("externalAccess.on")); err != nil {
		t.Fatal(err)
	}
	pumpUntil(t, tt, q, func() bool { return tt.HasText(off) })
	if n, enabled := puts(); n != 2 || enabled {
		t.Fatalf("PUT count %d enabled %v", n, enabled)
	}
}

// Without a Web UI user, enabling only warns and opens the settings.
func TestFooterExternalAccessNeedsCredentials(t *testing.T) {
	a, tt, q, puts := footerRig(t, ExternalAccess{CanToggle: true, Port: 8765})
	off := a.T("externalAccess.off")
	pumpUntil(t, tt, q, func() bool { return tt.HasText(off) })
	if err := tt.Click(off); err != nil {
		t.Fatal(err)
	}
	pumpUntil(t, tt, q, func() bool { return tt.HasText(a.T("externalAccess.needsCredentials")) })
	if n, _ := puts(); n != 0 {
		t.Fatalf("PUT sent without credentials (%d)", n)
	}
}

// A bind fixed with --host (canToggle false, off) hides the toggle.
func TestFooterExternalAccessHiddenWhenFixed(t *testing.T) {
	a, tt, q, _ := footerRig(t, ExternalAccess{Port: 8765})
	for range 20 {
		q.Drain()
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if tt.HasText(a.T("externalAccess.off")) || tt.HasText(a.T("externalAccess.on")) {
		t.Fatal("toggle shown for a fixed bind")
	}
}
