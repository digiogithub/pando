package fswatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newProject creates a temp dir recognised as a project (safe to watch).
func newProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// waitFor drains sub until an event for path arrives or the timeout expires.
func waitFor(t *testing.T, sub *Subscription, path string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-sub.Events:
			if !ok {
				return false
			}
			if ev.Name == path {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func waitPaths(t *testing.T, h *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.PathCount() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("PathCount = %d, want >= %d", h.PathCount(), want)
}

func TestHubFanOutAndUnsubscribe(t *testing.T) {
	root := newProject(t)
	h := NewHub(root, nil)
	defer h.Close()
	a := mustSubscribe(t, h)
	b := mustSubscribe(t, h)
	waitPaths(t, h, 1)
	if h.SubscriberCount() != 2 {
		t.Fatalf("SubscriberCount = %d, want 2", h.SubscriberCount())
	}

	f1 := filepath.Join(root, "one.txt")
	if err := os.WriteFile(f1, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, a, f1, 5*time.Second) || !waitFor(t, b, f1, 5*time.Second) {
		t.Fatal("both subscribers should receive the event")
	}

	a.Close()
	// Buffered events may still be pending; the channel must end up closed.
	for range a.Events {
	}
	f2 := filepath.Join(root, "two.txt")
	if err := os.WriteFile(f2, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, b, f2, 5*time.Second) {
		t.Fatal("remaining subscriber should still receive events")
	}
	if h.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount = %d, want 1", h.SubscriberCount())
	}
}

func mustSubscribe(t *testing.T, h *Hub) *Subscription {
	t.Helper()
	s := h.Subscribe()
	if s == nil {
		t.Fatal("Subscribe returned nil")
	}
	return s
}

func TestHubExcludedDirsNotWatchedAndNewDirsAdded(t *testing.T) {
	root := newProject(t)
	for _, d := range []string{"node_modules/pkg", "src/deep"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHub(root, NewExcluder(root, nil))
	defer h.Close()
	sub := mustSubscribe(t, h)

	// root + src + src/deep are watched; node_modules is not.
	waitPaths(t, h, 3)
	time.Sleep(100 * time.Millisecond)
	if got := h.PathCount(); got != 3 {
		t.Fatalf("PathCount = %d, want 3 (excluded dirs must not be watched)", got)
	}

	// Events inside the excluded dir are never delivered.
	hidden := filepath.Join(root, "node_modules", "pkg", "x.js")
	if err := os.WriteFile(hidden, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if waitFor(t, sub, hidden, 500*time.Millisecond) {
		t.Fatal("event from excluded dir was delivered")
	}

	// A new directory is added dynamically and its files are reported.
	newDir := filepath.Join(root, "fresh")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	waitPaths(t, h, 4)
	file := filepath.Join(newDir, "a.txt")
	var seen bool
	for i := 0; i < 20 && !seen; i++ {
		_ = os.WriteFile(file, []byte{byte(i)}, 0o644)
		seen = waitFor(t, sub, file, 250*time.Millisecond)
	}
	if !seen {
		t.Fatal("no event from dynamically added directory")
	}
}

func TestHubCloseClosesSubscriptions(t *testing.T) {
	h := NewHub(newProject(t), nil)
	s := mustSubscribe(t, h)
	h.Close()
	select {
	case _, ok := <-s.Events:
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events not closed after hub Close")
	}
	if h.Subscribe() != nil {
		t.Fatal("Subscribe on closed hub should return nil")
	}
}
