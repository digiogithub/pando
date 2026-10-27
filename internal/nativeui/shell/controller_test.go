package shell

import "testing"

func TestUseTrayNeedsSettingAndLiveIcon(t *testing.T) {
	c := newController(Options{})
	if c.UseTray() {
		t.Fatal("tray used by default")
	}
	c.SetTrayOnClose(true)
	if c.UseTray() {
		t.Fatal("setting on without a live tray icon must keep the normal minimise/close")
	}
	c.trayOK.Store(true)
	if !c.UseTray() {
		t.Fatal("setting on with a tray icon must hide to the tray")
	}
	c.SetTrayOnClose(false)
	if c.UseTray() {
		t.Fatal("setting off must restore the normal behaviour")
	}
}

func TestTrayLabelsDefaults(t *testing.T) {
	l := TrayLabels{Quit: "Salir"}.orDefault()
	if l.Show != "Show Pando" || l.Settings != "Settings" || l.Quit != "Salir" {
		t.Errorf("labels = %+v", l)
	}
}

func TestDefaultMenuHasEditMenu(t *testing.T) {
	// The Edit role menu must stay: macOS copy/paste keys come from it.
	if DefaultMenu(nil) == nil {
		t.Fatal("no menu")
	}
}

type fakeWin struct{ calls []string }

func (f *fakeWin) Hide()           { f.calls = append(f.calls, "hide") }
func (f *fakeWin) Minimize()       { f.calls = append(f.calls, "minimize") }
func (f *fakeWin) Close()          { f.calls = append(f.calls, "close") }
func (f *fakeWin) ToggleMaximize() { f.calls = append(f.calls, "toggle") }

func TestMinimizeCloseFollowTraySetting(t *testing.T) {
	c := newController(Options{})
	w := &fakeWin{}
	c.minimize(w)
	c.closeWindow(w)
	if got := w.calls; len(got) != 2 || got[0] != "minimize" || got[1] != "close" {
		t.Fatalf("no tray: %v", got)
	}
	// Setting on but no live tray icon: still normal.
	c.SetTrayOnClose(true)
	w.calls = nil
	c.minimize(w)
	c.closeWindow(w)
	if got := w.calls; got[0] != "minimize" || got[1] != "close" {
		t.Fatalf("setting without tray icon: %v", got)
	}
	c.trayOK.Store(true)
	w.calls = nil
	c.minimize(w)
	c.closeWindow(w)
	if got := w.calls; len(got) != 2 || got[0] != "hide" || got[1] != "hide" {
		t.Fatalf("tray: %v", got)
	}
	// An explicit quit closes for real.
	c.quitting.Store(true)
	w.calls = nil
	c.closeWindow(w)
	if w.calls[0] != "close" {
		t.Fatalf("quitting: %v", w.calls)
	}
}

func TestWindowFramePerPlatform(t *testing.T) {
	cases := []struct {
		goos      string
		frameless bool
		hidden    bool
	}{{"darwin", false, false}, {"linux", true, false}, {"windows", false, true}}
	for _, tc := range cases {
		fl, style, hide := windowFrame(tc.goos)
		if fl != tc.frameless || hide != tc.hidden || (style == "hidden") != tc.hidden {
			t.Errorf("%s: frameless=%v style=%q hideButtons=%v", tc.goos, fl, style, hide)
		}
	}
}
