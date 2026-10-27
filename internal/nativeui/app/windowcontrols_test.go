package app

import (
	"context"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

// winHost is a fakeHost that also drives a window.
type winHost struct {
	fakeHost
	wmu       sync.Mutex
	calls     []string
	maximized bool
	tray      bool
}

func (h *winHost) rec(s string) { h.wmu.Lock(); h.calls = append(h.calls, s); h.wmu.Unlock() }
func (h *winHost) Minimize()    { h.rec("min") }
func (h *winHost) ToggleMaximize() {
	h.rec("max")
	h.wmu.Lock()
	h.maximized = !h.maximized
	h.wmu.Unlock()
}
func (h *winHost) IsMaximized() bool { h.wmu.Lock(); defer h.wmu.Unlock(); return h.maximized }
func (h *winHost) CloseWindow()      { h.rec("close") }
func (h *winHost) ToggleFullScreen() { h.rec("fullscreen") }
func (h *winHost) UseTray() bool     { return h.tray }
func (h *winHost) got() []string {
	h.wmu.Lock()
	defer h.wmu.Unlock()
	return append([]string(nil), h.calls...)
}

func newWinRoot(t *testing.T, custom bool) (*Context, *ui.Tester, *winHost) {
	t.Helper()
	old := CustomFrame
	CustomFrame = func() bool { return custom }
	t.Cleanup(func() { CustomFrame = old })
	a := NewContext(context.Background(), nil)
	a.SetDispatcher(&QueueDispatcher{})
	h := &winHost{}
	a.Shell().Host = h
	return a, ui.NewTester(NewRoot(a).View, 1100, 700), h
}

func TestWindowControlsCallHost(t *testing.T) {
	a, tt, h := newWinRoot(t, true)
	tt.Frame()
	for _, label := range []string{a.T("shell.minimise"), a.T("shell.maximiseWindow"), a.T("shell.closeWindow")} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %q: %v", label, err)
		}
	}
	got := h.got()
	if len(got) != 3 || got[0] != "min" || got[1] != "max" || got[2] != "close" {
		t.Fatalf("host calls = %v", got)
	}
	// The icon and label reflect the maximised state.
	tt.Frame()
	if err := tt.Click(a.T("shell.restoreWindow")); err != nil {
		t.Fatalf("restore button missing after maximise: %v", err)
	}
}

func TestWindowControlsTrayLabels(t *testing.T) {
	a, tt, h := newWinRoot(t, true)
	h.tray = true
	tt.Frame()
	if err := tt.Click(a.T("shell.minimiseToTray")); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(a.T("shell.closeToTray")); err != nil {
		t.Fatal(err)
	}
}

func TestNoWindowControlsOnMacOS(t *testing.T) {
	a, tt, _ := newWinRoot(t, false)
	tt.Frame()
	for _, k := range []string{"shell.minimise", "shell.maximiseWindow", "shell.closeWindow"} {
		if err := tt.Click(a.T(k)); err == nil {
			t.Errorf("%s drawn although the native title bar is used", k)
		}
	}
}

func TestFrameShortcuts(t *testing.T) {
	_, tt, h := newWinRoot(t, true)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyQ)
	if !h.quit {
		t.Error("Ctrl+Q did not quit")
	}
	tt.Key(0, ui.KeyF11)
	if got := h.got(); len(got) != 1 || got[0] != "fullscreen" {
		t.Errorf("F11 calls = %v", got)
	}
}

func TestFrameShortcutsOffOnMacOS(t *testing.T) {
	_, tt, h := newWinRoot(t, false)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyQ)
	if h.quit {
		t.Error("Cmd+Q handled by the app on macOS (the menu does it)")
	}
}
